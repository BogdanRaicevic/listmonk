package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V7_0_1_Fork6 re-runs V7_0_0 (fork-only). Upstream keeps adding to the
// unreleased v7.0.0 migration (eg: media tags), which databases already past
// v7.0.0 never pick up. Every statement in it is safe to run again.
func V7_0_1_Fork6(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	return V7_0_0(db, fs, ko, lo)
}
