package manager

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"testing"
)

func TestIsTemporarySendErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"SMTP 421 service unavailable", &textproto.Error{Code: 421, Msg: "try again later"}, true},
		{"SMTP 451 local error", &textproto.Error{Code: 451, Msg: "temporary failure"}, true},
		{"SMTP 452 mailbox full", fmt.Errorf("send: %w", &textproto.Error{Code: 452, Msg: "insufficient storage"}), true},
		{"SMTP 550 mailbox unavailable", &textproto.Error{Code: 550, Msg: "no such user"}, false},
		{"SMTP 554 rejected", &textproto.Error{Code: 554, Msg: "message rejected"}, false},
		{"dial refused", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}, true},
		{"DNS lookup failed", &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "smtp.example.com"}}, true},
		{"no SMTP servers", errors.New(`no SMTP servers available for messenger "email"`), true},
		// The connection broke after the e-mail may have been handed over:
		// never retried automatically.
		{"write broke mid-send", &net.OpError{Op: "write", Net: "tcp", Err: errors.New("broken pipe")}, false},
		{"read timeout", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("i/o timeout")}, false},
		{"EOF", io.EOF, false},
		{"other", errors.New("template error"), false},
	}

	for _, c := range cases {
		if got := isTemporarySendErr(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLogMessageMarksTemporary(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)

	m.logMessage(1, 1, &textproto.Error{Code: 421, Msg: "later"})
	m.logMessage(1, 2, &textproto.Error{Code: 550, Msg: "no"})
	m.logMessage(1, 3, nil)
	m.stopMessageLog()

	rows := f.rows()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if !rows[0].Temporary || rows[1].Temporary || rows[2].Temporary {
		t.Fatalf("wrong temporary flags: %+v", rows)
	}
}
