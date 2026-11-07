-- name: record-bounce
-- Insert a bounce and count the bounces for the subscriber and either unsubscribe them,
WITH sub AS (
    -- (fork) match the address case-insensitively (uses idx_subs_email on LOWER(email)).
    SELECT id, status FROM subscribers WHERE CASE WHEN $1 != '' THEN uuid = $1::UUID ELSE LOWER(email) = LOWER($2) END
),
camp AS (
    SELECT id FROM campaigns WHERE $3 != '' AND uuid = $3::UUID
),
-- (fork) a provider re-delivering a bounce (eg: after a failed answer) has the same
-- subscriber, campaign, type, source and time, or the same message ID (mailbox
-- bounces without a Date: header get the scan time). Record and act on it only once.
dup AS (
    SELECT 1 FROM bounces WHERE subscriber_id = (SELECT id FROM sub)
        AND campaign_id IS NOT DISTINCT FROM (SELECT id FROM camp)
        AND type = $4 AND source = $5
        AND (created_at = $7 OR (
            COALESCE($6::JSONB->>'message_id', '') != '' AND meta->>'message_id' = $6::JSONB->>'message_id'
        ))
    LIMIT 1
),
num AS (
    -- Add a +1 to include the current insertion that is happening.
    SELECT COUNT(*) + 1 AS num FROM bounces WHERE subscriber_id = (SELECT id FROM sub) AND type = $4
),
-- block1 and block2 will run when $8 = 'blocklist' and the number of bounces exceed $8.
block1 AS (
    UPDATE subscribers SET status='blocklisted'
    WHERE $9 = 'blocklist' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
        AND NOT EXISTS (SELECT 1 FROM dup)
),
block2 AS (
    UPDATE subscriber_lists SET status='unsubscribed'
    WHERE $9 = 'unsubscribe' AND (SELECT num FROM num) >= $8 AND subscriber_id = (SELECT id FROM sub) AND (SELECT status FROM sub) != 'blocklisted'
        AND NOT EXISTS (SELECT 1 FROM dup)
),
bounce AS (
    -- Record the bounce if the subscriber is not already blocklisted;
    -- (fork) complaints and hard bounces are recorded for blocklisted subscribers
    -- too, as "unsubscribe from all" blocklists and a complaint often follows it.
    INSERT INTO bounces (subscriber_id, campaign_id, type, source, meta, created_at)
    SELECT (SELECT id FROM sub), (SELECT id FROM camp), $4, $5, $6, $7
    WHERE NOT EXISTS (SELECT 1 WHERE ((SELECT status FROM sub) = 'blocklisted' AND $4 = 'soft') OR (SELECT num FROM num) > $8)
        AND NOT EXISTS (SELECT 1 FROM dup)
)
-- This delete  will only run when $9 = 'delete' and the number of bounces exceed $8.
DELETE FROM subscribers
    WHERE $9 = 'delete' AND (SELECT num FROM num) >= $8 AND id = (SELECT id FROM sub)
        AND NOT EXISTS (SELECT 1 FROM dup);

-- name: query-bounces
SELECT COUNT(*) OVER () AS total,
    bounces.id,
    bounces.type,
    bounces.source,
    bounces.meta,
    bounces.created_at,
    bounces.subscriber_id,
    subscribers.uuid AS subscriber_uuid,
    subscribers.email AS email,
    subscribers.name AS subscriber_name,
    subscribers.status as subscriber_status,
    (
        CASE WHEN bounces.campaign_id IS NOT NULL
        THEN JSON_BUILD_OBJECT('id', bounces.campaign_id, 'name', campaigns.name)
        ELSE NULL END
    ) AS campaign
FROM bounces
LEFT JOIN subscribers ON (subscribers.id = bounces.subscriber_id)
LEFT JOIN campaigns ON (campaigns.id = bounces.campaign_id)
WHERE ($1 = 0 OR bounces.id = $1)
    AND ($2 = 0 OR bounces.campaign_id = $2)
    AND ($3 = 0 OR bounces.subscriber_id = $3)
    AND ($4 = '' OR bounces.source = $4)
    AND ($5 = '' OR bounces.type = $5::bounce_type)
    AND ($8 OR EXISTS (
        SELECT 1 FROM subscriber_lists
        WHERE subscriber_id = bounces.subscriber_id AND list_id = ANY($9::INT[])
    ))
ORDER BY %order% OFFSET $6 LIMIT (CASE WHEN $7 < 1 THEN NULL ELSE $7 END);

-- name: delete-bounces
DELETE FROM bounces WHERE (CASE WHEN $2 THEN
    ($3 = 0 OR campaign_id = $3)
    AND ($4 = '' OR source = $4)
    AND ($5 = '' OR type = $5::bounce_type)
ELSE id = ANY($1) END)
    AND ($6 OR EXISTS (
        SELECT 1 FROM subscriber_lists
        WHERE subscriber_id = bounces.subscriber_id AND list_id = ANY($7::INT[])
    ));

-- name: delete-bounces-by-subscriber
WITH sub AS (
    SELECT id FROM subscribers WHERE CASE WHEN $1 > 0 THEN id = $1 ELSE uuid = $2 END
)
DELETE FROM bounces WHERE subscriber_id = (SELECT id FROM sub);

-- name: blocklist-bounced-subscribers
WITH subs AS (
    SELECT subscriber_id FROM bounces
    WHERE (CASE WHEN CARDINALITY($1::INT[]) > 0 THEN id = ANY($1)
    ELSE ($2 = 0 OR campaign_id = $2)
        AND ($3 = '' OR source = $3)
        AND ($4 = '' OR type = $4::bounce_type)
    END)
    AND ($5 OR EXISTS (
        SELECT 1 FROM subscriber_lists
        WHERE subscriber_id = bounces.subscriber_id AND list_id = ANY($6::INT[])
    ))
),
b AS (
    UPDATE subscribers SET status='blocklisted', updated_at=NOW()
    WHERE id = ANY(SELECT subscriber_id FROM subs)
)
UPDATE subscriber_lists SET status='unsubscribed', updated_at=NOW()
    WHERE subscriber_id = ANY(SELECT subscriber_id FROM subs);

