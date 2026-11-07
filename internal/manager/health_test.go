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
