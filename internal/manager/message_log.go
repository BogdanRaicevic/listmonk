package manager

// Fork: per-subscriber log of campaign e-mails and their delivery outcome.

import (
	"strings"
	"sync"
	"time"
)

// Delivery outcomes recorded in the campaign message log.
const (
	MessageSent   = "sent"
	MessageFailed = "failed"

	// MessageUncertain is a failure that may have been delivered anyway (the
	// connection broke mid-send). It's never resent automatically.
	MessageUncertain = "uncertain"
)

const (
	msgLogQueueSize = 10000
	msgLogBatchSize = 500
	msgLogInterval  = time.Second
	msgLogMaxErrLen = 500
)

// MessageLog is the delivery outcome of one campaign e-mail to one subscriber.
type MessageLog struct {
	CampaignID   int
	SubscriberID int
	Status       string
	Error        string

	// Temporary is true for a failure that is safe and worth retrying: the
	// e-mail certainly wasn't delivered and the cause may go away.
	Temporary bool
}

// messageLog buffers outcomes and writes them to the store in batches so that
// sending isn't slowed down by a DB write per e-mail.
type messageLog struct {
	q        chan MessageLog
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newMessageLog() *messageLog {
	return &messageLog{
		q:    make(chan MessageLog, msgLogQueueSize),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// logMessage queues a campaign e-mail's outcome for the log.
func (m *Manager) logMessage(campID, subID int, err error) {
	l := MessageLog{CampaignID: campID, SubscriberID: subID, Status: MessageSent}
	if err != nil {
		l.Status = MessageFailed
		if isUncertainSendErr(err) {
			l.Status = MessageUncertain
		}
		l.Temporary = isTemporarySendErr(err)
		l.Error = err.Error()
		if len(l.Error) > msgLogMaxErrLen {
			l.Error = l.Error[:msgLogMaxErrLen]
		}
		// Postgres rejects invalid UTF-8 (eg: a Latin-1 SMTP reply, or a character
		// cut above) and NUL bytes, which would fail the whole batch.
		l.Error = strings.ToValidUTF8(strings.ReplaceAll(l.Error, "\x00", ""), "?")
	}

	select {
	case <-m.msgLog.stop:
		// Shutting down; the writer has stopped reading.
	case m.msgLog.q <- l:
	}
}

// runMessageLog writes queued outcomes to the store until stopMessageLog() is called.
func (m *Manager) runMessageLog() {
	defer close(m.msgLog.done)

	var (
		batch = make([]MessageLog, 0, msgLogBatchSize)
		tick  = time.NewTicker(msgLogInterval)
	)
	defer tick.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := m.store.RecordCampaignMessages(batch); err != nil {
			m.log.Printf("error recording %d campaign message outcomes: %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case l := <-m.msgLog.q:
			batch = append(batch, l)
			if len(batch) >= msgLogBatchSize {
				flush()
			}

		case <-tick.C:
			flush()

		case <-m.msgLog.stop:
			// Drain whatever is queued and write it out.
			for {
				select {
				case l := <-m.msgLog.q:
					batch = append(batch, l)
					if len(batch) >= msgLogBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// stopMessageLog stops the writer after flushing queued outcomes, waiting up to
// 2 seconds for it (a reload force-restarts after 3).
//
// Known limits, accepted as such: Close(), and so this flush, only runs on a
// reload (SIGHUP), so up to about a second of outcomes is lost when the process
// is stopped (eg: SIGTERM from docker/systemd), and a batch whose write fails is
// dropped. A lost "sent" row can let a resume send that e-mail again; a lost
// temporary failure isn't retried automatically.
func (m *Manager) stopMessageLog() {
	m.msgLog.stopOnce.Do(func() { close(m.msgLog.stop) })

	select {
	case <-m.msgLog.done:
	case <-time.After(2 * time.Second):
		m.log.Printf("timed out flushing the campaign message log")
	}
}
