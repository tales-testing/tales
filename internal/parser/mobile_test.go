package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
)

func writeTales(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "case.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tales: %v", err)
	}

	return dir
}

func TestLoadPathMobileLaunchTerminate(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "demo" {
  step "mobile" "launch" {
    platform = "ios"
    target = "iphone"
    launch {
      clear_state = true
    }
    expect {
      visible {
        id = "welcome.register"
        timeout = "20s"
      }
    }
  }

  step "mobile" "terminate" {
    platform = "ios"
    target = "iphone"
    terminate {}
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Scenarios) != 1 || len(suite.Scenarios[0].Steps) != 2 {
		t.Fatalf("expected 1 scenario with 2 steps, got %d/%d", len(suite.Scenarios), len(suite.Scenarios[0].Steps))
	}

	launch := suite.Scenarios[0].Steps[0]
	if launch.Mobile == nil {
		t.Fatal("expected step.Mobile to be populated")
	}

	if launch.Mobile.Launch == nil || launch.Mobile.Launch.ClearState.Empty() {
		t.Fatal("expected launch.clear_state to be parsed")
	}

	if len(launch.Mobile.Expect.Visible) != 1 {
		t.Fatalf("expected 1 visible expectation, got %d", len(launch.Mobile.Expect.Visible))
	}

	terminate := suite.Scenarios[0].Steps[1]
	if terminate.Mobile == nil || terminate.Mobile.Terminate == nil {
		t.Fatal("expected terminate block to be parsed")
	}
}

func TestLoadPathMobileActionsPreservesOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "register" {
  step "mobile" "register" {
    platform = "ios"
    target = "iphone"
    actions {
      tap {
        id = "welcome.register"
      }
      input_text {
        id    = "register.email"
        value = "user@example.com"
      }
      input_text {
        id     = "register.password"
        value  = "Secret123!"
        secure = true
      }
      clear_text {
        id = "register.email"
      }
      tap {
        id = "register.submit"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Mobile == nil {
		t.Fatal("expected step.Mobile to be populated")
	}

	want := []model.MobileActionKind{
		model.MobileActionTap,
		model.MobileActionInputText,
		model.MobileActionInputText,
		model.MobileActionClearText,
		model.MobileActionTap,
	}

	if len(step.Mobile.Actions) != len(want) {
		t.Fatalf("expected %d actions, got %d", len(want), len(step.Mobile.Actions))
	}

	for i, kind := range want {
		if step.Mobile.Actions[i].Kind != kind {
			t.Fatalf("action %d: want %q got %q", i, kind, step.Mobile.Actions[i].Kind)
		}
	}

	if step.Mobile.Actions[2].Secure.Empty() {
		t.Fatal("expected secure expression to be captured on the second input_text")
	}
}

func TestLoadPathMobileRejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "android-attempt" {
  step "mobile" "launch" {
    platform = "android"
    target = "phone"
    terminate {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for unsupported platform")
	}

	if !strings.Contains(diags.Error(), "not supported yet") {
		t.Fatalf("expected unsupported-platform diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsMissingTarget(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "missing-target" {
  step "mobile" "launch" {
    platform = "ios"
    terminate {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for missing target")
	}

	if !strings.Contains(diags.Error(), "Missing mobile target") {
		t.Fatalf("expected missing-target diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsMissingPlatform(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "missing-platform" {
  step "mobile" "launch" {
    target = "iphone"
    terminate {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for missing platform")
	}

	if !strings.Contains(diags.Error(), "Missing mobile platform") {
		t.Fatalf("expected missing-platform diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "bad-action" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    actions {
      swipe {
        id = "foo"
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
		t.Fatalf("expected unknown-action diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsMissingActionID(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "missing-id" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    actions {
      tap {}
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for tap without id")
	}

	if !strings.Contains(diags.Error(), "Missing tap attribute") {
		t.Fatalf("expected missing id diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsMobileFieldsOnHTTPStep(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "wrong-provider" {
  step "http" "x" {
    request {
      method = "GET"
      url = "http://localhost"
    }
    actions {
      tap { id = "a" }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for mobile fields on http step")
	}

	if !strings.Contains(diags.Error(), "Mobile fields on non-mobile step") {
		t.Fatalf("expected mobile-on-non-mobile diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileNotVisible(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "not-visible" {
  step "mobile" "verify" {
    platform = "ios"
    target = "iphone"
    expect {
      not_visible {
        id = "login.error"
        timeout = "5s"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Mobile == nil {
		t.Fatal("expected mobile step to be populated")
	}

	if len(step.Mobile.Expect.NotVisible) != 1 {
		t.Fatalf("expected 1 not_visible entry, got %d", len(step.Mobile.Expect.NotVisible))
	}
}
