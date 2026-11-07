package manager

import (
	"testing"
	"time"
)

func TestSlidingWindowWait(t *testing.T) {
	m := &Manager{}
	if m.SlidingWindowWait() {
		t.Fatal("no wait recorded, but reported waiting")
	}

	m.slidingUntil.Store(time.Now().Add(time.Minute).UnixNano())
	if !m.SlidingWindowWait() {
		t.Fatal("wait in progress not reported")
	}

	m.slidingUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if m.SlidingWindowWait() {
		t.Fatal("finished wait still reported")
	}
}

// Retries are held back while the sliding window limit pauses sending. The
// fake store's Store is nil, so claiming retries would panic.
func TestNoRetriesDuringSlidingWindowWait(t *testing.T) {
	m := &Manager{store: &fakeLogStore{}}
	m.slidingUntil.Store(time.Now().Add(time.Minute).UnixNano())

	n, err := m.processRetries()
	if n != 0 || err != nil {
		t.Fatalf("want no retries during the wait, got %d, %v", n, err)
	}
}
