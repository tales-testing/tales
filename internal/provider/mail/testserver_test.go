package mail

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/mail"
	"sync"
	"testing"

	sasl "github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
)

// recordedMessage is one message captured by the test server.
type recordedMessage struct {
	From  string
	Rcpts []string
	Data  []byte
}

// MessageID returns the parsed Message-ID header of the recorded message.
func (m recordedMessage) MessageID() string {
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Data))
	if err != nil {
		return ""
	}

	return parsed.Header.Get("Message-ID")
}

// header returns a parsed header value of the recorded message.
func (m recordedMessage) header(name string) string {
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Data))
	if err != nil {
		return ""
	}

	return parsed.Header.Get(name)
}

// recordingBackend is a go-smtp backend that stores received messages and can
// be configured to reject at any protocol stage (with a full *smtp.SMTPError so
// tests can assert the enhanced code) or to require AUTH.
type recordingBackend struct {
	mu             sync.Mutex
	messages       []recordedMessage
	rejectMailFrom *smtp.SMTPError            // reject MAIL FROM
	rejectRcpt     map[string]*smtp.SMTPError // reject specific RCPT TO
	rejectData     *smtp.SMTPError            // reject the SMTP DATA final response
	rejectLMTPData map[string]*smtp.SMTPError // reject a recipient at the LMTP message stage
	username       string
	password       string
}

func (b *recordingBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &recordingSession{backend: b}, nil
}

func (b *recordingBackend) record(from string, rcpts []string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cloned := make([]byte, len(data))
	copy(cloned, data)

	b.messages = append(b.messages, recordedMessage{From: from, Rcpts: append([]string(nil), rcpts...), Data: cloned})
}

func (b *recordingBackend) all() []recordedMessage {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]recordedMessage(nil), b.messages...)
}

type recordingSession struct {
	backend *recordingBackend
	from    string
	rcpts   []string
}

func (s *recordingSession) Reset()        { s.from = ""; s.rcpts = nil }
func (s *recordingSession) Logout() error { return nil }

func (s *recordingSession) Mail(from string, _ *smtp.MailOptions) error {
	if s.backend.rejectMailFrom != nil {
		return s.backend.rejectMailFrom
	}

	s.from = from

	return nil
}

func (s *recordingSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	if err := s.backend.rejectRcpt[to]; err != nil {
		return err
	}

	s.rcpts = append(s.rcpts, to)

	return nil
}

func (s *recordingSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if s.backend.rejectData != nil {
		return s.backend.rejectData
	}

	s.backend.record(s.from, s.rcpts, data)

	return nil
}

func (s *recordingSession) LMTPData(r io.Reader, status smtp.StatusCollector) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	s.backend.record(s.from, s.rcpts, data)

	for _, rcpt := range s.rcpts {
		// Pass an untyped nil for accepted recipients: a typed-nil
		// *smtp.SMTPError wrapped in the error interface is non-nil and
		// panics inside go-smtp's status handling.
		if serr := s.backend.rejectLMTPData[rcpt]; serr != nil {
			status.SetStatus(rcpt, serr)

			continue
		}

		status.SetStatus(rcpt, nil)
	}

	return nil
}

// AuthMechanisms / Auth implement smtp.AuthSession so the server advertises
// AUTH PLAIN when the backend carries credentials.
func (s *recordingSession) AuthMechanisms() []string {
	if s.backend.username == "" {
		return nil
	}

	return []string{sasl.Plain}
}

func (s *recordingSession) Auth(mech string) (sasl.Server, error) {
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnsupported
	}

	return sasl.NewPlainServer(func(identity, username, password string) error {
		if username != s.backend.username || password != s.backend.password {
			return smtp.ErrAuthFailed
		}

		return nil
	}), nil
}

// startMailServer starts an in-process go-smtp server on a random loopback port
// and returns its host and port. lmtp selects LMTP mode.
func startMailServer(t *testing.T, backend *recordingBackend, lmtp bool) (string, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := smtp.NewServer(backend)
	server.Domain = "localhost"
	server.AllowInsecureAuth = true
	server.LMTP = lmtp

	go func() { _ = server.Serve(ln) }()

	t.Cleanup(func() { _ = server.Close() })

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", ln.Addr())
	}

	return "127.0.0.1", addr.Port
}

// startUnixLMTPServer starts an in-process LMTP server on a unix socket and
// returns the socket path.
func startUnixLMTPServer(t *testing.T, backend *recordingBackend) string {
	t.Helper()

	path := fmt.Sprintf("%s/lmtp.sock", t.TempDir())

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	server := smtp.NewServer(backend)
	server.Domain = "localhost"
	server.LMTP = true

	go func() { _ = server.Serve(ln) }()

	t.Cleanup(func() { _ = server.Close() })

	return path
}
