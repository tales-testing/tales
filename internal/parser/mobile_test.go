package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
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
        id      = "welcome.register"
        timeout = "5s"
        interval = "100ms"
      }
      input_text {
        id      = "register.email"
        value   = "user@example.com"
        timeout = "3s"
      }
      input_text {
        id     = "register.password"
        value  = "Secret123!"
        secure = true
      }
      clear_text {
        id = "register.email"
      }
      wait_visible {
        id = "verify.screen"
      }
      wait_not_visible {
        id = "register.loading"
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
		model.MobileActionWaitVisible,
		model.MobileActionWaitNotVisible,
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

	if step.Mobile.Actions[0].Timeout.Empty() || step.Mobile.Actions[1].Timeout.Empty() {
		t.Fatal("expected action timeout expressions to be captured")
	}

	if step.Mobile.Actions[0].Interval.Empty() {
		t.Fatal("expected action interval expression to be captured")
	}
}

func TestLoadPathMobileRichExpectations(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "rich" {
  step "mobile" "verify" {
    platform = "ios"
    target = "iphone"
    expect {
      text {
        id = "home.title"
        value = contains("Welcome")
        timeout = "5s"
        interval = "100ms"
      }
      value {
        id = "register.email"
        value = "user@example.com"
      }
      enabled {
        id = "register.submit"
      }
      disabled {
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
	if len(step.Mobile.Expect.Text) != 1 {
		t.Fatalf("expected 1 text expectation, got %d", len(step.Mobile.Expect.Text))
	}
	if len(step.Mobile.Expect.Value) != 1 {
		t.Fatalf("expected 1 value expectation, got %d", len(step.Mobile.Expect.Value))
	}
	if len(step.Mobile.Expect.Enabled) != 1 {
		t.Fatalf("expected 1 enabled expectation, got %d", len(step.Mobile.Expect.Enabled))
	}
	if len(step.Mobile.Expect.Disabled) != 1 {
		t.Fatalf("expected 1 disabled expectation, got %d", len(step.Mobile.Expect.Disabled))
	}
	if step.Mobile.Expect.Text[0].Interval.Empty() {
		t.Fatal("expected text interval expression to be captured")
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

func TestLoadPathRejectsMobileAttributesOnNonMobileStep(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "bad-provider" {
  step "http" "looks_mobile" {
    platform = "ios"
    target = "iphone"
    request {
      method = "GET"
      url = "http://example.test"
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for mobile attributes on non-mobile step")
	}

	if !strings.Contains(diags.Error(), "Mobile fields on non-mobile step") {
		t.Fatalf("expected mobile-fields diagnostic, got: %s", diags.Error())
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
      pinch {
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
    platform = "ios"
    request {
      method = "GET"
      url = "http://localhost"
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

func TestLoadPathMobilePermissionsDecodedSortedByService(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "perms" {
  step "mobile" "launch" {
    platform = "ios"
    target = "iphone"
    permissions {
      photos = "deny"
      camera = "allow"
      contacts = "allow"
    }
    launch {
      clear_state = true
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

	// The decoder sorts permissions by service name so the decoded order
	// is deterministic regardless of HCL map iteration order.
	wantServices := []string{"camera", "contacts", "photos"}
	if len(step.Mobile.Permissions) != len(wantServices) {
		t.Fatalf("expected %d permissions, got %d", len(wantServices), len(step.Mobile.Permissions))
	}

	for i, service := range wantServices {
		if step.Mobile.Permissions[i].Service != service {
			t.Fatalf("permission %d: want service %q, got %q", i, service, step.Mobile.Permissions[i].Service)
		}

		if step.Mobile.Permissions[i].Decision.Empty() {
			t.Fatalf("permission %d (%s): expected decision expression to be captured", i, service)
		}
	}
}

func TestLoadPathMobileDeviceActionsDecoded(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "device" {
  step "mobile" "device" {
    platform = "ios"
    target = "iphone"
    actions {
      press_key { key = "return" }
      press_button { button = "home" }
      set_orientation { orientation = "landscape_left" }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	actions := suite.Scenarios[0].Steps[0].Mobile.Actions

	want := []model.MobileActionKind{
		model.MobileActionPressKey,
		model.MobileActionPressButton,
		model.MobileActionSetOrientation,
	}
	if len(actions) != len(want) {
		t.Fatalf("expected %d actions, got %d", len(want), len(actions))
	}

	for i, kind := range want {
		if actions[i].Kind != kind {
			t.Fatalf("action %d: want %q got %q", i, kind, actions[i].Kind)
		}

		// Device actions carry their argument in Value and never an id.
		if !actions[i].ID.Empty() {
			t.Fatalf("action %d (%s): expected no id", i, kind)
		}

		if actions[i].Value.Empty() {
			t.Fatalf("action %d (%s): expected value expression to be captured", i, kind)
		}
	}
}

func TestLoadPathMobileDeviceActionRejectsMissingArg(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "bad-device" {
  step "mobile" "device" {
    platform = "ios"
    target = "iphone"
    actions {
      press_key {}
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for press_key without key")
	}

	if !strings.Contains(diags.Error(), "Missing press_key attribute") {
		t.Fatalf("expected missing-key diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileGestureActionsDecoded(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "gestures" {
  step "mobile" "gestures" {
    platform = "ios"
    target = "iphone"
    actions {
      swipe {
        id        = "feed.list"
        direction = "up"
        distance  = 0.6
        duration  = "300ms"
      }
      scroll {
        id        = "feed.list"
        direction = "down"
      }
      long_press {
        id       = "feed.item"
        duration = "1s"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	actions := suite.Scenarios[0].Steps[0].Mobile.Actions
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}

	swipe := actions[0]
	if swipe.Kind != model.MobileActionSwipe {
		t.Fatalf("action 0: want swipe, got %q", swipe.Kind)
	}

	if swipe.Direction.Empty() || swipe.Distance.Empty() || swipe.Duration.Empty() {
		t.Fatal("expected swipe direction/distance/duration expressions to be captured")
	}

	scroll := actions[1]
	if scroll.Kind != model.MobileActionScroll {
		t.Fatalf("action 1: want scroll, got %q", scroll.Kind)
	}

	// distance / duration are optional and stay empty when omitted.
	if !scroll.Distance.Empty() || !scroll.Duration.Empty() {
		t.Fatal("expected omitted scroll distance/duration to stay empty")
	}

	longPress := actions[2]
	if longPress.Kind != model.MobileActionLongPress {
		t.Fatalf("action 2: want long_press, got %q", longPress.Kind)
	}

	if longPress.Duration.Empty() {
		t.Fatal("expected long_press duration expression to be captured")
	}
}

func TestLoadPathMobileSwipeRejectsMissingDirection(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "bad-swipe" {
  step "mobile" "swipe" {
    platform = "ios"
    target = "iphone"
    actions {
      swipe {
        id = "feed.list"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for swipe without direction")
	}

	if !strings.Contains(diags.Error(), "Missing swipe attribute") {
		t.Fatalf("expected missing-direction diagnostic, got: %s", diags.Error())
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
