package manager

// Fork: automatic retries of temporarily failed campaign e-mails, and manual
// resends, driven by campaign_messages.retry_at.

import (
	"errors"
	"io"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/knadh/listmonk/models"
)

const (
	// RetryBackoff is the wait before the first automatic retry of a temporary
	// failure; each further retry waits RetryBackoff * attempts.
	RetryBackoff = 10 * time.Minute

	// RetryMaxAttempts is how many times in all (the first send included) an
	// e-mail is tried before temporary failures stop being retried automatically.
	RetryMaxAttempts = 4

	retryInterval  = 30 * time.Second
	retryBatchSize = 100
)

// RetryTarget is a failed campaign e-mail due for a retry.
type RetryTarget struct {
	CampaignID int `db:"campaign_id"`
	models.Subscriber

	// Eligible is whether the subscriber should still get the campaign
	// (not blocklisted, still subscribed, not excluded).
	Eligible bool `db:"eligible"`
}

// isTemporarySendErr reports whether a send error certainly means the e-mail
// wasn't delivered and may succeed later: an SMTP 4xx reply, or no connection
// to the SMTP server at all. Anything else (5xx rejections, or connections that
// broke mid-send, which may have delivered) isn't retried automatically.
func isTemporarySendErr(err error) bool {
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		return smtpErr.Code >= 400 && smtpErr.Code < 500
	}

	// Couldn't connect to the SMTP server.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// No SMTP server configured/available for the messenger, or no free
	// connection in smtppool's pool in time (both before anything is sent).
	msg := err.Error()
	return strings.Contains(msg, "no SMTP servers available") || strings.Contains(msg, "timed out waiting for free conn")
}

// isUncertainSendErr reports whether a send error means the e-mail may have
// been delivered anyway: the connection to the SMTP server broke or timed out
// after it was established (eg: EOF, reset, broken pipe, read/write timeout),
// possibly after the server had accepted the message. Such e-mails are never
// resent automatically, nor on a campaign resume, to avoid duplicates.
// Failures to connect and SMTP replies are certain, not uncertain.
func isUncertainSendErr(err error) bool {
	if err == nil || isTemporarySendErr(err) {
		return false
	}

	// The server replied (eg: 5xx): certainly not delivered.
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	// Any other network error on an established connection (read/write, timeouts).
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	msg := err.Error()
	return strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe")
}

// keepForResume reports whether a failed campaign e-mail should be kept for a
// resend when its campaign is stopped (paused) and later resumed. Uncertain
// sends may have been delivered, so they aren't.
func keepForResume(err error) bool {
	return err != nil && !isUncertainSendErr(err)
}

// runRetries periodically resends failed campaign e-mails whose retry is due.
func (m *Manager) runRetries() {
	t := time.NewTicker(retryInterval)
	defer t.Stop()

	for range t.C {
		for {
			n, err := m.processRetries()
			if err != nil {
				m.log.Printf("error processing campaign e-mail retries: %v", err)
				break
			}
			if n < retryBatchSize {
				break
			}
		}
	}
}

// processRetries resends one batch of due retries and returns how many it claimed.
func (m *Manager) processRetries() (int, error) {
	// Don't resend while the sliding window limit is pausing sends.
	if m.SlidingWindowWait() {
		return 0, nil
	}

	targets, err := m.store.ClaimCampaignRetries(retryBatchSize)
	if err != nil || len(targets) == 0 {
		return 0, err
	}

	var (
		camps = map[int]*models.Campaign{}
		sent  = map[int]int{}
		gap   time.Duration
	)
	if m.cfg.MessageRate > 0 {
		gap = time.Second / time.Duration(m.cfg.MessageRate)
	}
	for _, t := range targets {
		if !t.Eligible {
			if err := m.store.ClearCampaignRetry(t.CampaignID, t.ID); err != nil {
				m.log.Printf("error clearing retry (campaign %d, subscriber %d): %v", t.CampaignID, t.ID, err)
			}
			continue
		}

		c, ok := camps[t.CampaignID]
		if !ok {
			if c, err = m.loadRetryCampaign(t.CampaignID); err != nil {
				// Leave the claimed retries to be picked up again later.
				m.log.Printf("error preparing campaign %d for retries: %v", t.CampaignID, err)
			}
			camps[t.CampaignID] = c
		}
		if c == nil {
			continue
		}

		msg, err := m.NewCampaignMessage(c, t.Subscriber)
		if err != nil {
			m.logMessage(c.ID, t.ID, err)
			continue
		}

		err = m.messengers[c.Messenger].Push(m.campaignMessageOut(msg))
		if err != nil {
			m.log.Printf("error resending message in campaign %s: subscriber %d: %v", c.Name, t.ID, err)
		} else {
			sent[c.ID]++
		}
		m.logMessage(c.ID, t.ID, err)

		time.Sleep(gap)
	}

	// Add successful resends to the campaigns' sent counts.
	m.countsMut.Lock()
	for id, n := range sent {
		if err := m.store.UpdateCampaignCounts(id, 0, n, 0); err != nil {
			m.log.Printf("error updating sent count of campaign %d after retries: %v", id, err)
		}
	}
	m.countsMut.Unlock()

	return len(targets), nil
}

// loadRetryCampaign fetches and compiles a campaign for resending, as newPipe does.
func (m *Manager) loadRetryCampaign(id int) (*models.Campaign, error) {
	c, err := m.store.GetCampaign(id)
	if err != nil {
		return nil, err
	}
	if _, ok := m.messengers[c.Messenger]; !ok {
		return nil, errors.New("unknown messenger " + c.Messenger)
	}
	if err := m.LoadInlineImages(c); err != nil {
		return nil, err
	}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		return nil, err
	}
	if err := m.attachMedia(c); err != nil {
		return nil, err
	}
	return c, nil
}
