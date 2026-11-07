package bounce

import (
	"errors"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/bounce/mailbox"
	"github.com/knadh/listmonk/internal/bounce/webhooks"
	"github.com/knadh/listmonk/models"
)

// Mailbox represents a POP/IMAP mailbox client that can scan messages and pass
// their bounces to record. A message is deleted from the mailbox only after
// record succeeds for all its bounces (fork: durable bounces).
type Mailbox interface {
	Scan(limit int, record func(models.Bounce) error) error
}

// Opt represents bounce processing options.
type Opt struct {
	MailboxEnabled  bool        `json:"mailbox_enabled"`
	MailboxType     string      `json:"mailbox_type"`
	Mailbox         mailbox.Opt `json:"mailbox"`
	WebhooksEnabled bool        `json:"webhooks_enabled"`
	SES             struct {
		Enabled   bool
		TopicARNs []string
	}
	AzureEnabled            bool   `json:"azure_enabled"`
	AzureSharedSecret       string `json:"azure_shared_secret"`
	AzureSharedSecretHeader string `json:"azure_shared_secret_header"`
	Sendgrid                struct {
		Enabled bool
		Key     string
	}
	Postmark struct {
		Enabled  bool
		Username string
		Password string
	}
	ForwardEmail struct {
		Enabled bool
		Key     string
	}
	Lettermint struct {
		Enabled bool
		Key     string
	}

	RecordBounceCB func(models.Bounce) error
}

// Manager handles e-mail bounces.
type Manager struct {
	mailbox      Mailbox
	SES          *webhooks.SES
	Azure        *webhooks.Azure
	Sendgrid     *webhooks.Sendgrid
	Postmark     *webhooks.Postmark
	Forwardemail *webhooks.Forwardemail
	Lettermint   *webhooks.Lettermint
	queries      *Queries
	opt          Opt
	log          *log.Logger
}

// Queries contains the queries.
type Queries struct {
	DB          *sqlx.DB
	RecordQuery *sqlx.Stmt
}

// New returns a new instance of the bounce manager.
func New(opt Opt, q *Queries, lo *log.Logger) (*Manager, error) {
	m := &Manager{
		opt:     opt,
		queries: q,
		log:     lo,
	}

	// Is there a mailbox?
	if opt.MailboxEnabled {
		switch opt.MailboxType {
		case "pop":
			m.mailbox = mailbox.NewPOP(opt.Mailbox, lo)
		default:
			return nil, errors.New("unknown bounce mailbox type")
		}
	}

	if opt.WebhooksEnabled {
		if opt.SES.Enabled {
			m.SES = webhooks.NewSES(opt.SES.TopicARNs)
		}
		if opt.AzureEnabled {
			m.Azure = webhooks.NewAzure(opt.AzureSharedSecret, opt.AzureSharedSecretHeader)
		}

		if opt.Sendgrid.Enabled {
			sg, err := webhooks.NewSendgrid(opt.Sendgrid.Key)
			if err != nil {
				lo.Printf("error initializing sendgrid webhooks: %v", err)
			} else {
				m.Sendgrid = sg
			}
		}

		if opt.Postmark.Enabled {
			m.Postmark = webhooks.NewPostmark(opt.Postmark.Username, opt.Postmark.Password)
		}

		if opt.ForwardEmail.Enabled {
			fe := webhooks.NewForwardemail([]byte(opt.ForwardEmail.Key))
			m.Forwardemail = fe
		}

		if opt.Lettermint.Enabled {
			m.Lettermint = webhooks.NewLettermint([]byte(opt.Lettermint.Key))
		}
	}

	return m, nil
}

// Run is a blocking function that scans the bounce mailbox, if one is enabled.
// Webhook bounces are recorded directly by Record().
func (m *Manager) Run() {
	if m.opt.MailboxEnabled {
		m.runMailboxScanner()
	}
}

// runMailboxScanner runs a blocking loop that scans the mailbox at given intervals.
func (m *Manager) runMailboxScanner() {
	for {
		m.log.Printf("scanning bounce mailbox %s", m.opt.Mailbox.Host)
		if err := m.mailbox.Scan(1000, m.Record); err != nil {
			m.log.Printf("error scanning bounce mailbox: %v", err)
		}

		time.Sleep(m.opt.Mailbox.ScanInterval)
	}
}

// Record records a new bounce event given the subscriber's email or UUID.
//
// Fork: bounces used to be queued in memory and written to the DB later, after
// the provider (webhook) had been answered or the message deleted from the
// mailbox, so a restart, crash or DB error lost them. They are now written
// before returning, and an error is returned if that fails so the caller can
// have the provider retry (webhooks) or keep the message (mailbox).
func (m *Manager) Record(b models.Bounce) error {
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	return m.opt.RecordBounceCB(b)
}
