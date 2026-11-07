package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

// V7_0_1_Fork4 adds fork_canonical_email(), which maps the variants of an e-mail
// address that reach the same mailbox to one form, and an index on it (fork-only).
func V7_0_1_Fork4(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf, lo *log.Logger) error {
	if _, err := db.Exec(forkCanonicalEmailSQL); err != nil {
		return err
	}

	return nil
}

// forkCanonicalEmailSQL lowercases an address, drops a +tag from its local part,
// and for Gmail drops the dots and maps googlemail.com to gmail.com. eg:
// John.Doe+news@GoogleMail.com -> johndoe@gmail.com.
const forkCanonicalEmailSQL = `
CREATE OR REPLACE FUNCTION fork_canonical_email(e TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
    SELECT CASE WHEN x.d = '' OR x.l = '' THEN x.e ELSE
        (CASE WHEN x.d IN ('gmail.com', 'googlemail.com') THEN REPLACE(x.l, '.', '') ELSE x.l END)
        || '@' || (CASE WHEN x.d = 'googlemail.com' THEN 'gmail.com' ELSE x.d END)
    END
    FROM (
        SELECT LOWER(TRIM(e)) AS e,
            SPLIT_PART(SPLIT_PART(LOWER(TRIM(e)), '@', 1), '+', 1) AS l,
            SPLIT_PART(LOWER(TRIM(e)), '@', 2) AS d
    ) x
$fn$;
CREATE INDEX IF NOT EXISTS idx_subs_canonical_email ON subscribers(fork_canonical_email(email));
`
