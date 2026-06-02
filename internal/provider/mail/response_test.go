package mail

import (
	"testing"

	"github.com/tales-testing/tales/internal/provider"
)

func TestBuildRequestMetaFiltersReservedHeaders(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{
		From:    "sender@example.com",
		To:      []string{"a@example.test"},
		Subject: "real subject",
		Headers: map[string]string{
			"X-Test-ID":  "abc",
			"Subject":    "ignored",   // reserved: generated from the explicit field
			"From":       "spoof@x",   // reserved
			"Message-ID": "<spoof@x>", // reserved
		},
		MessageID: "<m@tales.local>",
	}

	meta := buildRequestMeta(Target{Name: "inbound", Protocol: "smtp"}, exec, messageSpec{})

	headers := meta["headers"].AsValueMap()

	if _, ok := headers["X-Test-ID"]; !ok {
		t.Errorf("custom header X-Test-ID must be reported")
	}

	for _, reserved := range []string{"Subject", "From", "Message-ID"} {
		if _, ok := headers[reserved]; ok {
			t.Errorf("reserved header %q must not appear in report metadata (it is not sent verbatim)", reserved)
		}
	}
}

func TestBuildRequestMetaOmitsBody(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{
		From: "sender@example.com",
		To:   []string{"a@example.test"},
		Text: "super secret body",
		HTML: "<p>secret</p>",
	}

	meta := buildRequestMeta(Target{Name: "inbound", Protocol: "smtp"}, exec, messageSpec{})

	for _, banned := range []string{"body", "text", "html", "raw", "raw_body"} {
		if _, ok := meta[banned]; ok {
			t.Errorf("request metadata must not carry a %q key", banned)
		}
	}
}

func TestNonReservedHeadersNilSafe(t *testing.T) {
	t.Parallel()

	if got := nonReservedHeaders(nil); len(got) != 0 {
		t.Fatalf("nil headers must stay empty, got %v", got)
	}
}
