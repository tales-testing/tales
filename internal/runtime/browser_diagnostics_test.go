package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
	browserprovider "github.com/tales-testing/tales/internal/provider/browser"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/tales-testing/tales/internal/report"
)

const browserTalesDiagnostics = `version = 1

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

scenario "browser diagnostics" {
  step "browser" "submit" {
    target = "chrome"
    actions {
      goto {
        url = "http://127.0.0.1/web/form"
      }
      click {
        selector = "#submit"
      }
      wait_visible {
        selector = "#done"
        timeout  = "50ms"
      }
    }
  }
}
`

// runBrowserDiagnostics runs browserTalesDiagnostics against fake, with
// artifacts written under a temporary directory.
func runBrowserDiagnostics(t *testing.T, fake *driver.FakeDriver) *report.StepResult {
	t.Helper()

	builder := browserprovider.SessionBuilderFunc{
		BuildFn: func(_ context.Context, target browserprovider.Target) (*browserprovider.Session, error) {
			return &browserprovider.Session{TargetName: target.Name, Target: target}, nil
		},
		ScenarioFn: func(_ context.Context, _ *browserprovider.Session, _ string) (*browserprovider.ScenarioBrowserCtx, error) {
			return &browserprovider.ScenarioBrowserCtx{Driver: fake}, nil
		},
	}

	browser := browserprovider.New(
		browserprovider.WithSessionBuilder(builder),
		browserprovider.WithCaptureMode(provider.CaptureFailures),
		browserprovider.WithArtifactsBase(t.TempDir()),
	)

	runner := NewRunner(provider.NewRegistry(browser))

	result, err := runner.Run(context.Background(), loadTales(t, browserTalesDiagnostics), Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	return result.Scenarios[0].Steps[0]
}

func TestRunnerBrowserActionsCarryTimingAndPoint(t *testing.T) {
	t.Parallel()

	fake := driver.NewFakeDriver()
	fake.PointerPoint = &driver.Point{X: 37.5, Y: 90}
	fake.Visibility["#done"] = true

	step := runBrowserDiagnostics(t, fake)
	if step.Status != report.StatusPass {
		t.Fatalf("step status = %q, failure=%+v", step.Status, step.Failure)
	}

	if len(step.Actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(step.Actions))
	}

	goTo, click, wait := step.Actions[0], step.Actions[1], step.Actions[2]

	if click.Point == nil || *click.Point != (report.ActionPoint{X: 37.5, Y: 90}) {
		t.Errorf("click point = %+v, want (37.5, 90)", click.Point)
	}

	if goTo.Point != nil || wait.Point != nil {
		t.Errorf("only pointer actions carry a point: goto=%+v wait=%+v", goTo.Point, wait.Point)
	}

	for _, a := range step.Actions {
		if a.StartedAt.IsZero() || a.EndedAt().Before(a.StartedAt) {
			t.Errorf("action %d (%s) timing: started=%v ended=%v", a.Index, a.Kind, a.StartedAt, a.EndedAt())
		}
	}

	if click.StartedAt.Before(goTo.EndedAt()) {
		t.Errorf("click started %v before goto ended %v", click.StartedAt, goTo.EndedAt())
	}
}

func TestRunnerBrowserFailureListsDiagnosticsArtifacts(t *testing.T) {
	t.Parallel()

	fake := driver.NewFakeDriver()
	fake.FailOnSelector = map[string]error{"#done": errors.New("selector #done not visible")}
	fake.ConsoleEntries = []driver.ConsoleEntry{{Type: driver.ConsoleTypeException, Level: "error", Text: "Uncaught Error: boom"}}
	fake.PageStateValue = &driver.PageState{URL: "http://127.0.0.1/web/form"}

	step := runBrowserDiagnostics(t, fake)
	if step.Status != report.StatusFail {
		t.Fatalf("step status = %q, want fail", step.Status)
	}

	types := map[string]bool{}
	for _, a := range step.Artifacts {
		types[a.Type] = true
	}

	for _, want := range []string{"console", "network", "state"} {
		if !types[want] {
			t.Errorf("step artifacts %+v miss %q", step.Artifacts, want)
		}
	}

	if failed := step.Actions[2]; failed.Status != report.StatusFail {
		t.Errorf("wait_visible status = %q, want fail", failed.Status)
	}
}
