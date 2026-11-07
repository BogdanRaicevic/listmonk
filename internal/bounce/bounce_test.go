package bounce

import (
	"errors"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
)

// Fork: bounces are recorded before Record returns, so a webhook can answer the
// provider with an error (and have it retry) if storing fails.
func TestRecordIsSynchronous(t *testing.T) {
	var got []models.Bounce
	m := &Manager{opt: Opt{RecordBounceCB: func(b models.Bounce) error {
		got = append(got, b)
		return nil
	}}}

	if err := m.Record(models.Bounce{Email: "a@example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("bounce not recorded before Record returned")
	}
	if got[0].CreatedAt.IsZero() || time.Since(got[0].CreatedAt) > time.Minute {
		t.Fatalf("CreatedAt not defaulted to now: %v", got[0].CreatedAt)
	}

	ts := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	_ = m.Record(models.Bounce{Email: "b@example.com", CreatedAt: ts})
	if !got[1].CreatedAt.Equal(ts) {
		t.Fatalf("provider timestamp not kept: %v", got[1].CreatedAt)
	}
}

func TestRecordReturnsStoreError(t *testing.T) {
	want := errors.New("db down")
	m := &Manager{opt: Opt{RecordBounceCB: func(models.Bounce) error { return want }}}

	if err := m.Record(models.Bounce{Email: "a@example.com"}); !errors.Is(err, want) {
		t.Fatalf("want store error, got %v", err)
	}
}

// Fork: a mailbox bounce that can never be recorded is dropped (nil, so the
// message is deleted); a transient error is returned (the message is kept).
func TestRecordFromMailbox(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantNil bool
	}{
		{"recorded", nil, true},
		{"invalid bounce type (400)", echo.NewHTTPError(http.StatusBadRequest, "invalid type"), true},
		{"malformed UUID (22P02)", &pq.Error{Code: "22P02", Message: "invalid input syntax for type uuid"}, true},
		{"DB down", errors.New("dial tcp: connection refused"), false},
		{"server error (500)", echo.NewHTTPError(http.StatusInternalServerError, "oops"), false},
	}

	for _, c := range cases {
		m := &Manager{
			opt: Opt{RecordBounceCB: func(models.Bounce) error { return c.err }},
			log: log.New(io.Discard, "", 0),
		}
		if got := m.recordFromMailbox(models.Bounce{Email: "a@example.com"}); (got == nil) != c.wantNil {
			t.Errorf("%s: got %v, want nil=%v", c.name, got, c.wantNil)
		}
	}
}
