package bounce

import (
	"errors"
	"testing"
	"time"

	"github.com/knadh/listmonk/models"
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
