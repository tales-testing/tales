package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
	browserprovider "github.com/tales-testing/tales/internal/provider/browser"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

func newStubBrowserProvider(drv *driver.FakeDriver) *browserprovider.Provider {
	builder := browserprovider.SessionBuilderFunc{
		BuildFn: func(_ context.Context, target browserprovider.Target) (*browserprovider.Session, error) {
			return &browserprovider.Session{TargetName: target.Name, Target: target}, nil
		},
		ScenarioFn: func(_ context.Context, _ *browserprovider.Session, _ string) (*browserprovider.ScenarioBrowserCtx, error) {
			return &browserprovider.ScenarioBrowserCtx{Driver: drv}, nil
		},
	}

	return browserprovider.New(browserprovider.WithSessionBuilder(builder))
}

const browserTalesLoginFlow = `version = 1

config {
  base_url = "https://example.com"
  browser = {
    targets = {
      chrome = {
        browser  = "chrome"
        headless = true
      }
    }
  }
}

scenario "browser login" {
  step "browser" "login" {
    target = "chrome"
    actions {
      goto {
        url = "${config.base_url}/web/login"
      }
      fill {
        selector = "[data-testid='login.email']"
        value    = "demo@example.com"
      }
      fill {
        selector = "[data-testid='login.password']"
        value    = "secret"
        secure   = true
      }
      click {
        selector = "[data-testid='login.submit']"
      }
    }
    expect {
      url {
        value = "https://example.com/web/dashboard"
      }
      title {
        value = "Dashboard"
      }
    }
    capture {
      title_text = text("h1")
      csrf       = attribute("meta[name='csrf-token']", "content")
      current    = browser.url
      page_title = browser.title
    }
  }
}
`

func TestRunnerExecutesBrowserStep(t *testing.T) {
	t.Parallel()

	fake := driver.NewFakeDriver()
	fake.URLValue = "https://example.com/web/dashboard"
	fake.TitleValue = "Dashboard"
	fake.OuterHTMLValue = `<html><head><title>Dashboard</title><meta name="csrf-token" content="csrf-demo-token"></head><body><h1>Hello demo</h1></body></html>`

	suite := loadTales(t, browserTalesLoginFlow)

	runner := NewRunner(provider.NewRegistry(newStubBrowserProvider(fake)))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	if result == nil || len(result.Scenarios) != 1 {
		t.Fatalf("expected one scenario, got %+v", result)
	}

	scenario := result.Scenarios[0]
	if scenario.Status != "pass" {
		t.Fatalf("scenario status = %q, want pass; failure=%+v", scenario.Status, scenario.Steps)
	}

	if len(scenario.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(scenario.Steps))
	}

	step := scenario.Steps[0]
	if step.Status != "pass" {
		t.Fatalf("step status = %q, want pass; failure=%+v", step.Status, step.Failure)
	}

	if len(step.Actions) != 4 {
		t.Fatalf("expected 4 action results, got %d", len(step.Actions))
	}

	// Secure fill masked to ***.
	masked := step.Actions[2]
	if masked.Value != "***" {
		t.Errorf("secure fill value = %q, want ***", masked.Value)
	}

	// The console / network recording starts before the first action, so
	// a failure can show what led to it (issue #83).
	calls := fake.MethodsCalled()
	want := []string{"StartDiagnostics", "Goto", "Fill", "Fill", "Click"}

	for i, w := range want {
		if calls[i] != w {
			t.Errorf("call[%d] = %q, want %q (full: %v)", i, calls[i], w, calls)
		}
	}
}

const browserTalesForce = `version = 1

config {
  browser = {
    targets = {
      chrome = {
        browser  = "chrome"
        headless = true
      }
    }
  }
}

scenario "browser force" {
  step "browser" "pointer" {
    target = "chrome"
    vars {
      deliberate = true
    }
    actions {
      click {
        selector = "#behind"
        force    = vars.deliberate
      }
      hover {
        selector = "#menu"
      }
      check {
        selector = "#tos"
        force    = true
      }
      uncheck {
        selector = "#news"
        force    = false
      }
    }
  }
}
`

func TestRunnerBrowserPointerActionsCarryForce(t *testing.T) {
	t.Parallel()

	fake := driver.NewFakeDriver()
	suite := loadTales(t, browserTalesForce)

	runner := NewRunner(provider.NewRegistry(newStubBrowserProvider(fake)))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != "pass" {
		t.Fatalf("step status = %q, want pass; failure=%+v", step.Status, step.Failure)
	}

	want := []driver.Call{
		{Method: "Click", Selector: "#behind", Force: true},
		{Method: "Hover", Selector: "#menu", Force: false},
		{Method: "Check", Selector: "#tos", Force: true},
		{Method: "Uncheck", Selector: "#news", Force: false},
	}

	pointerCalls := make([]driver.Call, 0, len(want))

	for _, call := range fake.Calls {
		switch call.Method {
		case "Click", "Hover", "Check", "Uncheck":
			pointerCalls = append(pointerCalls, call)
		}
	}

	if len(pointerCalls) != len(want) {
		t.Fatalf("pointer calls = %+v, want %+v", pointerCalls, want)
	}

	for i, w := range want {
		if pointerCalls[i] != w {
			t.Errorf("pointer call[%d] = %+v, want %+v", i, pointerCalls[i], w)
		}
	}

	actions, ok := step.Request["actions"].([]map[string]any)
	if !ok {
		t.Fatalf("request.actions = %T, want []map[string]any", step.Request["actions"])
	}

	if actions[0]["force"] != true {
		t.Errorf("request.actions[0].force = %v, want true", actions[0]["force"])
	}

	if _, present := actions[1]["force"]; present {
		t.Errorf("request.actions[1] must not carry force when it is not set: %+v", actions[1])
	}

	if got := step.Actions[0].Label; got != "click #behind (force)" {
		t.Errorf("action label = %q, want %q", got, "click #behind (force)")
	}
}

func TestRunnerBrowserForceMustBeABool(t *testing.T) {
	t.Parallel()

	fake := driver.NewFakeDriver()
	suite := loadTales(t, `version = 1

config {
  browser = {
    targets = {
      chrome = {
        browser  = "chrome"
        headless = true
      }
    }
  }
}

scenario "browser force" {
  step "browser" "pointer" {
    target = "chrome"
    actions {
      click {
        selector = "#behind"
        force    = "always"
      }
    }
  }
}
`)

	runner := NewRunner(provider.NewRegistry(newStubBrowserProvider(fake)))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != "fail" || step.Failure == nil {
		t.Fatalf("step = %+v, want an evaluation failure", step)
	}

	if !strings.Contains(step.Failure.Message, "browser.actions[0].force") {
		t.Errorf("failure = %q, want it to name browser.actions[0].force", step.Failure.Message)
	}

	if len(fake.Calls) != 0 {
		t.Errorf("no driver call expected before the step evaluates, got %v", fake.MethodsCalled())
	}
}
