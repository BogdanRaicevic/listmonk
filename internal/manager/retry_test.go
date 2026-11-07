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

func TestIsUncertainSendErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		// Broke on an established connection: may have been delivered.
		{"EOF after DATA", io.EOF, true},
		{"unexpected EOF", fmt.Errorf("data: %w", io.ErrUnexpectedEOF), true},
		{"write broken pipe", &net.OpError{Op: "write", Net: "tcp", Err: errors.New("broken pipe")}, true},
		{"read timeout", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("i/o timeout")}, true},
		{"closed conn", fmt.Errorf("send: %w", net.ErrClosed), true},
		{"reset text", errors.New("read tcp: connection reset by peer"), true},
		// Certain outcomes.
		{"SMTP 421", &textproto.Error{Code: 421, Msg: "later"}, false},
		{"SMTP 550", &textproto.Error{Code: 550, Msg: "no such user"}, false},
		{"dial refused", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, false},
		{"no SMTP servers", errors.New(`no SMTP servers available for messenger "email"`), false},
		{"template error", errors.New(`template: content:1: function "x" not defined`), false},
	}

	for _, c := range cases {
		if got := isUncertainSendErr(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestKeepForResume(t *testing.T) {
	if keepForResume(nil) {
		t.Error("a sent e-mail must not be kept for resume")
	}
	if !keepForResume(&textproto.Error{Code: 550, Msg: "no"}) {
		t.Error("a certain failure must be kept for resume")
	}
	if !keepForResume(&net.OpError{Op: "dial", Err: errors.New("refused")}) {
		t.Error("a failed connect must be kept for resume")
	}
	if keepForResume(io.EOF) {
		t.Error("an uncertain send must not be kept for resume")
	}
}

func TestLogMessageMarksUncertain(t *testing.T) {
	f := &fakeLogStore{}
	m := newTestLogManager(f)

	m.logMessage(1, 1, io.EOF)
	m.logMessage(1, 2, &textproto.Error{Code: 550, Msg: "no"})
	m.stopMessageLog()

	rows := f.rows()
	if rows[0].Status != MessageUncertain || rows[0].Temporary {
		t.Fatalf("EOF: want uncertain, not temporary; got %+v", rows[0])
	}
	if rows[1].Status != MessageFailed {
		t.Fatalf("550: want failed; got %+v", rows[1])
	}
}
