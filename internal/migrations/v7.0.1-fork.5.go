package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V7_0_1_Fork5 adds the campaign rules that skip subscribers who were e-mailed
// too often recently or ignored their last e-mails, and the functions behind
// them (fork-only).
func V7_0_1_Fork5(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	if _, err := db.Exec(`
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS contact_window_days INT NOT NULL DEFAULT 30;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS skip_contacted_min INT NOT NULL DEFAULT 0;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS skip_unopened_last INT NOT NULL DEFAULT 0;
` + forkContactSQL); err != nil {
		return err
	}

	return nil
}

// forkContactSQL is repeated in schema.sql.
const forkContactSQL = `
CREATE OR REPLACE FUNCTION fork_recent_sends(sub INT, days INT, camp INT) RETURNS INT
LANGUAGE sql STABLE PARALLEL SAFE AS $fn$
    SELECT COUNT(*)::INT FROM campaign_messages
    WHERE subscriber_id = sub AND status = 'sent' AND campaign_id != camp
        AND updated_at > NOW() - MAKE_INTERVAL(days => days)
$fn$;

CREATE OR REPLACE FUNCTION fork_ignored_last(sub INT, n INT, camp INT) RETURNS BOOLEAN
LANGUAGE sql STABLE PARALLEL SAFE AS $fn$
    WITH m AS (
        SELECT campaign_id FROM campaign_messages
        WHERE subscriber_id = sub AND status = 'sent' AND campaign_id != camp
        ORDER BY updated_at DESC LIMIT n
    )
    SELECT n > 0 AND (SELECT COUNT(*) FROM m) = n
        AND NOT EXISTS (SELECT 1 FROM campaign_views v JOIN m USING (campaign_id) WHERE v.subscriber_id = sub)
        AND NOT EXISTS (SELECT 1 FROM link_clicks c JOIN m USING (campaign_id) WHERE c.subscriber_id = sub)
$fn$;
`
