package manager

// Fork: signals for /health/sending.

import "time"

// SlidingWindowWait reports whether sending is currently paused on purpose by
// the sliding window rate limit (so a running campaign not sending isn't stuck).
func (m *Manager) SlidingWindowWait() bool {
	return time.Now().UnixNano() < m.slidingUntil.Load()
}
