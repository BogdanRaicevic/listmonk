package bounce

// Fork: mailbox bounces that can never be recorded are dropped, not kept.

import (
	"errors"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
)

// recordFromMailbox records a bounce read from the mailbox. A bounce that can
// never be recorded (invalid data: a 4xx, or a Postgres data exception such as a
// malformed UUID) is logged and reported as done, so that its message is deleted
// instead of being kept and retried on every scan. Other errors (eg: the DB is
// down) are returned, so the message is kept for the next scan.
func (m *Manager) recordFromMailbox(b models.Bounce) error {
	err := m.Record(b)
	if err == nil || !isPermanentRecordErr(err) {
		return err
	}

	m.log.Printf("dropping mailbox bounce that can't be recorded (%s): %v", b.Email, err)
	return nil
}

// isPermanentRecordErr reports whether recording a bounce failed because of the
// bounce itself, so retrying can't help.
func isPermanentRecordErr(err error) bool {
	if code := echo.StatusCode(err); code >= 400 && code < 500 {
		return true
	}

	// Class 22: data exception (eg: invalid input syntax for type uuid).
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code.Class() == "22"
}
