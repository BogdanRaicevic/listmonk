package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V7_0_1_Fork2 adds campaign_messages, a per-subscriber log of campaign e-mails
// and their delivery outcome (fork-only).
func V7_0_1_Fork2(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS campaign_messages (
            campaign_id    INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
            subscriber_id  INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE ON UPDATE CASCADE,
            status         TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'uncertain')),
            error          TEXT NOT NULL DEFAULT '',
            attempts       INTEGER NOT NULL DEFAULT 1,
            created_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            updated_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            PRIMARY KEY (campaign_id, subscriber_id)
        );
        CREATE INDEX IF NOT EXISTS idx_camp_msgs_sub ON campaign_messages(subscriber_id);
        CREATE INDEX IF NOT EXISTS idx_camp_msgs_unsent ON campaign_messages(campaign_id, status) WHERE status != 'sent';
	`); err != nil {
		return err
	}

	return nil
}
