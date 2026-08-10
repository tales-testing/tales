package mail

import (
	"context"
	"maps"
	"net"
	"strconv"
	"strings"
	"testing"

	smtp "github.com/emersion/go-smtp"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// assertBool / assertString / assertStatus read a field of a response.json
// object and fail the test on mismatch.
func assertBool(t *testing.T, obj cty.Value, key string, want bool) {
	t.Helper()

	if got := obj.GetAttr(key).True(); got != want {
		t.Errorf("%s: want %v got %v", key, want, got)
	}
}

func assertString(t *testing.T, obj cty.Value, key, want string) {
	t.Helper()

	if got := obj.GetAttr(key).AsString(); got != want {
		t.Errorf("%s: want %q got %q", key, want, got)
	}
}

func assertStatus(t *testing.T, obj cty.Value, want int) {
	t.Helper()

	got := obj.GetAttr("status_code")
	if got.IsNull() {
		t.Errorf("status_code: want %d got null", want)

		return
	}

	if got.AsBigFloat().Cmp(cty.NumberIntVal(int64(want)).AsBigFloat()) != 0 {
		t.Errorf("status_code: want %d got %s", want, got.AsBigFloat().String())
	}
}

// smtpConfig builds a config map with one SMTP target.
func smtpConfig(name, host string, port int, extra map[string]cty.Value) map[string]cty.Value {
	attrs := map[string]cty.Value{
		"protocol": cty.StringVal("smtp"),
		"host":     cty.StringVal(host),
		"port":     cty.NumberIntVal(int64(port)),
	}

	maps.Copy(attrs, extra)

	return mailConfig(name, attrs)
}

func lmtpConfig(name, network, address string) map[string]cty.Value {
	return mailConfig(name, map[string]cty.Value{
		"protocol": cty.StringVal("lmtp"),
		"network":  cty.StringVal(network),
		"address":  cty.StringVal(address),
	})
}

func mailConfig(name string, target map[string]cty.Value) map[string]cty.Value {
	return map[string]cty.Value{
		"mail": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				name: cty.ObjectVal(target),
			}),
		}),
	}
}

func mailInput(config map[string]cty.Value, exec *provider.MailExecution) provider.Input {
	return provider.Input{
		Scenario: "s",
		Step:     &model.Step{Name: "send", File: "mail.tales"},
		Config:   config,
		Mail:     exec,
	}
}

func TestProviderSendSMTP(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{}
	host, port := startMailServer(t, backend, false)

	exec := &provider.MailExecution{
		Target:    "inbound",
		From:      "sender@example.com",
		To:        []string{"archive@example.test"},
		Subject:   "SMTP ingestion test",
		Text:      "Hello from Tales",
		Headers:   map[string]string{"X-Test-ID": "smtp-001"},
		MessageID: "<deadbeef@tales.local>",
	}

	out, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", host, port, nil), exec))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	jsonVal := out.Response["json"]
	if jsonVal.GetAttr("accepted").True() != true {
		t.Fatalf("accepted should be true")
	}

	if got := jsonVal.GetAttr("message_id").AsString(); got != "<deadbeef@tales.local>" {
		t.Fatalf("message_id: want <deadbeef@tales.local> got %q", got)
	}

	if got := jsonVal.GetAttr("protocol").AsString(); got != "smtp" {
		t.Fatalf("protocol: want smtp got %q", got)
	}

	msgs := backend.all()
	if len(msgs) != 1 {
		t.Fatalf("want 1 received message got %d", len(msgs))
	}

	if id := msgs[0].MessageID(); id != "<deadbeef@tales.local>" {
		t.Fatalf("round-tripped Message-ID: want <deadbeef@tales.local> got %q", id)
	}

	if subj := msgs[0].header("Subject"); subj != "SMTP ingestion test" {
		t.Fatalf("subject: want %q got %q", "SMTP ingestion test", subj)
	}

	if x := msgs[0].header("X-Test-ID"); x != "smtp-001" {
		t.Fatalf("custom header X-Test-ID: want smtp-001 got %q", x)
	}
}

