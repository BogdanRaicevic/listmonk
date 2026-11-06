package main

import "fmt"

// campaignAudience holds subscriber-page SQL filters that segment a campaign's
// recipients by engagement, for exporting or retargeting via the subscribers page.
type campaignAudience struct {
	ID         int
	Name       string
	Opened     string
	NotOpened  string
	Clicked    string
	NotClicked string
	Bounced    string
	UnsubList  string
	UnsubAll   string
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

const (
	// The subscription is still active (honouring list optin), so the
	// subscriber received the campaign and can still be retargeted.
	subActiveCond = `((l.optin = 'double' AND sl.status = 'confirmed') OR (l.optin != 'double' AND sl.status != 'unsubscribed'))`

	// The subscription was unsubscribed after the campaign started. Memberships
	// keep only their last update, so this is attributed to the campaign by time.
	subUnsubAfterCond = `(sl.status = 'unsubscribed' AND sl.updated_at >= c.started_at)`
)

// newCampaignAudience returns the engagement segment queries for a campaign.
func newCampaignAudience(id int, name string) campaignAudience {
	var (
		viewed    = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM campaign_views WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		clicked   = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM link_clicks WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		bounced   = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM bounces WHERE campaign_id = %d)`, id)
		recipient = `subscribers.status != 'blocklisted' AND ` + fmt.Sprintf(campaignSentExp, id, subActiveCond)
		unsubbed  = fmt.Sprintf(campaignSentExp, id, subUnsubAfterCond)
	)

	return campaignAudience{
		ID:         id,
		Name:       name,
		Opened:     viewed,
		NotOpened:  recipient + " AND NOT (" + viewed + ")",
		Clicked:    clicked,
		NotClicked: recipient + " AND NOT (" + clicked + ")",
		Bounced:    bounced,

		// Unsubscribed from the campaign's list(s) only.
		UnsubList: `subscribers.status != 'blocklisted' AND ` + unsubbed,

		// "Unsubscribe from all" blocklists the subscriber and unsubscribes every
		// list. Exclude those who bounced on this campaign, as bounce actions can
		// also blocklist.
		UnsubAll: `subscribers.status = 'blocklisted' AND ` + unsubbed + " AND NOT (" + bounced + ")",
	}
}
