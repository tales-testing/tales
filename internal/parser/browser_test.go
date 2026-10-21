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
      wait_enabled {
        selector = "#submit"
        timeout  = "20s"
      }
      wait_disabled {
        selector = "#submit"
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
      upload_file {
        selector = "input#document"
        paths    = ["./fixtures/contract.pdf"]
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

	if got := len(step.Browser.Actions); got != 20 {
		t.Fatalf("expected 20 actions, got %d", got)
	}

	// wait_enabled / wait_disabled sit right after wait_not_visible and
	// carry the same selector-only surface as the other wait actions.
	if got := step.Browser.Actions[10].Kind; got != model.BrowserActionWaitEnabled {
		t.Fatalf("expected wait_enabled at index 10, got %q", got)
	}

	if step.Browser.Actions[10].Timeout.Empty() {
		t.Fatal("expected the wait_enabled timeout expression to be captured")
	}

	if got := step.Browser.Actions[11].Kind; got != model.BrowserActionWaitDisabled {
		t.Fatalf("expected wait_disabled at index 11, got %q", got)
	}
}

func TestLoadPathBrowserUploadFile(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "upload" {
  step "browser" "attach" {
    target = "chrome"
    actions {
      upload_file {
        selector = "input#document"
        paths    = ["./fixtures/contract.pdf", "./fixtures/appendix.pdf"]
        timeout  = "5s"
      }
      upload_file {
        selector = "input#avatar"
        paths    = "./fixtures/avatar.png"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	actions := suite.Scenarios[0].Steps[0].Browser.Actions
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	for i, action := range actions {
		if action.Kind != model.BrowserActionUploadFile {
			t.Fatalf("actions[%d].Kind = %q, want upload_file", i, action.Kind)
		}

		if action.Selector.Empty() || action.Paths.Empty() {
			t.Fatalf("actions[%d] must carry selector and paths, got %+v", i, action)
		}
	}

	if actions[0].Timeout.Empty() {
		t.Error("expected the first upload_file to carry a timeout")
	}
}

func TestLoadPathBrowserUploadFileValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		block string
		want  string
	}{
		{
			name:  "missing paths",
			block: `upload_file { selector = "input#document" }`,
			want:  "paths",
		},
		{
			name:  "missing selector",
			block: `upload_file { paths = ["./a.pdf"] }`,
			want:  "selector",
		},
		{
			name:  "unknown attribute",
			block: "upload_file {\n        selector = \"input#a\"\n        paths    = [\"./a.pdf\"]\n        value    = \"x\"\n      }",
			want:  "Unknown upload_file attribute",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := `version = 1

scenario "upload" {
  step "browser" "attach" {
    target = "chrome"
    actions {
      ` + tc.block + `
    }
  }
}
`

			_, diags := LoadPath(writeTales(t, content))
			if !diags.HasErrors() {
				t.Fatalf("expected diagnostics for %s", tc.name)
			}

			if !strings.Contains(diags.Error(), tc.want) {
				t.Fatalf("diagnostics = %s, want it to mention %q", diags.Error(), tc.want)
			}
		})
	}
}

func TestLoadPathBrowserForceOnPointerActions(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "force" {
  step "browser" "pointer" {
    target = "chrome"
    actions {
      click {
        selector = "#behind"
        force    = true
      }
      hover {
        selector = "#menu"
        force    = true
      }
      check {
        selector = "#tos"
        force    = true
      }
      uncheck {
        selector = "#news"
        force    = false
      }
      click {
        selector = "#plain"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	actions := suite.Scenarios[0].Steps[0].Browser.Actions
	if len(actions) != 5 {
		t.Fatalf("expected 5 actions, got %d", len(actions))
	}

	wantKinds := []model.BrowserActionKind{
		model.BrowserActionClick,
		model.BrowserActionHover,
		model.BrowserActionCheck,
		model.BrowserActionUncheck,
	}

	for i, kind := range wantKinds {
		if actions[i].Kind != kind {
			t.Fatalf("actions[%d].Kind = %q, want %q", i, actions[i].Kind, kind)
		}

		if actions[i].Force.Empty() {
			t.Fatalf("actions[%d] (%s) must carry the force expression", i, kind)
		}
	}

	if !actions[4].Force.Empty() {
		t.Fatalf("a click without force must leave the expression empty, got %+v", actions[4].Force)
	}
}

func TestLoadPathBrowserForceRejectedOnOtherActions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		block string
		want  string
	}{
		{
			name:  "clear",
			block: "clear {\n        selector = \"#a\"\n        force    = true\n      }",
			want:  `clear attribute "force" is not supported; allowed: selector, timeout, interval.`,
		},
		{
			name:  "submit",
			block: "submit {\n        selector = \"form\"\n        force    = true\n      }",
			want:  `submit attribute "force" is not supported`,
		},
		{
			name:  "wait_visible",
			block: "wait_visible {\n        selector = \"#a\"\n        force    = true\n      }",
			want:  `wait_visible attribute "force" is not supported`,
		},
		{
			name:  "fill",
			block: "fill {\n        selector = \"#a\"\n        value    = \"x\"\n        force    = true\n      }",
			want:  `fill attribute "force" is not supported`,
		},
		{
			name:  "goto",
			block: "goto {\n        url   = \"https://example.com\"\n        force = true\n      }",
			want:  `goto attribute "force" is not supported`,
		},
		{
			name:  "pointer action lists force among the allowed attributes",
			block: "click {\n        selector = \"#a\"\n        value    = \"x\"\n      }",
			want:  `click attribute "value" is not supported; allowed: selector, force, timeout, interval.`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := `version = 1

scenario "force" {
  step "browser" "pointer" {
    target = "chrome"
    actions {
      ` + tc.block + `
    }
  }
}
`

			_, diags := LoadPath(writeTales(t, content))
			if !diags.HasErrors() {
				t.Fatalf("expected diagnostics for %s", tc.name)
			}

			if !strings.Contains(diags.Error(), tc.want) {
				t.Fatalf("diagnostics = %s, want it to mention %q", diags.Error(), tc.want)
			}
		})
	}
}

func TestLoadPathBrowserMissingTargetAcceptedAtParseTime(t *testing.T) {
	t.Parallel()

	// `target` is optional at parse time. ResolveTarget() applies the
	// single-target shortcut (or surfaces a precise error) at execution
	// time. The parser must not block this.
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

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("did not expect diagnostics, got: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Browser == nil {
		t.Fatal("expected step.Browser to be populated")
	}

	if !step.Browser.Target.Empty() {
		t.Fatalf("expected empty target expression, got %+v", step.Browser.Target)
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

// browserPressContent wraps one press block in a minimal browser suite.
func browserPressContent(press string) string {
	return `version = 1

config {
  submit_key = "Enter"
}

scenario "s" {
  step "browser" "n" {
    target = "chrome"
    actions {
      ` + press + `
    }
  }
}
`
}

// A literal press key that is neither one character nor a key name is a
// load-time error: chromedp would type it letter by letter (issue #80), so
// `tales validate` must catch a typo before a run types it into a field.
func TestLoadPathBrowserPressRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
		want []string
	}{
		{name: "typo", key: `"Entr"`, want: []string{`"Entr"`, "Enter", "F12"}},
		{name: "wrong case", key: `"enter"`, want: []string{`did you mean "Enter"?`}},
		{name: "text", key: `"abc"`, want: []string{`"abc"`, "fill"}},
		{name: "empty", key: `""`, want: []string{`""`}},
		{name: "static template", key: `"En${"tr"}"`, want: []string{`"Entr"`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			press := "press {\n        selector = \"#field\"\n        key      = " + tc.key + "\n      }"

			_, diags := LoadPath(writeTales(t, browserPressContent(press)))
			if !diags.HasErrors() {
				t.Fatalf("expected a diagnostic for press key %s", tc.key)
			}

			msg := diags.Error()
			if !strings.Contains(msg, "Invalid press key") {
				t.Fatalf("expected an 'Invalid press key' diagnostic, got: %s", msg)
			}

			// The diagnostic points at the key attribute (line 13 of the
			// generated file), not at the press block.
			if !strings.Contains(msg, "case.tales:13,") {
				t.Fatalf("expected the diagnostic on the key line, got: %s", msg)
			}

			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("expected %q in diagnostics, got: %s", want, msg)
				}
			}
		})
	}
}

// Single characters and key names load, and so does a key computed from an
// expression: its value is only known at run time, where the provider
// checks it before pressing anything.
func TestLoadPathBrowserPressAcceptsKeys(t *testing.T) {
	t.Parallel()

	keys := []string{`"Enter"`, `"Tab"`, `"ArrowDown"`, `"F12"`, `"Space"`, `"a"`, `"\r"`, `" "`, `config.submit_key`, `env("TALES_PRESS_KEY", "Entr")`}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			press := "press {\n        key = " + key + "\n      }"

			suite, diags := LoadPath(writeTales(t, browserPressContent(press)))
			if diags.HasErrors() {
				t.Fatalf("unexpected diagnostics for press key %s: %s", key, diags.Error())
			}

			action := suite.Scenarios[0].Steps[0].Browser.Actions[0]
			if action.Kind != model.BrowserActionPress || action.Key.Empty() {
				t.Fatalf("expected a press action carrying its key, got %+v", action)
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
