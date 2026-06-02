package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintMailSummaryAccepted(t *testing.T) {
	t.Parallel()

	step := &StepResult{
		Provider: "mail",
		Response: map[string]interface{}{
			"json": map[string]interface{}{
				"protocol":   "smtp",
				"message_id": "<abc@tales.local>",
				"recipients": map[string]interface{}{
					"accepted": []interface{}{"a@example.test", "b@example.test"},
					"rejected": []interface{}{},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := printMailSummary(&buf, step); err != nil {
		t.Fatalf("printMailSummary: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"protocol: smtp", "message-id: <abc@tales.local>", "recipients: 2 accepted, 0 rejected"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q, got:\n%s", want, out)
		}
	}
}

func TestPrintMailSummaryRejection(t *testing.T) {
	t.Parallel()

	step := &StepResult{
		Provider: "mail",
		Response: map[string]interface{}{
			"json": map[string]interface{}{
				"protocol":             "smtp",
				"rejected":             true,
				"stage":                "mail_from",
				"status_code":          int64(550),
				"enhanced_status_code": "5.7.1",
				"message":              "sender domain rejected",
				"recipients": map[string]interface{}{
					"accepted": []interface{}{},
					"rejected": []interface{}{},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := printMailSummary(&buf, step); err != nil {
		t.Fatalf("printMailSummary: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `rejected at mail_from: status=550 enhanced=5.7.1 message="sender domain rejected"`) {
		t.Errorf("missing top-level rejection line, got:\n%s", out)
	}
}

func TestPrintMailSummaryRecipientRejection(t *testing.T) {
	t.Parallel()

	step := &StepResult{
		Provider: "mail",
		Response: map[string]interface{}{
			"json": map[string]interface{}{
				"protocol": "lmtp",
				"rejected": true,
				"stage":    "message",
				"recipients": map[string]interface{}{
					"accepted": []interface{}{"ok@example.test"},
					"rejected": []interface{}{
						map[string]interface{}{"address": "bad@example.test", "status_code": int64(550), "message": "no such user"},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := printMailSummary(&buf, step); err != nil {
		t.Fatalf("printMailSummary: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "recipients: 1 accepted, 1 rejected") {
		t.Errorf("missing recipient counts, got:\n%s", out)
	}

	if !strings.Contains(out, "rejected bad@example.test: 550 no such user") {
		t.Errorf("missing per-recipient rejection detail, got:\n%s", out)
	}
}

func TestPrintMailSummaryNoResponse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := printMailSummary(&buf, &StepResult{Provider: "mail"}); err != nil {
		t.Fatalf("printMailSummary: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty response, got %q", buf.String())
	}
}
