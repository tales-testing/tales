package mail

import (
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, time.June, 2, 10, 30, 0, 0, time.UTC)
}

func buildOrFatal(t *testing.T, spec messageSpec) string {
	t.Helper()

	raw, err := buildMessage(spec, fixedNow())
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	return string(raw)
}

func TestBuildMessageTextOnly(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "sender@example.com",
		To:        []string{"archive@example.test"},
		Subject:   "Hello",
		Text:      "Plain body",
		MessageID: "<m1@tales.local>",
	})

	wants := []string{
		"From: sender@example.com\r\n",
		"To: archive@example.test\r\n",
		"Subject: Hello\r\n",
		"Date: Tue, 02 Jun 2026 10:30:00 +0000\r\n",
		"Message-ID: <m1@tales.local>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\nPlain body\r\n",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}

	if strings.Contains(out, "multipart") {
		t.Errorf("text-only message must not be multipart")
	}
}

func TestBuildMessageHTMLOnly(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		HTML:      "<p>Hi</p>",
		MessageID: "<m2@tales.local>",
	})

	if !strings.Contains(out, "Content-Type: text/html; charset=utf-8\r\n") {
		t.Errorf("html-only must use text/html, got:\n%s", out)
	}
}

func TestBuildMessageAlternative(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "plain",
		HTML:      "<p>html</p>",
		MessageID: "<m3@tales.local>",
	})

	if !strings.Contains(out, "Content-Type: multipart/alternative;") {
		t.Errorf("text+html must be multipart/alternative, got:\n%s", out)
	}

	if !strings.Contains(out, "Content-Type: text/plain; charset=utf-8") || !strings.Contains(out, "Content-Type: text/html; charset=utf-8") {
		t.Errorf("alternative must carry both parts, got:\n%s", out)
	}
}

func TestBuildMessageAttachmentsMixedBase64(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "see attached",
		MessageID: "<m4@tales.local>",
		Attachments: []attachmentPayload{
			{Filename: "proof.json", ContentType: "application/json", Data: []byte(`{"id":1}`)},
		},
	})

	for _, want := range []string{
		"Content-Type: multipart/mixed;",
		`Content-Type: application/json; name="proof.json"`,
		"Content-Transfer-Encoding: base64\r\n",
		`Content-Disposition: attachment; filename="proof.json"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("attachment output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestBuildMessageBccNotInHeaders(t *testing.T) {
	t.Parallel()

	// messageSpec never carries Bcc — confirm a Bcc header is never emitted
	// even when a custom header tries to inject one.
	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "body",
		Headers:   map[string]string{"Bcc": "secret@example.test"},
		MessageID: "<m5@tales.local>",
	})

	if strings.Contains(strings.ToLower(out), "bcc:") {
		t.Errorf("message must not contain a Bcc header, got:\n%s", out)
	}
}

func TestBuildMessageCustomHeaderAndReservedSkip(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Subject:   "real subject",
		Text:      "body",
		Headers:   map[string]string{"X-Test-ID": "abc", "Subject": "ignored", "From": "ignored@x"},
		MessageID: "<m6@tales.local>",
	})

	if !strings.Contains(out, "X-Test-Id: abc\r\n") {
		t.Errorf("custom header should be present (canonicalized), got:\n%s", out)
	}

	if strings.Count(out, "Subject:") != 1 || strings.Contains(out, "Subject: ignored") {
		t.Errorf("reserved Subject must not be duplicated from headers, got:\n%s", out)
	}

	if strings.Count(out, "From:") != 1 {
		t.Errorf("reserved From must not be duplicated, got:\n%s", out)
	}
}

func TestBuildMessageNonASCIISubjectQEncoded(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Subject:   "Café résumé",
		Text:      "body",
		MessageID: "<m7@tales.local>",
	})

	if !strings.Contains(out, "Subject: =?utf-8?q?") {
		t.Errorf("non-ASCII subject must be Q-encoded, got:\n%s", out)
	}
}

func TestBuildMessageNonASCIIBodyQuotedPrintable(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "Crème brûlée",
		MessageID: "<m8@tales.local>",
	})

	if !strings.Contains(out, "Content-Transfer-Encoding: quoted-printable\r\n") {
		t.Errorf("non-ASCII body must use quoted-printable, got:\n%s", out)
	}
}

func TestBuildMessageUserMessageIDRespected(t *testing.T) {
	t.Parallel()

	out := buildOrFatal(t, messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "body",
		MessageID: "<user-supplied@example.com>",
	})

	if !strings.Contains(out, "Message-ID: <user-supplied@example.com>\r\n") {
		t.Errorf("supplied Message-ID must be used, got:\n%s", out)
	}
}

func TestBuildMessageCRLFAndTrailingNewline(t *testing.T) {
	t.Parallel()

	raw, err := buildMessage(messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "line1\nline2",
		MessageID: "<m9@tales.local>",
	}, fixedNow())
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Errorf("message must use CRLF only, found a bare LF")
	}

	if !strings.HasSuffix(string(raw), "\r\n") {
		t.Errorf("message must end with CRLF")
	}
}

func TestBuildMessageHeaderInjectionRejected(t *testing.T) {
	t.Parallel()

	_, err := buildMessage(messageSpec{
		From:      "s@example.com",
		To:        []string{"a@example.test"},
		Text:      "body",
		Headers:   map[string]string{"X-Evil": "value\r\nBcc: attacker@example.test"},
		MessageID: "<m10@tales.local>",
	}, fixedNow())
	if err == nil {
		t.Fatalf("expected header injection to be rejected")
	}
}

func TestDeriveBoundaryDeterministic(t *testing.T) {
	t.Parallel()

	a := deriveBoundary("<id@tales.local>", "mixed")
	b := deriveBoundary("<id@tales.local>", "mixed")

	if a != b {
		t.Fatalf("boundary must be deterministic: %q vs %q", a, b)
	}

	if deriveBoundary("<id@tales.local>", "alt") == a {
		t.Fatalf("different kinds must produce different boundaries")
	}
}
