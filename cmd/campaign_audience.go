package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v5"
)

// audienceSegment is one engagement segment of a campaign's recipients. Query is
// an advanced-query expression for the subscribers page and its CSV export.
type audienceSegment struct {
	Key   string // eg: not-opened. Used in test IDs and download file names.
	Label string // i18n key.
	Query string
	Count int
}

// campaignAudience holds the segments of a single campaign.
type campaignAudience struct {
	ID       int
	Name     string
	Segments []audienceSegment

	// Exact is true when recipients come from the campaign message log rather
	// than being estimated from list memberships (campaigns sent before the log).
	Exact bool
}

// campaignSentExp matches subscribers who were on the campaign's lists when it
// started (up to its max_subscriber_id) and not in its exclusion lists. listmonk
// does not log individual sends, so this mirrors how next-campaign-subscribers
// picks recipients. %[1]d is the campaign ID and %[2]s an extra condition on the
// subscription (sl) row.
const campaignSentExp = `subscribers.id IN (
  SELECT sl.subscriber_id FROM subscriber_lists sl
  JOIN lists l ON l.id = sl.list_id
  JOIN campaigns c ON c.id = %[1]d
  WHERE sl.list_id IN (SELECT list_id FROM campaign_lists WHERE campaign_id = %[1]d)
    AND sl.subscriber_id <= c.max_subscriber_id
    AND sl.created_at <= COALESCE(c.started_at, NOW())
    AND %[2]s
) AND subscribers.id NOT IN (
  SELECT esl.subscriber_id FROM subscriber_lists esl
  WHERE esl.list_id IN (SELECT list_id FROM campaign_exclude_lists WHERE campaign_id = %[1]d AND list_id IS NOT NULL)
)`

// campaignLoggedExp matches subscribers the campaign message log has as sent to
// (exact), whose subscription to one of the campaign's lists matches %[2]s.
const campaignLoggedExp = `subscribers.id IN (
  SELECT subscriber_id FROM campaign_messages WHERE campaign_id = %[1]d AND status = 'sent'
) AND subscribers.id IN (
  SELECT sl.subscriber_id FROM subscriber_lists sl
  JOIN lists l ON l.id = sl.list_id
  JOIN campaigns c ON c.id = %[1]d
  WHERE sl.list_id IN (SELECT list_id FROM campaign_lists WHERE campaign_id = %[1]d)
    AND %[2]s
)`

const (
	// The subscription is still active (honouring list optin), so the
	// subscriber received the campaign and can still be retargeted.
	subActiveCond = `((l.optin = 'double' AND sl.status = 'confirmed') OR (l.optin != 'double' AND sl.status != 'unsubscribed'))`

	// The subscription was unsubscribed after the campaign started. Memberships
	// keep only their last update, so this is attributed to the campaign by time.
	subUnsubAfterCond = `(sl.status = 'unsubscribed' AND sl.updated_at >= c.started_at)`
)

// newCampaignAudience returns the engagement segments of a campaign (without
// counts). exact uses the campaign message log for who received the campaign.
func newCampaignAudience(id int, name string, exact bool) campaignAudience {
	sentExp := campaignSentExp
	if exact {
		sentExp = campaignLoggedExp
	}

	var (
		viewed    = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM campaign_views WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		clicked   = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM link_clicks WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		bounced   = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM bounces WHERE campaign_id = %d)`, id)
		recipient = `subscribers.status != 'blocklisted' AND ` + fmt.Sprintf(sentExp, id, subActiveCond)
		unsubbed  = fmt.Sprintf(sentExp, id, subUnsubAfterCond)
	)

	return campaignAudience{
		ID:    id,
		Name:  name,
		Exact: exact,
		Segments: []audienceSegment{
			{Key: "opened", Label: "analytics.opened", Query: viewed},
			{Key: "not-opened", Label: "analytics.notOpened", Query: recipient + " AND NOT (" + viewed + ")"},
			{Key: "clicked", Label: "analytics.clicked", Query: clicked},
			{Key: "not-clicked", Label: "analytics.notClicked", Query: recipient + " AND NOT (" + clicked + ")"},
			{Key: "bounced", Label: "analytics.bounced", Query: bounced},

			// Unsubscribed from the campaign's list(s) only.
			{Key: "unsub-list", Label: "analytics.unsubList", Query: `subscribers.status != 'blocklisted' AND ` + unsubbed},

			// "Unsubscribe from all" blocklists the subscriber and unsubscribes every
			// list. Exclude those who bounced on this campaign, as bounce actions can
			// also blocklist.
			{Key: "unsub-all", Label: "analytics.unsubAll", Query: `subscribers.status = 'blocklisted' AND ` + unsubbed + " AND NOT (" + bounced + ")"},
		},
	}
}

// countAudience fills in the subscriber count of each segment with a single query.
// The expressions are built above from the integer campaign ID only.
func countAudience(db *sqlx.DB, a *campaignAudience) error {
	cols := make([]string, len(a.Segments))
	for i, s := range a.Segments {
		cols[i] = "COUNT(*) FILTER (WHERE " + s.Query + ")"
	}

	dest := make([]any, len(a.Segments))
	for i := range a.Segments {
		dest[i] = &a.Segments[i].Count
	}

	return db.QueryRow("SELECT " + strings.Join(cols, ", ") + " FROM subscribers").Scan(dest...)
}

// exportSubscribersSlim writes a subscriber export with only the columns needed
// for retargeting: email, plus name and the `firma` attribute when at least one
// exported subscriber has a value for them. Rows are collected first so that
// empty optional columns can be left out of the header.
func (a *App) exportSubscribersSlim(c *echo.Context, exp func() ([]models.SubscriberExport, error)) error {
	type row struct{ email, name, firma string }

	var (
		rows     []row
		hasName  bool
		hasFirma bool
	)
	for {
		out, err := exp()
		if err != nil {
			return err
		}
		if len(out) == 0 {
			break
		}

		for _, r := range out {
			var attr struct {
				Firma any `json:"firma"`
			}
			_ = json.Unmarshal([]byte(r.Attribs), &attr)

			firma := ""
			if attr.Firma != nil {
				firma = strings.TrimSpace(fmt.Sprint(attr.Firma))
			}

			hasName = hasName || strings.TrimSpace(r.Name) != ""
			hasFirma = hasFirma || firma != ""
			rows = append(rows, row{email: r.Email, name: r.Name, firma: firma})
		}
	}

	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, "text/csv; charset=utf-8")
	hdr.Set(echo.HeaderContentDisposition, "attachment; filename=subscribers.csv")
	hdr.Set(echo.HeaderCacheControl, "no-cache")

	cols := []string{"email"}
	if hasName {
		cols = append(cols, "name")
	}
	if hasFirma {
		cols = append(cols, "firma")
	}

	wr := csv.NewWriter(c.Response())
	if err := wr.Write(cols); err != nil {
		return err
	}
	for _, r := range rows {
		rec := []string{r.email}
		if hasName {
			rec = append(rec, r.name)
		}
		if hasFirma {
			rec = append(rec, r.firma)
		}
		if err := wr.Write(rec); err != nil {
			a.log.Printf("error streaming slim CSV export: %v", err)
			break
		}
	}
	wr.Flush()

	return wr.Error()
}