func TestProviderSendLMTPPerRecipient(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{}
	host, port := startMailServer(t, backend, true)

	exec := &provider.MailExecution{
		Target:    "lmtp_inbound",
		From:      "sender@example.com",
		To:        []string{"a@example.test", "b@example.test"},
		Subject:   "LMTP ingestion test",
		Text:      "Hello from Tales LMTP",
		MessageID: "<lmtp-1@tales.local>",
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	out, err := New().Execute(context.Background(), mailInput(lmtpConfig("lmtp_inbound", "tcp", addr), exec))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	accepted := out.Response["json"].GetAttr("recipients").GetAttr("accepted")
	if accepted.LengthInt() != 2 {
		t.Fatalf("want 2 accepted recipients got %d", accepted.LengthInt())
	}

	if out.Response["json"].GetAttr("protocol").AsString() != "lmtp" {
		t.Fatalf("protocol should be lmtp")
	}
}

func TestProviderSendLMTPUnixSocket(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{}
	socket := startUnixLMTPServer(t, backend)

	exec := &provider.MailExecution{
		Target:    "socket",
		From:      "sender@example.com",
		To:        []string{"archive@example.test"},
		Subject:   "unix socket",
		Text:      "over a unix socket",
		MessageID: "<unix-1@tales.local>",
	}

	if _, err := New().Execute(context.Background(), mailInput(lmtpConfig("socket", "unix", socket), exec)); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(backend.all()) != 1 {
		t.Fatalf("want 1 received message")
	}
}

func TestProviderSMTPAuthPlain(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{username: "user", password: "s3cr3t"}
	host, port := startMailServer(t, backend, false)

	auth := cty.ObjectVal(map[string]cty.Value{
		"username": cty.StringVal("user"),
		"password": cty.StringVal("s3cr3t"),
	})

	exec := &provider.MailExecution{
		Target:    "inbound",
		From:      "sender@example.com",
		To:        []string{"archive@example.test"},
		Subject:   "authed",
		Text:      "body",
		MessageID: "<auth-1@tales.local>",
	}

	cfg := smtpConfig("inbound", host, port, map[string]cty.Value{"auth": auth})

	if _, err := New().Execute(context.Background(), mailInput(cfg, exec)); err != nil {
		t.Fatalf("execute with auth: %v", err)
	}

	if len(backend.all()) != 1 {
		t.Fatalf("want 1 received message after auth")
	}
}

func TestProviderMailFromRejected(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{rejectMailFrom: &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "sender domain rejected"}}
	host, port := startMailServer(t, backend, false)

	exec := &provider.MailExecution{
		Target: "inbound", From: "attacker@invalid.example", To: []string{"a@example.test"},
		Text: "body", MessageID: "<mf-1@tales.local>",
	}

	out, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", host, port, nil), exec))
	if err != nil {
		t.Fatalf("MAIL FROM rejection must not be a provider error, got: %v", err)
	}

	mail := out.Response["json"]
	assertBool(t, mail, "accepted", false)
	assertBool(t, mail, "rejected", true)
	assertString(t, mail, "stage", "mail_from")
	assertString(t, mail, "enhanced_status_code", "5.7.1")
	assertStatus(t, mail, 550)

	if len(backend.all()) != 0 {
		t.Fatalf("no message should be stored when MAIL FROM is rejected")
	}
}

func TestProviderAllRecipientsRejected(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{rejectRcpt: map[string]*smtp.SMTPError{
		"bad@example.test": {Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "user unknown"},
	}}
	host, port := startMailServer(t, backend, false)

	exec := &provider.MailExecution{
		Target: "inbound", From: "sender@example.com", To: []string{"bad@example.test"},
		Text: "body", MessageID: "<reject-1@tales.local>",
	}

	out, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", host, port, nil), exec))
	if err != nil {
		t.Fatalf("all-recipients-rejected must not be a provider error, got: %v", err)
	}

	mail := out.Response["json"]
	assertBool(t, mail, "accepted", false)
	assertBool(t, mail, "rejected", true)
	assertString(t, mail, "stage", "rcpt")

	rejected := mail.GetAttr("recipients").GetAttr("rejected")
	if rejected.LengthInt() != 1 {
		t.Fatalf("want 1 rejected recipient got %d", rejected.LengthInt())
	}

	if len(backend.all()) != 0 {
		t.Fatalf("message must not be stored when every recipient is rejected")
	}
}

