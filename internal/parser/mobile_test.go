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

func TestLoadPathMobileAcceptsEverySupportedPlatform(t *testing.T) {
	t.Parallel()

	for _, platform := range supportedMobilePlatforms {
		t.Run(platform, func(t *testing.T) {
			t.Parallel()

			content := `version = 1

scenario "platform" {
  step "mobile" "terminate" {
    platform = "` + platform + `"
    target = "phone"
    terminate {}
  }
}
`

			if _, diags := LoadPath(writeTales(t, content)); diags.HasErrors() {
				t.Fatalf("platform %q should parse, got: %s", platform, diags.Error())
			}
		})
	}
}

func TestLoadPathMobileRejectsUnknownPlatform(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "windows-phone" {
  step "mobile" "launch" {
    platform = "windows"
    target = "phone"
    terminate {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for an unknown platform")
	}

	// The message has to name the alternatives: a typo'd platform is the
	// likeliest cause, and listing them turns the error into the fix.
	msg := diags.Error()

	for _, want := range []string{"windows", "android", "ios"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic should mention %q, got: %s", want, msg)
		}
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

	if !strings.Contains(diags.Error(), "Missing element locator") {
		t.Fatalf("expected missing element locator diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileActionsFirstAttribute(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "system-picker" {
  step "mobile" "pick" {
    platform = "ios"
    target = "iphone"
    actions {
      wait_visible {
        id      = "PXGGridLayout-Info"
        first   = true
        timeout = "10s"
      }
      tap {
        id    = "PXGGridLayout-Info"
        first = true
      }
      double_tap {
        id    = "system-cell"
        first = true
      }
      long_press {
        id       = "system-cell"
        first    = true
        duration = "1s"
      }
      input_text {
        id    = "system-cell"
        value = "v"
        first = true
      }
      clear_text {
        id    = "system-cell"
        first = true
      }
      swipe {
        id        = "PXGGridLayout-Info"
        direction = "up"
        first     = true
      }
      scroll {
        id        = "PXGGridLayout-Info"
        direction = "down"
        first     = true
      }
      wait_not_visible {
        id    = "loading"
        first = true
      }
      tap {
        id = "no-first"
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
	if len(actions) != 10 {
		t.Fatalf("expected 10 actions, got %d", len(actions))
	}

	for i := range 9 {
		if actions[i].First.Empty() {
			t.Fatalf("action %d (%s): expected First expression to be captured", i, actions[i].Kind)
		}
	}

	if !actions[9].First.Empty() {
		t.Fatal("expected the last tap (no first attribute) to leave First empty")
	}
}

func TestLoadPathMobileRejectsUnknownFirstSibling(t *testing.T) {
	t.Parallel()

	// Regression: error messages must list "first" in the allowed set so the
	// hint stays accurate after the attribute was added.
	content := `version = 1

scenario "bad-attr" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    actions {
      tap {
        id    = "x"
        nopes = true
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics for unknown tap attribute")
	}

	got := diags.Error()
	if !strings.Contains(got, "Unknown tap attribute") {
		t.Fatalf("expected unknown-attribute diagnostic, got: %s", got)
	}

	if !strings.Contains(got, "first") {
		t.Fatalf("expected the allowed-attributes hint to list 'first', got: %s", got)
	}
}

func TestLoadPathMobileActionsLabelAttribute(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "system-picker" {
  step "mobile" "pick" {
    platform = "ios"
    target = "iphone"
    actions {
      wait_visible {
        label   = "Done"
        timeout = "10s"
      }
      tap {
        label = "Done"
      }
      double_tap {
        label = "Done"
      }
      long_press {
        label    = "Done"
        duration = "1s"
      }
      input_text {
        label = "Done"
        value = "v"
      }
      clear_text {
        label = "Done"
      }
      swipe {
        label     = "Done"
        direction = "up"
      }
      scroll {
        label     = "Done"
        direction = "down"
      }
      wait_not_visible {
        label = "Done"
      }
    }
    expect {
      visible {
        label = "Done"
      }
      not_visible {
        label = "Cancel"
      }
      text {
        label = "Done"
        value = "Done"
      }
      value {
        label = "Done"
        value = "Done"
      }
      enabled {
        label = "Done"
      }
      disabled {
        label = "Done"
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	mobile := suite.Scenarios[0].Steps[0].Mobile
	if len(mobile.Actions) != 9 {
		t.Fatalf("expected 9 actions, got %d", len(mobile.Actions))
	}

	for i, action := range mobile.Actions {
		if action.Label.Empty() {
			t.Fatalf("action %d (%s): expected Label expression to be captured", i, action.Kind)
		}

		if !action.ID.Empty() {
			t.Fatalf("action %d (%s): expected ID expression to be empty when only label is set", i, action.Kind)
		}
	}

	if len(mobile.Expect.Visible) != 1 || mobile.Expect.Visible[0].Label.Empty() || !mobile.Expect.Visible[0].ID.Empty() {
		t.Fatalf("expected visible expect to carry Label and empty ID, got %+v", mobile.Expect.Visible)
	}

	if mobile.Expect.Text[0].Label.Empty() || mobile.Expect.Value[0].Label.Empty() {
		t.Fatal("expected text/value expects to carry Label")
	}

	if mobile.Expect.Enabled[0].Label.Empty() || mobile.Expect.Disabled[0].Label.Empty() {
		t.Fatal("expected enabled/disabled expects to carry Label")
	}
}

func TestLoadPathMobileRejectsIDAndLabelTogether(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "conflict" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    actions {
      tap {
        id    = "welcome.signin"
        label = "Sign in"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when both id and label are set")
	}

	if !strings.Contains(diags.Error(), "Conflicting element locator") {
		t.Fatalf("expected Conflicting element locator diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsMissingLocator(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "missing" {
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
		t.Fatal("expected diagnostics when neither id nor label is set")
	}

	if !strings.Contains(diags.Error(), "Missing element locator") {
		t.Fatalf("expected Missing element locator diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileExpectRejectsIDAndLabelTogether(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "expect-conflict" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    expect {
      visible {
        id    = "welcome.signin"
        label = "Sign in"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when both id and label are set in an expect block")
	}

	if !strings.Contains(diags.Error(), "Conflicting element locator") {
		t.Fatalf("expected Conflicting element locator diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileExpectRejectsMissingLocator(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "expect-missing" {
  step "mobile" "do" {
    platform = "ios"
    target = "iphone"
    expect {
      visible {}
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when neither id nor label is set in an expect block")
	}

	if !strings.Contains(diags.Error(), "Missing element locator") {
		t.Fatalf("expected Missing element locator diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileDismissKeyboardActionDecodes(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "form" {
  step "mobile" "fill" {
    platform = "ios"
    target = "iphone"
    actions {
      input_text {
        id    = "form.name"
        value = "Alice"
      }
      dismiss_keyboard {}
      wait_visible {
        id = "form.submit"
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

	if actions[1].Kind != model.MobileActionDismissKeyboard {
		t.Fatalf("expected dismiss_keyboard action, got %q", actions[1].Kind)
	}

	if !actions[1].ID.Empty() || !actions[1].Label.Empty() || !actions[1].Value.Empty() {
		t.Fatal("dismiss_keyboard must carry no locator or value expression")
	}
}

func TestLoadPathMobileScrollToDecodes(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "form" {
  step "mobile" "fill" {
    platform = "ios"
    target = "iphone"
    actions {
      scroll_to {
        id = "form.identifier_value"
      }
      input_text {
        id    = "form.identifier_value"
        value = "123456789"
      }
      scroll_to {
        label = "Done"
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

	if actions[0].Kind != model.MobileActionScrollTo || actions[0].ID.Empty() {
		t.Fatalf("expected first scroll_to to carry an ID, got %+v", actions[0])
	}

	if actions[2].Kind != model.MobileActionScrollTo || actions[2].Label.Empty() {
		t.Fatalf("expected second scroll_to to carry a Label, got %+v", actions[2])
	}
}

func TestLoadPathMobileScrollToRejectsIDAndLabelTogether(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "form" {
  step "mobile" "fill" {
    platform = "ios"
    target = "iphone"
    actions {
      scroll_to {
        id    = "form.field"
        label = "Done"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when scroll_to carries both id and label")
	}

	if !strings.Contains(diags.Error(), "Conflicting element locator") {
		t.Fatalf("expected Conflicting element locator, got: %s", diags.Error())
	}
}

func TestLoadPathMobileScrollToRejectsTimeout(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "form" {
  step "mobile" "fill" {
    platform = "ios"
    target = "iphone"
    actions {
      scroll_to {
        id      = "form.field"
        timeout = "5s"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when scroll_to carries an unsupported attribute")
	}

	if !strings.Contains(diags.Error(), "Unknown scroll_to attribute") {
		t.Fatalf("expected Unknown scroll_to attribute diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathMobileDismissKeyboardRejectsAttributes(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "form" {
  step "mobile" "fill" {
    platform = "ios"
    target = "iphone"
    actions {
      dismiss_keyboard {
        timeout = "5s"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected diagnostics when dismiss_keyboard carries an attribute")
	}

	if !strings.Contains(diags.Error(), "Unknown dismiss_keyboard attribute") {
		t.Fatalf("expected Unknown dismiss_keyboard attribute diagnostic, got: %s", diags.Error())
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

func TestLoadPathMobileAcceptsTheTextLocator(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "text-locator" {
  step "mobile" "confirm" {
    platform = "android"
    target   = "phone"
    actions {
      tap {
        text = "Allow"
      }
    }
    expect {
      visible {
        text = "Allowed"
      }
    }
  }
}
`

	if _, diags := LoadPath(writeTales(t, content)); diags.HasErrors() {
		t.Fatalf("the text locator should parse, got: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsTwoLocatorsOnOneAction(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "conflict" {
  step "mobile" "confirm" {
    platform = "android"
    target   = "phone"
    actions {
      tap {
        id   = "dialog.allow"
        text = "Allow"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected an error when two locators are set")
	}

	msg := diags.Error()

	if !strings.Contains(msg, "Conflicting element locator") {
		t.Fatalf("unexpected diagnostic: %s", msg)
	}

	// The message lists every locator, so an author who set the wrong
	// pair can see the full menu rather than guessing.
	for _, want := range []string{"id", "label", "text"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic should mention %q, got: %s", want, msg)
		}
	}
}

func TestLoadPathMobileRejectsTwoLocatorsOnOneExpect(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "conflict" {
  step "mobile" "confirm" {
    platform = "android"
    target   = "phone"
    expect {
      visible {
        label = "Allow"
        text  = "Allow"
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected an error when two locators are set on an expect block")
	}

	if !strings.Contains(diags.Error(), "Conflicting element locator") {
		t.Fatalf("unexpected diagnostic: %s", diags.Error())
	}
}
