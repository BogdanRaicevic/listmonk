package main

// Fork: how often a campaign's audience was e-mailed recently, and how much a
// list overlaps with other lists.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// defaultUnopenedLast is how many last e-mails the preview checks for opens
// when the campaign doesn't skip non-openers.
const defaultUnopenedLast = 5

func validContactRules(windowDays, contactedMin, unopenedLast int) bool {
	return windowDays >= 1 && windowDays <= 365 &&
		contactedMin >= 0 && contactedMin <= 1000 &&
		unopenedLast >= 0 && unopenedLast <= 100
}

type audiencePreview struct {
	Total          int       `db:"total" json:"total"`
	Contacted      int       `db:"contacted" json:"contacted"`
	Contacted1To2  int       `db:"contacted_1_2" json:"contacted_1_2"`
	Contacted3To6  int       `db:"contacted_3_6" json:"contacted_3_6"`
	Contacted7Plus int       `db:"contacted_7_plus" json:"contacted_7_plus"`
	Unopened       int       `db:"unopened" json:"unopened"`
	UnopenedLast   int       `db:"-" json:"unopened_last"`
	Skipped        int       `db:"skipped" json:"skipped"`
	LogSince       null.Time `db:"log_since" json:"log_since"`
}

// PreviewCampaignAudience returns how often the subscribers a campaign would
// reach were e-mailed recently, and how many its skip rules would leave out.
func (a *App) PreviewCampaignAudience(c *echo.Context) error {
	var req struct {
		CampaignID        int   `json:"campaign_id"`
		ListIDs           []int `json:"lists"`
		ExcludeListIDs    []int `json:"exclude_lists"`
		ContactWindowDays int   `json:"contact_window_days"`
		SkipContactedMin  int   `json:"skip_contacted_min"`
		SkipUnopenedLast  int   `json:"skip_unopened_last"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !validContactRules(req.ContactWindowDays, req.SkipContactedMin, req.SkipUnopenedLast) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("campaigns.fieldInvalidContactRules"))
	}

	user := auth.GetUser(c)
	lists := user.FilterListsByPerm(auth.PermTypeGet|auth.PermTypeManage, req.ListIDs)
	excl := user.FilterListsByPerm(auth.PermTypeGet|auth.PermTypeManage, req.ExcludeListIDs)

	unopenedLast := req.SkipUnopenedLast
	if unopenedLast == 0 {
		unopenedLast = defaultUnopenedLast
	}

	out := audiencePreview{UnopenedLast: unopenedLast}
	if err := a.queries.PreviewCampaignAudience.Get(&out, pq.Array(lists), pq.Array(excl), req.ContactWindowDays,
		req.SkipContactedMin, unopenedLast, req.SkipUnopenedLast > 0, req.CampaignID); err != nil {
		a.log.Printf("error previewing campaign audience: %v", err)
		return err
	}

	return c.JSON(http.StatusOK, okResp{out})
}

// listOverlap is how a list's subscribers overlap with other lists. The Query
// fields are advanced-query expressions for the subscribers page and export.
type listOverlap struct {
	Members  int             `db:"members"`
	InOther  int             `db:"in_other"`
	AvgOther float64         `db:"avg_other"`
	TopJSON  json.RawMessage `db:"top_lists"`
	Variants int             `db:"variants"`

	TopLists      []listOverlapItem
	InOtherQuery  string
	VariantsQuery string
}

type listOverlapItem struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Query string `json:"-"`
}

// getListOverlap counts only lists the user can see. Queries are built from integer IDs only.
func (a *App) getListOverlap(user auth.User, listID int) (listOverlap, error) {
	all, ids := user.GetPermittedLists(auth.PermTypeGet)

	var o listOverlap
	if err := a.queries.GetListOverlap.Get(&o, listID, all, pq.Array(ids)); err != nil {
		a.log.Printf("error fetching list overlap: %v", err)
		return o, err
	}
	if err := json.Unmarshal(o.TopJSON, &o.TopLists); err != nil {
		return o, err
	}

	member := func(id int) string {
		return fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM subscriber_lists WHERE list_id = %d AND status != 'unsubscribed')`, id)
	}
	for i, l := range o.TopLists {
		o.TopLists[i].Query = member(listID) + " AND " + member(l.ID)
	}

	other := fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM subscriber_lists WHERE list_id != %d AND status != 'unsubscribed')`, listID)
	if !all {
		other = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM subscriber_lists WHERE list_id != %d AND status != 'unsubscribed' AND list_id = ANY('%s'))`, listID, intArrayLiteral(ids))
	}
	o.InOtherQuery = member(listID) + " AND " + other
	o.VariantsQuery = member(listID) + ` AND EXISTS (SELECT 1 FROM subscribers s2 WHERE fork_canonical_email(s2.email) = fork_canonical_email(subscribers.email) AND s2.id != subscribers.id)`

	return o, nil
}

func intArrayLiteral(ids []int) string {
	s := "{"
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprint(id)
	}
	return s + "}"
}
