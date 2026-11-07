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
-- The ORDER BY on the primary key keeps it an index probe: with a plain EXISTS,
-- the prepared statement's generic plan seq-scans the whole table.
SELECT COUNT(*) > 0 FROM (
    SELECT 1 FROM campaign_messages WHERE campaign_id = $1 ORDER BY campaign_id, subscriber_id LIMIT 1
) x;

-- name: claim-campaign-retries
-- Claims up to $1 failed campaign e-mails whose retry is due, for finished
-- regular campaigns. Claimed rows are pushed back by 15 minutes so that a slow or
-- crashed retry run doesn't resend them before its outcome is recorded (which
-- sets retry_at again). eligible is whether the subscriber should still get the
-- campaign, ie: not blocklisted, still subscribed to one of its lists (honouring
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
-- Counts of a campaign's logged e-mails that weren't sent: failed, uncertain
-- (may have been delivered), and failed ones pending a retry; and whether the
-- campaign is finished (only finished campaigns are resent).
SELECT COUNT(*) FILTER (WHERE status = 'failed') AS failed,
       COUNT(*) FILTER (WHERE status = 'uncertain') AS uncertain,
       COUNT(*) FILTER (WHERE status = 'failed' AND retry_at IS NOT NULL) AS retrying,
       COALESCE((SELECT status = 'finished' FROM campaigns WHERE id = $1), FALSE) AS finished
FROM campaign_messages WHERE campaign_id = $1 AND status != 'sent';

-- name: apply-complaint-to-email-variants
-- Applies a complaint's bounce action to every subscriber whose address reaches
-- the same mailbox as the complaining one (see fork_canonical_email()), eg:
-- john.doe+news@gmail.com and johndoe@gmail.com. $1 = e-mail (or ''),
-- $2 = subscriber UUID (or ''), $3 = action: 'blocklist' blocklists and
-- unsubscribes them, 'unsubscribe' only unsubscribes them. It applies once the
-- complaints across all the variants reach the configured count ($4); $5 is 1
-- when the current complaint wasn't stored (the address isn't a subscriber).
WITH addr AS (
    SELECT fork_canonical_email(CASE WHEN $1 != '' THEN $1
        ELSE (SELECT email FROM subscribers WHERE CASE WHEN $2 != '' THEN uuid = $2::UUID ELSE FALSE END) END) AS canon
),
subs AS (
    SELECT id FROM subscribers WHERE fork_canonical_email(email) = (SELECT canon FROM addr)
        AND (
            SELECT COUNT(*) FROM bounces b JOIN subscribers s ON s.id = b.subscriber_id
            WHERE b.type = 'complaint' AND fork_canonical_email(s.email) = (SELECT canon FROM addr)
        ) + $5 >= $4
),
bl AS (
    UPDATE subscribers SET status = 'blocklisted', updated_at = NOW()
    WHERE $3 = 'blocklist' AND id IN (SELECT id FROM subs) AND status != 'blocklisted'
)
UPDATE subscriber_lists SET status = 'unsubscribed', updated_at = NOW()
    WHERE $3 IN ('blocklist', 'unsubscribe') AND subscriber_id IN (SELECT id FROM subs) AND status != 'unsubscribed';

-- name: is-email-suppressed
-- Whether an address must not be sent transactional e-mail: one of its variants
-- (fork_canonical_email()) complained, or the address hard-bounced.
SELECT EXISTS (
    SELECT 1 FROM subscribers s JOIN bounces b ON b.subscriber_id = s.id AND b.type = 'complaint'
    WHERE fork_canonical_email(s.email) = fork_canonical_email($1)
) OR EXISTS (
    SELECT 1 FROM subscribers s JOIN bounces b ON b.subscriber_id = s.id AND b.type = 'hard'
    WHERE LOWER(s.email) = LOWER($1)
);

-- name: get-sending-health
-- Counts of things stuck in sending, for /health/sending. $1 is how long
-- something may go without progress (an interval).
WITH recent_failures AS (
    -- Failed/uncertain e-mails of the last 15 minutes (partial index on status != 'sent').
    SELECT campaign_id, COUNT(*) AS n FROM campaign_messages
    WHERE status != 'sent' AND updated_at > NOW() - INTERVAL '15 minutes'
    GROUP BY campaign_id
)
SELECT
    -- Running for a while without any e-mail logged lately.
    (SELECT COUNT(*) FROM campaigns c
        WHERE c.status = 'running' AND c.started_at < NOW() - $1::INTERVAL
        AND COALESCE((SELECT MAX(updated_at) FROM campaign_messages WHERE campaign_id = c.id), c.started_at) < NOW() - $1::INTERVAL
    ) AS running_stalled,

    -- Scheduled, but not started long after its time.
    (SELECT COUNT(*) FROM campaigns WHERE status = 'scheduled' AND send_at < NOW() - $1::INTERVAL) AS scheduled_overdue,

    -- Paused in the last day with a failed last e-mail, ie: likely auto-paused for errors.
    (SELECT COUNT(*) FROM campaigns c
        WHERE c.status = 'paused' AND c.updated_at > NOW() - INTERVAL '1 day'
        AND (SELECT status FROM campaign_messages WHERE campaign_id = c.id ORDER BY updated_at DESC LIMIT 1) != 'sent'
    ) AS paused_failing,

    -- Automatic retries well past their time: the retry worker isn't running.
    -- Only retries it would claim (finished regular campaigns) count.
    (SELECT COUNT(*) FROM campaign_messages WHERE status = 'failed' AND retry_at < NOW() - $1::INTERVAL
        AND campaign_id IN (SELECT id FROM campaigns WHERE status = 'finished' AND type = 'regular')) AS retries_overdue,

    -- Mostly failing lately: at least 10 failures in 15 minutes, more than sends.
    (SELECT CASE WHEN f.n >= 10 AND f.n > (
            SELECT COUNT(*) FROM campaign_messages
            WHERE campaign_id IN (SELECT campaign_id FROM recent_failures)
                AND status = 'sent' AND updated_at > NOW() - INTERVAL '15 minutes'
        ) THEN 1 ELSE 0 END
        FROM (SELECT COALESCE(SUM(n), 0) AS n FROM recent_failures) f
    ) AS failing;
