package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMailSuite(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "mail.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestLoadPathMailStep(t *testing.T) {
	t.Parallel()

	dir := writeMailSuite(t, `version = 1

scenario "mail send" {
  step "mail" "send" {
    target = "inbound"
    message {
      from    = "sender@example.com"
      to      = ["archive@example.test"]
      cc      = ["cc@example.test"]
      subject = "Hello"
      text    = "Plain body"
      html    = "<p>HTML</p>"
      headers = {
        "X-Test-ID" = "smtp-001"
      }
      attachment {
        filename     = "proof.json"
        content_type = "application/json"
        content      = "{}"
      }
    }
    expect {
      json = {
        accepted = true
      }
    }
    capture {
      message_id = response.json.message_id
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Provider != "mail" {
		t.Fatalf("provider: want mail got %q", step.Provider)
	}

	if step.Mail == nil {
		t.Fatalf("step.Mail should be set")
	}

	if step.Mail.Target.Expr == nil {
		t.Fatalf("target expression should be set")
	}

	msg := step.Mail.Message
	if msg == nil {
		t.Fatalf("message block should be parsed")
	}

	for name, expr := range map[string]bool{
		"from": msg.From.Expr != nil, "to": msg.To.Expr != nil, "cc": msg.Cc.Expr != nil,
		"subject": msg.Subject.Expr != nil, "text": msg.Text.Expr != nil,
		"html": msg.HTML.Expr != nil, "headers": msg.Headers.Expr != nil,
	} {
		if !expr {
			t.Errorf("message.%s expression should be set", name)
		}
	}

	if len(msg.Attachments) != 1 {
		t.Fatalf("want 1 attachment got %d", len(msg.Attachments))
	}

	if msg.Attachments[0].Content.Expr == nil || msg.Attachments[0].Filename.Expr == nil {
		t.Fatalf("attachment filename/content expressions should be set")
	}

	if _, ok := step.Capture["message_id"]; !ok {
		t.Fatalf("capture message_id should be parsed")
	}
}

func TestMailAttachmentsPreserveOrder(t *testing.T) {
	t.Parallel()

	dir := writeMailSuite(t, `version = 1

scenario "mail order" {
  step "mail" "send" {
    target = "inbound"
    message {
      from = "a@example.com"
      to   = ["b@example.test"]
      text = "x"
      attachment {
        filename = "first.txt"
        content  = "1"
      }
      attachment {
        filename = "second.txt"
        content  = "2"
      }
      attachment {
        filename = "third.txt"
        content  = "3"
      }
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	atts := suite.Scenarios[0].Steps[0].Mail.Message.Attachments
	if len(atts) != 3 {
		t.Fatalf("want 3 attachments got %d", len(atts))
	}
}

// mailCase renders a one-step mail suite with the given message body and
// optional target so each validation case stays focused on a single error.
func mailCase(message, target string) string {
	var b strings.Builder

	b.WriteString("version = 1\n\nscenario \"s\" {\n  step \"mail\" \"send\" {\n")

	if target != "" {
		fmt.Fprintf(&b, "    target = %q\n", target)
	}

	if message != "" {
		fmt.Fprintf(&b, "    %s\n", message)
	}

	b.WriteString("  }\n}\n")

	return b.String()
}

func TestMailValidationErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		content string
		wantSub string
	}{
		"missing target": {
			content: mailCase(`message {
      from = "a@x.test"
      to   = ["b@x.test"]
      text = "x"
    }`, ""),
			wantSub: "Missing mail target",
		},
		"missing message": {
			content: mailCase("", `inbound`),
			wantSub: "Missing mail message",
		},
		"missing from": {
			content: mailCase(`message {
      to   = ["b@x.test"]
      text = "x"
    }`, `inbound`),
			wantSub: "Missing mail sender",
		},
		"missing recipient": {
			content: mailCase(`message {
      from = "a@x.test"
      text = "x"
    }`, `inbound`),
			wantSub: "Missing mail recipient",
		},
		"attachment both sources": {
			content: mailCase(`message {
      from = "a@x.test"
      to   = ["b@x.test"]
      attachment {
        filename = "f.txt"
        path     = "f.txt"
        content  = "x"
      }
    }`, `inbound`),
			wantSub: "Conflicting attachment source",
		},
		"attachment no source": {
			content: mailCase(`message {
      from = "a@x.test"
      to   = ["b@x.test"]
      attachment {
        filename = "f.txt"
      }
    }`, `inbound`),
			wantSub: "Missing attachment source",
		},
		"attachment missing filename": {
			content: mailCase(`message {
      from = "a@x.test"
      to   = ["b@x.test"]
      attachment {
        content = "x"
      }
    }`, `inbound`),
			wantSub: "Missing attachment filename",
		},
		"unknown message attribute": {
			content: mailCase(`message {
      from  = "a@x.test"
      to    = ["b@x.test"]
      text  = "x"
      bogus = "y"
    }`, `inbound`),
			wantSub: "Unsupported argument",
		},
		"message on non-mail step": {
			content: `version = 1

scenario "s" {
  step "http" "send" {
    message {
      from = "a@x.test"
      to   = ["b@x.test"]
      text = "x"
    }
  }
}
`,
			wantSub: "Mail fields on non-mail step",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := writeMailSuite(t, tc.content)

			_, diags := LoadPath(dir)
			if !diags.HasErrors() {
				t.Fatalf("expected diagnostics for %q", name)
			}

			if !strings.Contains(diags.Error(), tc.wantSub) {
				t.Fatalf("want error containing %q, got: %s", tc.wantSub, diags.Error())
			}
		})
	}
}
