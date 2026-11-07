package main

// Fork: per-subscriber campaign message log (campaign_messages).

import (
	"net/http"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
)

// subCampaignMessage is a campaign e-mail sent to a subscriber, for the
// subscriber's Activity tab.
type subCampaignMessage struct {
	CampaignID      int       `db:"campaign_id"`
	CampaignName    string    `db:"campaign_name"`
	CampaignSubject string    `db:"campaign_subject"`
	Status          string    `db:"status"`
	Error           string    `db:"error"`
	Attempts        int       `db:"attempts"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// getSubscriberMessages returns the logged campaign e-mails of a subscriber,
// limited to campaigns on lists the user can access.
func (a *App) getSubscriberMessages(subID int, allLists bool, listIDs []int) ([]subCampaignMessage, error) {
	var out []subCampaignMessage
	if err := a.queries.GetSubscriberCampaignMessage.Select(&out, subID, allLists, pq.Array(listIDs)); err != nil {
		a.log.Printf("error fetching subscriber campaign messages: %v", err)
		return nil, err
	}
	return out, nil
}

// hasCampaignMessages reports whether a campaign has any logged e-mails, ie:
// whether it was sent with the message log in place.
func (a *App) hasCampaignMessages(campID int) bool {
	var has bool
	if err := a.queries.HasCampaignMessages.Get(&has, campID); err != nil {
		a.log.Printf("error checking campaign messages: %v", err)
		return false
	}
	return has
}

// campaignMessageCounts is the summary of a campaign's logged e-mails that
// weren't sent.
type campaignMessageCounts struct {
	Failed    int  `db:"failed"`
	Uncertain int  `db:"uncertain"`
	Retrying  int  `db:"retrying"`
	Finished  bool `db:"finished"`
}

// getCampaignMessageCounts returns how many of a campaign's e-mails weren't sent
// and how many of those are waiting for a retry.
func (a *App) getCampaignMessageCounts(campID int) (campaignMessageCounts, error) {
	var out campaignMessageCounts
	err := a.queries.GetCampaignMessageCounts.Get(&out, campID)
	return out, err
}

// ResendCampaignFailed schedules all failed e-mails of a finished campaign to be
// resent right away. The retry worker resends them to those who should still get
// the campaign.
func (a *App) ResendCampaignFailed(c *echo.Context) error {
	id := getID(c)

	// The user must be able to manage the campaign.
	if err := a.checkCampaignPerm(auth.PermTypeManage, id, c); err != nil {
		return err
	}

	var n int
	if err := a.queries.ScheduleCampaignResend.Get(&n, id); err != nil {
		a.log.Printf("error scheduling campaign resend: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, a.i18n.Ts("globals.messages.errorUpdating", "name", "campaign", "error", err.Error()))
	}

	// Nothing to resend: no failed e-mails, or the campaign isn't finished.
	if n == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.T("analytics.resendNone"))
	}

	return c.JSON(http.StatusOK, okResp{map[string]int{"scheduled": n}})
}
