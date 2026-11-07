package main

// Fork: per-subscriber campaign message log (campaign_messages).

import (
	"time"

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
