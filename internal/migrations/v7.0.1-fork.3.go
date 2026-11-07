package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V7_0_1_Fork3 adds retry scheduling to the campaign message log (fork-only):
// failed sends with a retry_at are resent once it passes.
func V7_0_1_Fork3(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	if _, err := db.Exec(`
        ALTER TABLE campaign_messages ADD COLUMN IF NOT EXISTS retry_at TIMESTAMP WITH TIME ZONE NULL;
        CREATE INDEX IF NOT EXISTS idx_camp_msgs_retry ON campaign_messages(retry_at) WHERE retry_at IS NOT NULL;
	`); err != nil {
		return err
	}

	return nil
}
