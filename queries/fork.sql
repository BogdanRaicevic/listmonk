-- Fork-only queries. Kept in a separate file to stay clear of upstream's query files.

-- name: record-campaign-messages
-- Upserts the delivery outcome of campaign e-mails. $1..$5 are parallel arrays of
-- campaign IDs, subscriber IDs, statuses, errors and whether a failure is
-- temporary. Rows for campaigns or subscribers deleted in the meantime are
-- skipped, and if a batch has the same campaign+subscriber more than once, the
-- last one wins.
--
-- A temporary failure is scheduled for an automatic retry (retry_at) with a
-- backoff of $6 * attempts, until it has been tried $7 times in all. A send
-- or a permanent failure clears retry_at.
INSERT INTO campaign_messages (campaign_id, subscriber_id, status, error, retry_at)
    SELECT DISTINCT ON (u.campaign_id, u.subscriber_id) u.campaign_id, u.subscriber_id, u.status, u.error,
        (CASE WHEN u.status = 'failed' AND u.temporary AND 1 < $7::INT THEN NOW() + $6::INTERVAL ELSE NULL END)
    FROM UNNEST($1::INT[], $2::INT[], $3::TEXT[], $4::TEXT[], $5::BOOLEAN[]) WITH ORDINALITY AS u(campaign_id, subscriber_id, status, error, temporary, n)
    WHERE EXISTS (SELECT 1 FROM campaigns WHERE id = u.campaign_id)
        AND EXISTS (SELECT 1 FROM subscribers WHERE id = u.subscriber_id)
    ORDER BY u.campaign_id, u.subscriber_id, u.n DESC
ON CONFLICT (campaign_id, subscriber_id) DO UPDATE SET
    status = EXCLUDED.status,
    error = EXCLUDED.error,
    attempts = campaign_messages.attempts + 1,
    -- EXCLUDED.retry_at is set only for temporary failures.
    retry_at = (CASE WHEN EXCLUDED.retry_at IS NOT NULL AND campaign_messages.attempts + 1 < $7::INT
        THEN NOW() + $6::INTERVAL * (campaign_messages.attempts + 1) ELSE NULL END),
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

-- name: claim-campaign-retries
-- Claims up to $1 failed campaign e-mails whose retry is due, for finished
-- regular campaigns. Claimed rows are pushed back by 15 minutes so that a slow or
-- crashed retry run doesn't resend them before its outcome is recorded (which
-- sets retry_at again). eligible is whether the subscriber should still get the
-- campaign: not blocklisted, still subscribed to one of its lists (honouring
-- optin) and not in one of its exclusion lists.
WITH due AS (
    SELECT cm.campaign_id, cm.subscriber_id FROM campaign_messages cm
    JOIN campaigns c ON c.id = cm.campaign_id
    WHERE cm.status = 'failed' AND cm.retry_at <= NOW()
        AND c.status = 'finished' AND c.type = 'regular'
    ORDER BY cm.retry_at
    LIMIT $1
    FOR UPDATE OF cm SKIP LOCKED
),
claimed AS (
    UPDATE campaign_messages cm SET retry_at = NOW() + INTERVAL '15 minutes'
    FROM due WHERE cm.campaign_id = due.campaign_id AND cm.subscriber_id = due.subscriber_id
    RETURNING cm.campaign_id, cm.subscriber_id
)
SELECT claimed.campaign_id, s.id, s.uuid, s.email, s.name, s.attribs, s.status, s.created_at, s.updated_at,
    (s.status != 'blocklisted'
        AND EXISTS (
            SELECT 1 FROM subscriber_lists sl JOIN lists l ON l.id = sl.list_id
            WHERE sl.subscriber_id = s.id
                AND sl.list_id IN (SELECT list_id FROM campaign_lists WHERE campaign_id = claimed.campaign_id)
                AND ((l.optin = 'double' AND sl.status = 'confirmed') OR (l.optin != 'double' AND sl.status != 'unsubscribed'))
        )
        AND NOT EXISTS (
            SELECT 1 FROM subscriber_lists esl
            WHERE esl.subscriber_id = s.id
                AND esl.list_id IN (SELECT list_id FROM campaign_exclude_lists WHERE campaign_id = claimed.campaign_id AND list_id IS NOT NULL)
        )
    ) AS eligible
FROM claimed JOIN subscribers s ON s.id = claimed.subscriber_id
ORDER BY claimed.campaign_id, s.id;

-- name: clear-campaign-retry
-- Cancels a scheduled retry (eg: the subscriber should no longer get the campaign).
UPDATE campaign_messages SET retry_at = NULL, updated_at = NOW() WHERE campaign_id = $1 AND subscriber_id = $2;

-- name: schedule-campaign-resend
-- Schedules all failed e-mails of a finished campaign to be resent now. Returns
-- how many were scheduled.
WITH upd AS (
    UPDATE campaign_messages cm SET retry_at = NOW()
    FROM campaigns c
    WHERE c.id = cm.campaign_id AND cm.campaign_id = $1 AND cm.status = 'failed' AND c.status = 'finished'
    RETURNING 1
)
SELECT COUNT(*) FROM upd;

-- name: get-campaign-message-counts
-- Counts of a campaign's logged e-mails that weren't sent, and of those pending a retry.
SELECT COUNT(*) FILTER (WHERE status != 'sent') AS not_sent,
       COUNT(*) FILTER (WHERE status = 'failed' AND retry_at IS NOT NULL) AS retrying
FROM campaign_messages WHERE campaign_id = $1;
