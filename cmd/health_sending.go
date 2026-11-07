package main

// Fork: /health/sending, for monitoring whether mail is stuck.

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
)

const (
	sendingHealthDefaultMinutes = 30
	sendingHealthCacheFor       = time.Minute
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
	Minutes          int `json:"minutes"`

	checkedAt time.Time
}

var (
	sendingHealthCache   = map[int]sendingHealth{}
	sendingHealthCacheMu sync.Mutex
)

// SendingHealth reports whether mail is stuck: running campaigns not sending,
// scheduled campaigns not starting, campaigns paused after failures, overdue
// retries, or sends mostly failing. It answers 503 if anything is stuck, so an
// uptime monitor can alert on the status code alone. ?minutes= sets how long
// something may go without progress (default 30, 5..1440). Results are cached
// for a minute.
func (a *App) SendingHealth(c *echo.Context) error {
	minutes := sendingHealthDefaultMinutes
	if v, err := strconv.Atoi(c.QueryParam("minutes")); err == nil {
		minutes = min(max(v, 5), 1440)
	}

	sendingHealthCacheMu.Lock()
	defer sendingHealthCacheMu.Unlock()

	h, ok := sendingHealthCache[minutes]
	if !ok || time.Since(h.checkedAt) >= sendingHealthCacheFor {
		var out sendingHealth
		if err := a.queries.GetSendingHealth.Get(&out, fmt.Sprintf("%d minutes", minutes)); err != nil {
			a.log.Printf("error checking sending health: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "error checking sending health")
		}

		// The sliding window rate limit pauses sending on purpose.
		if a.manager.SlidingWindowWait() {
			out.RunningStalled = 0
		}

		out.Minutes = minutes
		out.Stuck = out.RunningStalled + out.ScheduledOverdue + out.PausedFailing + out.RetriesOverdue + out.Failing
		out.checkedAt = time.Now()
		sendingHealthCache[minutes] = out
		h = out
	}

	code := http.StatusOK
	if h.Stuck > 0 {
		code = http.StatusServiceUnavailable
	}
	return c.JSON(code, okResp{h})
}
