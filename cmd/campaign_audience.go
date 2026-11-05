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
}

// campaignRecipientsExp is an advanced-query expression (for the subscribers
// page) matching the subscribers a campaign was sent to. listmonk does not log
// individual sends, so it mirrors next-campaign-subscribers: subscribers on the
// campaign's lists when it started, up to its max_subscriber_id, honouring list
// optin and the campaign's exclusion lists. Current statuses are used, so those
// who have since unsubscribed or been blocklisted are left out.
const campaignRecipientsExp = `subscribers.status != 'blocklisted' AND subscribers.id IN (
  SELECT sl.subscriber_id FROM subscriber_lists sl
  JOIN lists l ON l.id = sl.list_id
  JOIN campaigns c ON c.id = %[1]d
  WHERE sl.list_id IN (SELECT list_id FROM campaign_lists WHERE campaign_id = %[1]d)
    AND sl.subscriber_id <= c.max_subscriber_id
    AND sl.created_at <= COALESCE(c.started_at, NOW())
    AND ((l.optin = 'double' AND sl.status = 'confirmed') OR (l.optin != 'double' AND sl.status != 'unsubscribed'))
) AND subscribers.id NOT IN (
  SELECT esl.subscriber_id FROM subscriber_lists esl
  WHERE esl.list_id IN (SELECT list_id FROM campaign_exclude_lists WHERE campaign_id = %[1]d AND list_id IS NOT NULL)
)`

// newCampaignAudience returns the engagement segment queries for a campaign.
func newCampaignAudience(id int, name string) campaignAudience {
	var (
		viewed    = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM campaign_views WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		clicked   = fmt.Sprintf(`subscribers.id IN (SELECT subscriber_id FROM link_clicks WHERE campaign_id = %d AND subscriber_id IS NOT NULL)`, id)
		recipient = fmt.Sprintf(campaignRecipientsExp, id)
	)

	return campaignAudience{
		ID:         id,
		Name:       name,
		Opened:     viewed,
		NotOpened:  recipient + " AND NOT (" + viewed + ")",
		Clicked:    clicked,
		NotClicked: recipient + " AND NOT (" + clicked + ")",
	}
}
