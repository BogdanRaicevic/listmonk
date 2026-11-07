package main

// Fork: /health/sending, for monitoring whether mail is stuck.

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
)

const (
	// sendingHealthStall is how long something may go without progress.
	sendingHealthStall    = "30 minutes"
	sendingHealthCacheFor = time.Minute
)

// sendingHealth is what /health/sending reports: counts only, so that it can be
// public (eg: for an uptime monitor) without revealing campaigns or addresses.
type sendingHealth struct {
	Stuck            int `json:"stuck"`
	RunningStalled   int `json:"running_stalled" db:"running_stalled"`
	ScheduledOverdue int `json:"scheduled_overdue" db:"scheduled_overdue"`
	PausedFailing    int `json:"paused_failing" db:"paused_failing"`
	RetriesOverdue   int `json:"retries_overdue" db:"retries_overdue"`
	Failing          int `json:"failing" db:"failing"`

	checkedAt time.Time
}

var (
	sendingHealthCache   sendingHealth
	sendingHealthCacheMu sync.Mutex
)

// SendingHealth reports whether mail is stuck: running campaigns not sending,
// scheduled campaigns not starting, campaigns paused after failures, overdue
// retries, or sends mostly failing. It answers 503 if anything is stuck, so an
// uptime monitor can alert on the status code alone. Results are cached for a
// minute.
func (a *App) SendingHealth(c *echo.Context) error {
	sendingHealthCacheMu.Lock()
	defer sendingHealthCacheMu.Unlock()

	h := sendingHealthCache
	if time.Since(h.checkedAt) >= sendingHealthCacheFor {
		if err := a.queries.GetSendingHealth.Get(&h, sendingHealthStall); err != nil {
			a.log.Printf("error checking sending health: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "error checking sending health")
		}

		// The sliding window rate limit pauses sending on purpose.
		if a.manager.SlidingWindowWait() {
			h.RunningStalled = 0
		}

		h.Stuck = h.RunningStalled + h.ScheduledOverdue + h.PausedFailing + h.RetriesOverdue + h.Failing
		h.checkedAt = time.Now()
		sendingHealthCache = h
	}

	code := http.StatusOK
	if h.Stuck > 0 {
		code = http.StatusServiceUnavailable
	}
	return c.JSON(code, okResp{h})
}
