package runtime

import (
	"context"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
)

func suiteTeardownOptions(t *testing.T) Options {
	t.Helper()

	return Options{Seed: 1, Parallel: 1, ArtifactsBase: t.TempDir()}
}

func TestSuiteTeardownRunsAfterEveryScenario(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{newHTTPStep("purge")},
		Scenarios: []*model.Scenario{
			{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("first")}},
			{Name: "b", File: "x.tales", Steps: []*model.Step{newHTTPStep("second")}},
		},
	}

	result, err := runner.Run(context.Background(), suite, suiteTeardownOptions(t))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Teardown) != 1 || result.Teardown[0].Status != report.StatusPass {
		t.Fatalf("suite teardown should run and pass, got %+v", result.Teardown)
	}

	if result.Teardown[0].Phase != phaseSuiteTeardown {
		t.Fatalf("phase = %q, want %q", result.Teardown[0].Phase, phaseSuiteTeardown)
	}

	calls := fp.callNames()
	if len(calls) != 3 || calls[2] != "purge" {
		t.Fatalf("call order = %v, want the suite teardown last", calls)
	}
}

// The suite teardown must also fire when a scenario failed: the failing run
// is exactly the one that leaves resources behind.
func TestSuiteTeardownRunsAfterScenarioFailure(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{newHTTPStep("purge")},
		Scenarios:    []*model.Scenario{{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}}},
	}

	result, err := runner.Run(context.Background(), suite, suiteTeardownOptions(t))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Teardown) != 1 || result.Teardown[0].Status != report.StatusPass {
		t.Fatalf("suite teardown should run after a failure, got %+v", result.Teardown)
	}
}

// A safety net that only fires when the filter happened to match something is
// not a safety net.
func TestSuiteTeardownRunsWhenNoScenarioMatches(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{newHTTPStep("purge")},
		Scenarios:    []*model.Scenario{{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}}},
	}

	opts := suiteTeardownOptions(t)
	opts.Scenario = "does-not-exist"

	result, err := runner.Run(context.Background(), suite, opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Scenarios) != 0 {
		t.Fatalf("scenarios = %d, want 0 after filtering", len(result.Scenarios))
	}

	if len(result.Teardown) != 1 || result.Teardown[0].Status != report.StatusPass {
		t.Fatalf("suite teardown should run with no scenario selected, got %+v", result.Teardown)
	}
}

// Cleanup steps are independent obligations, not a pipeline: one failing must
// not skip the ones after it, and the run must fail even if every scenario
// passed.
func TestSuiteTeardownFailureDoesNotStopLaterStepsAndFailsTheRun(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{"broken": true}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{newHTTPStep("broken"), newHTTPStep("still_runs")},
		Scenarios:    []*model.Scenario{{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}}},
	}

	result, err := runner.Run(context.Background(), suite, suiteTeardownOptions(t))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.Teardown) != 2 {
		t.Fatalf("teardown results = %d, want 2", len(result.Teardown))
	}

	if result.Teardown[0].Status != report.StatusFail || result.Teardown[1].Status != report.StatusPass {
		t.Fatalf("unexpected teardown statuses: %+v", result.Teardown)
	}

	if len(result.TeardownFailures) != 1 {
		t.Fatalf("teardown failures = %d, want 1", len(result.TeardownFailures))
	}

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario status = %q, want pass", result.Scenarios[0].Status)
	}

	if !result.Failed() {
		t.Fatal("a failing suite teardown must fail the run")
	}
}

// Later cleanup steps must be able to consume what earlier ones captured.
func TestSuiteTeardownStepsShareResults(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	second := newHTTPStep("second")
	second.Request.URL = expr(`"http://example.test/${result.first.id}"`)

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{newHTTPStep("first"), second},
		Scenarios:    []*model.Scenario{{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}}},
	}

	result, err := runner.Run(context.Background(), suite, suiteTeardownOptions(t))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Teardown[1].Status != report.StatusPass {
		t.Fatalf("second teardown step should read result.first, got %+v", result.Teardown[1])
	}
}

func TestSuiteTeardownHonorsWhen(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	guarded := newHTTPStep("guarded")
	guarded.When = expr(`can(result.never.id)`)

	suite := &model.Suite{
		TeardownFile: "x.tales",
		Teardown:     []*model.Step{guarded},
		Scenarios:    []*model.Scenario{{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}}},
	}

	result, err := runner.Run(context.Background(), suite, suiteTeardownOptions(t))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Teardown[0].Status != report.StatusSkip {
		t.Fatalf("guarded suite teardown step should be skipped, got %+v", result.Teardown[0])
	}
}

// The seed mix of a suite teardown step must not depend on how many scenarios
// ran, on --parallel, or on any filter: the pseudo-scenario name is a literal
// precisely so replaying a run reproduces the same generated values.
func TestSuiteTeardownSeedIsIndependentOfScheduling(t *testing.T) {
	t.Parallel()

	generated := func(t *testing.T, parallel int, scenarioFilter string) string {
		t.Helper()

		fp := &fakeProvider{failFor: map[string]bool{}}
		runner := NewRunner(provider.NewRegistry(fp))

		purge := newHTTPStep("purge")
		purge.Request.URL = expr(`"http://example.test/${generate("cleanup_email")}"`)

		suite := &model.Suite{
			Generators:   map[string]*model.Generator{"cleanup_email": {Type: "email", Name: "cleanup_email"}},
			TeardownFile: "x.tales",
			Teardown:     []*model.Step{purge},
			Scenarios: []*model.Scenario{
				{Name: "a", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}},
				{Name: "b", File: "x.tales", Steps: []*model.Step{newHTTPStep("main")}},
			},
		}

		opts := suiteTeardownOptions(t)
		opts.Seed = 4242
		opts.Parallel = parallel
		opts.Scenario = scenarioFilter

		result, err := runner.Run(context.Background(), suite, opts)
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		url, ok := result.Teardown[0].Request["url"].(string)
		if !ok {
			t.Fatalf("teardown request url missing: %+v", result.Teardown[0].Request)
		}

		return url
	}

	base := generated(t, 1, "")

	if got := generated(t, 8, ""); got != base {
		t.Fatalf("suite teardown seed changed with --parallel: %q vs %q", got, base)
	}

	if got := generated(t, 1, "a"); got != base {
		t.Fatalf("suite teardown seed changed with --scenario: %q vs %q", got, base)
	}
}