func TestProviderPartialRejection(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{rejectRcpt: map[string]*smtp.SMTPError{
		"bad@example.test": {Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "user unknown"},
	}}
	host, port := startMailServer(t, backend, false)

	exec := &provider.MailExecution{
		Target: "inbound", From: "sender@example.com", To: []string{"ok@example.test", "bad@example.test"},
		Text: "body", MessageID: "<partial-1@tales.local>",
	}

	out, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", host, port, nil), exec))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	mail := out.Response["json"]
	assertBool(t, mail, "accepted", true)
	assertBool(t, mail, "rejected", true)

	recipients := mail.GetAttr("recipients")
	if recipients.GetAttr("accepted").LengthInt() != 1 {
		t.Fatalf("want 1 accepted recipient")
	}

	rejected := recipients.GetAttr("rejected")
	if rejected.LengthInt() != 1 {
		t.Fatalf("want 1 rejected recipient got %d", rejected.LengthInt())
	}

	first := rejected.Index(cty.NumberIntVal(0))
	if first.GetAttr("address").AsString() != "bad@example.test" {
		t.Fatalf("rejected address mismatch")
	}

	assertString(t, first, "stage", "rcpt")
	assertString(t, first, "enhanced_status_code", "5.1.1")

	if first.GetAttr("status_code").AsBigFloat().Cmp(cty.NumberIntVal(550).AsBigFloat()) != 0 {
		t.Fatalf("rejected status_code should be 550")
	}
}

func TestProviderMessageRejected(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{rejectData: &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "message rejected due to DMARC policy"}}
	host, port := startMailServer(t, backend, false)

	exec := &provider.MailExecution{
		Target: "inbound", From: "sender@example.com", To: []string{"a@example.test"},
		Text: "body", MessageID: "<msg-1@tales.local>",
	}

	out, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", host, port, nil), exec))
	if err != nil {
		t.Fatalf("message rejection must not be a provider error, got: %v", err)
	}

	mail := out.Response["json"]
	assertBool(t, mail, "accepted", false)
	assertBool(t, mail, "rejected", true)
	assertString(t, mail, "stage", "message")
	assertStatus(t, mail, 554)

	if !strings.Contains(mail.GetAttr("message").AsString(), "DMARC") {
		t.Fatalf("message should carry the server reason, got %q", mail.GetAttr("message").AsString())
	}
}

func TestProviderLMTPPartialRejection(t *testing.T) {
	t.Parallel()

	backend := &recordingBackend{rejectLMTPData: map[string]*smtp.SMTPError{
		"bad@example.test": {Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "user unknown"},
	}}
	host, port := startMailServer(t, backend, true)

	exec := &provider.MailExecution{
		Target: "lmtp", From: "sender@example.com", To: []string{"ok@example.test", "bad@example.test"},
		Text: "body", MessageID: "<lmtp-partial@tales.local>",
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	out, err := New().Execute(context.Background(), mailInput(lmtpConfig("lmtp", "tcp", addr), exec))
	if err != nil {
		t.Fatalf("LMTP partial rejection must not be a provider error, got: %v", err)
	}

	mail := out.Response["json"]
	assertBool(t, mail, "accepted", true)
	assertBool(t, mail, "rejected", true)

	recipients := mail.GetAttr("recipients")
	if recipients.GetAttr("accepted").LengthInt() != 1 || recipients.GetAttr("rejected").LengthInt() != 1 {
		t.Fatalf("want 1 accepted and 1 rejected recipient")
	}

	rej := recipients.GetAttr("rejected").Index(cty.NumberIntVal(0))
	if rej.GetAttr("address").AsString() != "bad@example.test" {
		t.Fatalf("rejected address mismatch")
	}

	assertString(t, rej, "stage", "message")
}

func TestProviderConnectionRefusedIsFatal(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{
		Target: "inbound", From: "sender@example.com", To: []string{"a@example.test"},
		Text: "body", MessageID: "<refused@tales.local>",
	}

	// Port 1 is not listening: a transport failure must remain a provider error.
	_, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", "127.0.0.1", 1, nil), exec))
	if err == nil {
		t.Fatalf("connection refused must be a provider error")
	}
}

func TestProviderUnknownTarget(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{Target: "missing", From: "a@x.test", To: []string{"b@x.test"}, Text: "x", MessageID: "<x@tales.local>"}

	_, err := New().Execute(context.Background(), mailInput(smtpConfig("inbound", "127.0.0.1", 12525, nil), exec))
	if err == nil || !strings.Contains(err.Error(), `mail target "missing" not found`) {
		t.Fatalf("want not-found error, got: %v", err)
	}
}
