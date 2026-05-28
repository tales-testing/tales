package parser

import (
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
)

func TestLoadPathBrowserStepBasic(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "login" {
  step "browser" "open" {
    target = "chrome"
    actions {
      goto {
        url = "https://example.com/login"
      }
      fill {
        selector = "[data-testid='login.email']"
        value    = "user@example.com"
      }
      fill {
        selector = "[data-testid='login.password']"
        value    = "secret"
        secure   = true
      }
      click {
        selector = "[data-testid='login.submit']"
      }
      wait_visible {
        selector = "[data-testid='dashboard.title']"
        timeout  = "10s"
      }
    }
    expect {
      visible {
        selector = "[data-testid='dashboard.title']"
      }
      text {
        selector = "[data-testid='dashboard.title']"
        value    = "Dashboard"
      }
      attribute {
        selector = "meta[name='csrf-token']"
        name     = "content"
        value    = "csrf-demo-token"
      }
      url {
        value = "https://example.com/dashboard"
      }
      title {
        value = "Dashboard"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Scenarios) != 1 || len(suite.Scenarios[0].Steps) != 1 {
		t.Fatalf("expected 1 scenario with 1 step, got %d/%d", len(suite.Scenarios), len(suite.Scenarios[0].Steps))
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Browser == nil {
		t.Fatal("expected step.Browser to be populated")
	}

	if len(step.Browser.Actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(step.Browser.Actions))
	}

	expectedKinds := []model.BrowserActionKind{
		model.BrowserActionGoto,
		model.BrowserActionFill,
		model.BrowserActionFill,
		model.BrowserActionClick,
		model.BrowserActionWaitVisible,
	}

	for i, want := range expectedKinds {
		if step.Browser.Actions[i].Kind != want {
			t.Errorf("action[%d] kind = %q, want %q", i, step.Browser.Actions[i].Kind, want)
		}
	}

	expect := step.Browser.Expect
	if len(expect.Visible) != 1 || len(expect.Text) != 1 ||
		len(expect.Attribute) != 1 || len(expect.URL) != 1 || len(expect.Title) != 1 {
		t.Fatalf("expectation counts mismatch: %+v", expect)
	}
}

func TestLoadPathBrowserAllActions(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "actions_smoke" {
  step "browser" "every_action" {
    target = "chrome"
    actions {
      goto {
        url = "https://example.com/"
      }
      click {
        selector = "#a"
      }
      fill {
        selector = "#b"
        value    = "x"
      }
      clear {
        selector = "#c"
      }
      press {
        key = "Enter"
      }
      submit {
        selector = "form#login"
      }
      scroll {
        x = 0
        y = 800
      }
      scroll {
        selector = "#footer"
      }
      wait_visible {
        selector = "#ready"
      }
      wait_not_visible {
        selector = ".spinner"
      }
      hover {
        selector = "#menu"
      }
      select {
        selector = "select#country"
        value    = "FR"
      }
      check {
        selector = "input#tos"
      }
      uncheck {
        selector = "input#newsletter"
      }
      reload {}
      back {}
      forward {}
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Browser == nil {
		t.Fatal("expected step.Browser to be populated")
	}

	if got := len(step.Browser.Actions); got != 17 {
		t.Fatalf("expected 17 actions, got %d", got)
	}
}

func TestLoadPathBrowserMissingTarget(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "missing_target" {
  step "browser" "noop" {
    actions {
      goto {
        url = "https://example.com"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for missing target")
	}

	if !strings.Contains(diags.Error(), "Missing browser target") {
		t.Fatalf("expected 'Missing browser target' diag, got: %s", diags.Error())
	}
}

func TestLoadPathBrowserUnknownAction(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "unknown" {
  step "browser" "bad" {
    target = "chrome"
    actions {
      teleport {
        selector = "#x"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for unknown action")
	}

	if !strings.Contains(diags.Error(), "Unknown action") {
		t.Fatalf("expected 'Unknown action' diag, got: %s", diags.Error())
	}
}

func TestLoadPathBrowserRejectsMobileID(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "wrong_locator" {
  step "browser" "bad_expect" {
    target = "chrome"
    actions {
      goto {
        url = "https://example.com"
      }
    }
    expect {
      visible {
        id = "dashboard.title"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for mobile id on browser step")
	}

	if !strings.Contains(diags.Error(), "Unexpected id attribute") {
		t.Fatalf("expected 'Unexpected id attribute' diag, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsBrowserSelector(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "wrong_locator" {
  step "mobile" "bad_expect" {
    platform = "ios"
    target = "iphone"
    expect {
      visible {
        selector = "[data-testid='greeting']"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for browser selector on mobile step")
	}

	if !strings.Contains(diags.Error(), "Unexpected selector attribute") {
		t.Fatalf("expected 'Unexpected selector attribute' diag, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsBrowserExpectBlocks(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "browser_blocks_on_mobile" {
  step "mobile" "noop" {
    platform = "ios"
    target = "iphone"
    expect {
      url {
        value = "irrelevant"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for url block on mobile step")
	}

	if !strings.Contains(diags.Error(), "Unexpected url block") {
		t.Fatalf("expected 'Unexpected url block' diag, got: %s", diags.Error())
	}
}

func TestLoadPathBrowserScrollValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			"conflict",
			"scroll {\n        selector = \"#a\"\n        x        = 0\n        y        = 0\n      }",
			"Conflicting scroll attributes",
		},
		{
			"missing",
			"scroll {}",
			"Missing scroll target",
		},
		{
			"incomplete",
			"scroll {\n        x = 0\n      }",
			"Incomplete scroll offsets",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := `version = 1

scenario "s" {
  step "browser" "n" {
    target = "chrome"
    actions {
      ` + tc.content + `
    }
  }
}
`

			_, diags := LoadPath(writeTales(t, content))
			if !diags.HasErrors() {
				t.Fatal("expected scroll diagnostics")
			}

			if !strings.Contains(diags.Error(), tc.want) {
				t.Fatalf("expected %q in diags, got: %s", tc.want, diags.Error())
			}
		})
	}
}

func TestLoadPathBrowserStepOnDifferentProviderFails(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "wrong_provider" {
  step "http" "bad" {
    request {
      method = "GET"
      url    = "https://example.com"
    }
    expect {
      url { value = "x" }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for browser-only block on http step")
	}

	if !strings.Contains(diags.Error(), "Browser fields on non-browser step") {
		t.Fatalf("expected cross-provider rejection diag, got: %s", diags.Error())
	}
}
