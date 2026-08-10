package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
)

// callNames returns the provider call log. `when` regressions are only
// observable through it: a step wrongly reported as skipped while the
// provider still ran would otherwise pass unnoticed.
func (p *fakeProvider) callNames() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.calls...)
}

func findStep(steps []*report.StepResult, name string) *report.StepResult {
	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}

	return nil
}

func TestWhenFalseSkipsScenarioStep(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	guarded := newHTTPStep("guarded")
	guarded.When = expr(`false`)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main"), guarded},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	step := findStep(result.Scenarios[0].Steps, "guarded")
	if step == nil {
		t.Fatal("guarded step missing from the report")
	}

	if step.Status != report.StatusSkip {
		t.Fatalf("guarded step status = %q, want skipped", step.Status)
	}

	if step.SkipReason != whenFalseReason {
		t.Fatalf("skip reason = %q, want %q", step.SkipReason, whenFalseReason)
	}

	for _, name := range fp.callNames() {
		if name == "guarded" {
			t.Fatal("provider ran a step gated off by when")
		}
	}
}

func TestWhenTrueRunsScenarioStep(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	guarded := newHTTPStep("guarded")
	guarded.When = expr(`can(result.main.id)`)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main"), guarded},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	step := findStep(result.Scenarios[0].Steps, "guarded")
	if step == nil || step.Status != report.StatusPass {
		t.Fatalf("guarded step should pass, got %+v", step)
	}
}

// A step skipped by `when` must cascade to the steps that consume its
// results, exactly like skip_if / skip_unless does.
func TestWhenSkipCascadesToDependents(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	guarded := newHTTPStep("guarded")
	guarded.When = expr(`false`)

	dependent := newHTTPStep("dependent")
	dependent.Request.URL = expr(`"http://example.test/${result.guarded.id}"`)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{guarded, dependent},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	step := findStep(result.Scenarios[0].Steps, "dependent")
	if step == nil || step.Status != report.StatusSkip {
		t.Fatalf("dependent step should cascade-skip, got %+v", step)
	}

	if !strings.Contains(step.SkipReason, "guarded") {
		t.Fatalf("cascade reason = %q, want it to name the blocker", step.SkipReason)
	}

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario status = %q, want pass", result.Scenarios[0].Status)
	}
}

// An unevaluable `when` skips rather than fails (the can() guard depends on
// it), but the reason must say so instead of staying silent.
func TestWhenEvaluationErrorSkipsWithReason(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	guarded := newHTTPStep("guarded")
	guarded.When = expr(`"not a bool"`)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{guarded},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	step := findStep(result.Scenarios[0].Steps, "guarded")
	if step == nil || step.Status != report.StatusSkip {
		t.Fatalf("guarded step should be skipped, got %+v", step)
	}

	if !strings.Contains(step.SkipReason, "failed to evaluate") {
		t.Fatalf("skip reason = %q, want an evaluation-failure explanation", step.SkipReason)
	}
}

func TestWhenFalseSkipsKeywordStep(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	inner := newHTTPStep("inner_guarded")
	inner.When = expr(`false`)

	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"flow": {
				Name:  "flow",
				Steps: []*model.Step{newHTTPStep("inner_main"), inner},
			},
		},
		Scenarios: []*model.Scenario{{
			Name: "s",
			File: "x.tales",
			Steps: []*model.Step{{
				Provider: "keyword",
				Name:     "call_flow",
				Keyword:  &model.KeywordCall{Name: expr(`"flow"`)},
			}},
		}},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario status = %q, want pass: %+v", result.Scenarios[0].Status, result.Scenarios[0].Failure)
	}

	for _, name := range fp.callNames() {
		if name == "inner_guarded" {
			t.Fatal("provider ran a keyword step gated off by when")
		}
	}
}
