package runtime

import (
	"context"
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

	calls := fake.MethodsCalled()
	want := []string{"Goto", "Fill", "Fill", "Click"}

	for i, w := range want {
		if calls[i] != w {
			t.Errorf("call[%d] = %q, want %q (full: %v)", i, calls[i], w, calls)
		}
	}
}
