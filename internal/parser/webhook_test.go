package parser

import (
	"strings"
	"testing"
)

func TestLoadPathWebhookStart(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "start" {
    start {
      address       = "0.0.0.0:0"
      path          = "/webhooks/orders"
      public_host   = "host.docker.internal"
      public_scheme = "https"
      public_port   = 9000
      max_body_size = "1MB"
    }
    capture {
      id  = webhook.id
      url = webhook.url
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	wc := suite.Scenarios[0].Steps[0].Webhook
	if wc == nil || wc.Start == nil {
		t.Fatal("expected a webhook start call")
	}

	if wc.Wait != nil || wc.Stop != nil {
		t.Fatal("only start should be set")
	}
}

func TestLoadPathWebhookWaitWithExpect(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "start" {
    start { path = "/hook" }
    capture { id = webhook.id }
  }

  step "webhook" "expect" {
    target = result.start.id
    wait {
      timeout = "5s"
      count   = 2
    }
    expect {
      request {
        method = "POST"
        path   = "/hook"
        headers = {
          "X-Webhook-Signature" = is_string()
        }
        json = {
          type = "order.completed"
        }
      }
      hmac_signature {
        header  = "X-Webhook-Signature"
        secret  = "shh"
        format  = "t={timestamp},v1={signature}"
        payload = "${timestamp}.${request.body.raw}"
      }
    }
    capture { event_id = request.json.id }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	wait := suite.Scenarios[0].Steps[1].Webhook
	if wait == nil || wait.Wait == nil {
		t.Fatal("expected a webhook wait call")
	}

	if wait.Expect == nil || wait.Expect.Request == nil || wait.Expect.HMAC == nil {
		t.Fatalf("expected request + hmac_signature expectations, got %#v", wait.Expect)
	}
}

func TestLoadPathWebhookStop(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "stop" {
    stop { target = "webhook_x" }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if suite.Scenarios[0].Steps[0].Webhook.Stop == nil {
		t.Fatal("expected a webhook stop call")
	}
}

func TestWebhookRejectsMultipleOperations(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "bad" {
    start { path = "/hook" }
    stop  { target = "x" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "Conflicting webhook operation")
}

func TestWebhookRejectsMissingOperation(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "bad" {
    capture { id = webhook.id }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "Missing webhook operation")
}

func TestWebhookRejectsPathWithoutSlash(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "bad" {
    start { path = "hook" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "must start with")
}

func TestWebhookRejectsMissingHMACFields(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "expect" {
    target = result.start.id
    wait {}
    expect {
      hmac_signature {
        algorithm = "sha256"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "hmac_signature must declare")
}

func TestWebhookRejectsFieldsOnNonWebhookProvider(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "http" "bad" {
    start { path = "/hook" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "Webhook fields on non-webhook step")
}

func TestWebhookRejectsTopLevelExpectAttributes(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "expect" {
    target = result.start.id
    wait {}
    expect {
      json = { type = "x" }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "Unsupported webhook expect attribute")
}

func TestWebhookRejectsTargetOnStart(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "s" {
  step "webhook" "bad" {
    target = "x"
    start { path = "/hook" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	assertDiagContains(t, diags, "Unexpected target on webhook start")
}

func assertDiagContains(t *testing.T, diags interface{ Error() string }, want string) {
	t.Helper()

	if diags == nil {
		t.Fatalf("expected a diagnostic containing %q, got none", want)
	}

	if msg := diags.Error(); !strings.Contains(msg, want) {
		t.Fatalf("expected diagnostic containing %q, got: %s", want, msg)
	}
}
