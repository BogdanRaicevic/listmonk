package manager

import (
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLogStore records message log batches. The embedded Store is nil; only
// RecordCampaignMessages is used by the writer.
type fakeLogStore struct {
	Store

	mu      sync.Mutex
	batches [][]MessageLog
}

func (f *fakeLogStore) RecordCampaignMessages(logs []MessageLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, append([]MessageLog(nil), logs...))
	return nil
}

func (f *fakeLogStore) rows() []MessageLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []MessageLog
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func newTestLogManager(s Store) *Manager {
	m := &Manager{store: s, log: log.New(io.Discard, "", 0), msgLog: newMessageLog()}
	go m.runMessageLog()
	return m
}

func TestMessageLogFlushesOnStop(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)

	for i := 1; i <= 1200; i++ {
		var err error
		if i%100 == 0 {
			err = errors.New("550 mailbox unavailable")
		}
		m.logMessage(7, i, err)
	}
	m.stopMessageLog()

	rows := f.rows()
	if len(rows) != 1200 {
		t.Fatalf("want 1200 rows, got %d", len(rows))
	}
	for _, b := range f.batches {
		if len(b) > msgLogBatchSize {
			t.Fatalf("batch of %d exceeds max %d", len(b), msgLogBatchSize)
		}
	}

	var failed int
	for _, r := range rows {
		if r.CampaignID != 7 {
			t.Fatalf("wrong campaign ID %d", r.CampaignID)
		}
		switch r.Status {
		case MessageFailed:
			failed++
			if r.Error != "550 mailbox unavailable" {
				t.Fatalf("wrong error %q", r.Error)
			}
		case MessageSent:
			if r.Error != "" {
				t.Fatalf("sent row has error %q", r.Error)
			}
		default:
			t.Fatalf("unexpected status %q", r.Status)
		}
	}
	if failed != 12 {
		t.Fatalf("want 12 failed, got %d", failed)
	}
}

func TestMessageLogFlushesPeriodically(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)
	defer m.stopMessageLog()

	m.logMessage(1, 1, nil)

	deadline := time.Now().Add(3 * msgLogInterval)
	for time.Now().Before(deadline) {
		if len(f.rows()) == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("row not flushed within %s", 3*msgLogInterval)
}

func TestMessageLogTruncatesErrors(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)

	m.logMessage(1, 1, errors.New(strings.Repeat("x", 2000)))
	m.stopMessageLog()

	rows := f.rows()
	if len(rows) != 1 || len(rows[0].Error) != msgLogMaxErrLen {
		t.Fatalf("want 1 row with %d-char error, got %+v", msgLogMaxErrLen, rows)
	}
}

func TestMessageLogAfterStopDoesNotBlock(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)
	m.stopMessageLog()

	done := make(chan struct{})
	go func() {
		for i := 0; i < msgLogQueueSize+10; i++ {
			m.logMessage(1, i, nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("logMessage blocked after stop")
	}
}
