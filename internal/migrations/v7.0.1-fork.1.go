package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V7_0_1_Fork1(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	// Campaign exclusion lists.
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS campaign_exclude_lists (
            id           BIGSERIAL PRIMARY KEY,
            campaign_id  INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE ON UPDATE CASCADE,
            list_id      INTEGER NULL REFERENCES lists(id) ON DELETE SET NULL ON UPDATE CASCADE,
            list_name    TEXT NOT NULL DEFAULT ''
        );
        CREATE UNIQUE INDEX IF NOT EXISTS campaign_exclude_lists_campaign_id_list_id_idx ON campaign_exclude_lists (campaign_id, list_id);
        CREATE INDEX IF NOT EXISTS idx_camp_excl_lists_camp_id ON campaign_exclude_lists(campaign_id);
        CREATE INDEX IF NOT EXISTS idx_camp_excl_lists_list_id ON campaign_exclude_lists(list_id);
	`); err != nil {
		return err
	}

	return nil
}
