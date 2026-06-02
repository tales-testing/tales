//nolint:goconst // mockserver is fixture code; literal strings keep handlers readable
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	smtp "github.com/emersion/go-smtp"
)

// smtpListenAddr / lmtpListenAddr are the loopback endpoints the mock mail
// server binds in addition to the HTTP API. They match the e2e .tales targets.
const (
	smtpListenAddr = "127.0.0.1:2525"
	lmtpListenAddr = "127.0.0.1:2424"
)

// storedMail is a received message decoded enough for HTTP assertions. The raw
// MIME is kept so suites can inspect anything the decoded fields omit.
type storedMail struct {
	MessageID string            `json:"message_id"`
	From      string            `json:"from"`
	To        []string          `json:"to"`
	Subject   string            `json:"subject"`
	Text      string            `json:"text"`
	TestID    string            `json:"test_id"`
	Headers   map[string]string `json:"headers"`
	Raw       string            `json:"raw"`
}

// mailStore is the in-memory inbox the SMTP / LMTP listeners write to and the
// HTTP API reads from. It is indexed by Message-ID and by the X-Test-ID header.
type mailStore struct {
	mu       sync.Mutex
	byID     map[string]storedMail
	byTestID map[string]storedMail
}

func newMailStore() *mailStore {
	return &mailStore{
		byID:     map[string]storedMail{},
		byTestID: map[string]storedMail{},
	}
}

// add decodes and stores a received message. It is called before the listener
// sends its final acceptance so a follow-up HTTP poll always sees the message.
func (s *mailStore) add(from string, rcpts []string, raw []byte) {
	stored := decodeStoredMail(from, rcpts, raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	if stored.MessageID != "" {
		s.byID[stored.MessageID] = stored
	}

	if stored.TestID != "" {
		s.byTestID[stored.TestID] = stored
	}
}

func (s *mailStore) getByID(id string) (storedMail, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.byID[id]

	return stored, ok
}

func (s *mailStore) getByTestID(id string) (storedMail, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.byTestID[id]

	return stored, ok
}

func (s *mailStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.byID = map[string]storedMail{}
	s.byTestID = map[string]storedMail{}
}

// decodeStoredMail parses the raw MIME into the storedMail shape. The From
// header is reduced to its bare address so suites can assert the plain form.
func decodeStoredMail(from string, rcpts []string, raw []byte) storedMail {
	stored := storedMail{
		From:    from,
		To:      append([]string(nil), rcpts...),
		Headers: map[string]string{},
		Raw:     string(raw),
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return stored
	}

	decoder := new(mime.WordDecoder)

	stored.MessageID = parsed.Header.Get("Message-ID")
	stored.TestID = parsed.Header.Get("X-Test-ID")
	stored.Subject = decodeHeader(decoder, parsed.Header.Get("Subject"))

	if addr, err := mail.ParseAddress(parsed.Header.Get("From")); err == nil {
		stored.From = addr.Address
	}

	for key := range parsed.Header {
		stored.Headers[key] = parsed.Header.Get(key)
	}

	if body, err := io.ReadAll(parsed.Body); err == nil {
		stored.Text = string(body)
	}

	return stored
}

func decodeHeader(decoder *mime.WordDecoder, value string) string {
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		return value
	}

	return decoded
}

// mailBackend is the go-smtp backend that records every received message.
type mailBackend struct {
	store *mailStore
}

func (b *mailBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &mailSession{store: b.store}, nil
}

type mailSession struct {
	store *mailStore
	from  string
	rcpts []string
}

func (s *mailSession) Reset()        { s.from = ""; s.rcpts = nil }
func (s *mailSession) Logout() error { return nil }

func (s *mailSession) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from

	return nil
}

func (s *mailSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)

	return nil
}

func (s *mailSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read message data: %w", err)
	}

	s.store.add(s.from, s.rcpts, data)

	return nil
}

// LMTPData stores the message, then reports success for every recipient so the
// client reads one 250 per accepted recipient.
func (s *mailSession) LMTPData(r io.Reader, status smtp.StatusCollector) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read message data: %w", err)
	}

	s.store.add(s.from, s.rcpts, data)

	for _, rcpt := range s.rcpts {
		status.SetStatus(rcpt, nil)
	}

	return nil
}

// startMailListeners binds the SMTP and LMTP listeners. Bind failures are
// logged but non-fatal so the HTTP API (used by every other e2e suite) still
// comes up; the mail scenarios then fail loudly with a connection error.
func startMailListeners(store *mailStore) {
	startMailServer(store, smtpListenAddr, false)
	startMailServer(store, lmtpListenAddr, true)
}

func startMailServer(store *mailStore, addr string, lmtp bool) {
	server := smtp.NewServer(&mailBackend{store: store})
	server.Addr = addr
	server.Network = "tcp"
	server.Domain = "localhost"
	server.AllowInsecureAuth = true
	server.LMTP = lmtp
	server.ReadTimeout = 10 * time.Second
	server.WriteTimeout = 10 * time.Second
	server.MaxMessageBytes = 10 << 20

	proto := "SMTP"
	if lmtp {
		proto = "LMTP"
	}

	// Bind synchronously so the port is ready before /healthz answers (the e2e
	// readiness probe); only the accept loop runs in the background.
	var lc net.ListenConfig

	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Printf("mock %s listener bind on %s failed: %v", proto, addr, err)

		return
	}

	go func() {
		if err := server.Serve(ln); err != nil {
			log.Printf("mock %s listener on %s stopped: %v", proto, addr, err)
		}
	}()
}

// mailMessageByID serves GET /mail/messages?id=<message-id> (and a
// ?test_id=<id> fallback) from the SMTP/LMTP inbox.
func (s *serverState) mailMessageByID(w http.ResponseWriter, req *http.Request) bool {
	if id := req.URL.Query().Get("id"); id != "" {
		stored, ok := s.mail.getByID(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "message not found"})

			return true
		}

		writeJSON(w, http.StatusOK, stored)

		return true
	}

	if testID := req.URL.Query().Get("test_id"); testID != "" {
		stored, ok := s.mail.getByTestID(testID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "message not found"})

			return true
		}

		writeJSON(w, http.StatusOK, stored)

		return true
	}

	return false
}

// deleteMail clears the SMTP/LMTP inbox.
func (s *serverState) deleteMail(w http.ResponseWriter, _ *http.Request) {
	s.mail.clear()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
