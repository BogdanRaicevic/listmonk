-- Fork-only queries. Kept in a separate file to stay clear of upstream's query files.

-- name: record-campaign-messages
-- Upserts the delivery outcome of campaign e-mails. $1..$4 are parallel arrays of
-- campaign IDs, subscriber IDs, statuses and errors. Rows for campaigns or
-- subscribers deleted in the meantime are skipped, and if a batch has the same
-- campaign+subscriber more than once, the last one wins.
INSERT INTO campaign_messages (campaign_id, subscriber_id, status, error)
    SELECT DISTINCT ON (u.campaign_id, u.subscriber_id) u.campaign_id, u.subscriber_id, u.status, u.error
    FROM UNNEST($1::INT[], $2::INT[], $3::TEXT[], $4::TEXT[]) WITH ORDINALITY AS u(campaign_id, subscriber_id, status, error, n)
    WHERE EXISTS (SELECT 1 FROM campaigns WHERE id = u.campaign_id)
        AND EXISTS (SELECT 1 FROM subscribers WHERE id = u.subscriber_id)
    ORDER BY u.campaign_id, u.subscriber_id, u.n DESC
ON CONFLICT (campaign_id, subscriber_id) DO UPDATE SET
    status = EXCLUDED.status,
    error = EXCLUDED.error,
    attempts = campaign_messages.attempts + 1,
    updated_at = NOW();

-- name: get-subscriber-campaign-messages
-- The campaign e-mails sent to a subscriber, newest first, limited to campaigns
-- on lists the user can access.
SELECT cm.campaign_id, c.name AS campaign_name, c.subject AS campaign_subject,
       cm.status, cm.error, cm.attempts, cm.created_at, cm.updated_at
FROM campaign_messages cm
JOIN campaigns c ON c.id = cm.campaign_id
WHERE cm.subscriber_id = $1
    AND ($2 OR EXISTS (
        SELECT 1 FROM campaign_lists WHERE campaign_id = cm.campaign_id AND list_id = ANY($3::INT[])
    ))
ORDER BY cm.updated_at DESC
LIMIT 200;

-- name: has-campaign-messages
-- Whether a campaign has any logged e-mails, ie: was sent with the message log on.
SELECT EXISTS (SELECT 1 FROM campaign_messages WHERE campaign_id = $1);
