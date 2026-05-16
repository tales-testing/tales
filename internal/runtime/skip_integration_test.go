package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
)

func TestScenarioSkippedDoesNotRunSteps(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "skipped",
		File: "x.tales",
		SkipRules: []model.SkipRule{{
			Kind:      model.SkipIf,
			Condition: expr(`true`),
			Reason:    expr(`"intentionally skipped"`),
		}},
		Steps:    []*model.Step{newHTTPStep("main")},
		Teardown: []*model.Step{newHTTPStep("cleanup")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusSkip {
		t.Fatalf("scenario status = %s want skipped", scenarioResult.Status)
	}

	if scenarioResult.SkipReason != "intentionally skipped" {
		t.Fatalf("scenario skip reason = %q want %q", scenarioResult.SkipReason, "intentionally skipped")
	}

	if len(scenarioResult.Steps) != 0 {
		t.Fatalf("expected no step results, got %d", len(scenarioResult.Steps))
	}

	if len(scenarioResult.Teardown) != 0 {
		t.Fatalf("expected no teardown results, got %d", len(scenarioResult.Teardown))
	}

	if len(fp.calls) != 0 {
		t.Fatalf("provider was called %d times for a skipped scenario", len(fp.calls))
	}

	if result.Failed() {
		t.Fatalf("skipped scenario must not mark suite as failed")
	}
}

func TestScenarioNotSkippedWhenRuleDoesNotTrigger(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "runs",
		File: "x.tales",
		SkipRules: []model.SkipRule{{
			Kind:      model.SkipIf,
			Condition: expr(`false`),
		}},
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusPass {
		t.Fatalf("scenario status = %s want pass", scenarioResult.Status)
	}

	if len(fp.calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(fp.calls))
	}
}

func TestStepSkippedDoesNotInvokeProvider(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	skippedStep := newHTTPStep("skipped_step")
	skippedStep.SkipRules = []model.SkipRule{{
		Kind:      model.SkipIf,
		Condition: expr(`true`),
		Reason:    expr(`"step skip"`),
	}}

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "mixed",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main"), skippedStep},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusPass {
		t.Fatalf("scenario status = %s want pass", scenarioResult.Status)
	}

	var skippedResult *report.StepResult

	for _, sr := range scenarioResult.Steps {
		if sr.Name == "skipped_step" {
			skippedResult = sr
		}
	}

	if skippedResult == nil {
		t.Fatalf("expected step result for skipped_step")
	}

	if skippedResult.Status != report.StatusSkip {
		t.Fatalf("skipped_step status = %s want skipped", skippedResult.Status)
	}

	if skippedResult.SkipReason != "step skip" {
		t.Fatalf("skipped_step reason = %q want %q", skippedResult.SkipReason, "step skip")
	}

	if len(fp.calls) != 1 {
		t.Fatalf("provider should have been called only for non-skipped step, got %d calls: %v", len(fp.calls), fp.calls)
	}

	if fp.calls[0] != "main" {
		t.Fatalf("expected provider call for main, got %q", fp.calls[0])
	}
}

func TestStepCascadeSkipsDownstream(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))

	upstream := newHTTPStep("upstream")
	upstream.SkipRules = []model.SkipRule{{
		Kind:      model.SkipIf,
		Condition: expr(`true`),
		Reason:    expr(`"upstream skipped"`),
	}}

	downstream := newHTTPStep("downstream")
	downstream.DependsOn = []string{"upstream"}
	downstream.Request.URL = expr(`"http://example.test/${result.upstream.id}"`)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "cascade",
		File:  "x.tales",
		Steps: []*model.Step{upstream, downstream},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusPass {
		t.Fatalf("scenario status = %s want pass (skip cascade should not fail the scenario)", scenarioResult.Status)
	}

	var downstreamResult *report.StepResult

	for _, sr := range scenarioResult.Steps {
		if sr.Name == "downstream" {
			downstreamResult = sr
		}
	}

	if downstreamResult == nil {
		t.Fatalf("expected step result for downstream")
	}

	if downstreamResult.Status != report.StatusSkip {
		t.Fatalf("downstream status = %s want skipped", downstreamResult.Status)
	}

	if downstreamResult.SkipReason == "" {
		t.Fatalf("downstream skip reason must not be empty")
	}

	if !strings.Contains(downstreamResult.SkipReason, "upstream") {
		t.Fatalf("downstream skip reason should mention upstream, got %q", downstreamResult.SkipReason)
	}

	if len(fp.calls) != 0 {
		t.Fatalf("provider should not be invoked for either step, got %d calls: %v", len(fp.calls), fp.calls)
	}
}

func TestStepCascadeFailedReason(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp))

	downstream := newHTTPStep("dependent")
	downstream.DependsOn = []string{"main"}

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "cascade_failure",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main"), downstream},
	}}}

	result, _ := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})

	scenarioResult := result.Scenarios[0]

	var dependent *report.StepResult

	for _, sr := range scenarioResult.Steps {
		if sr.Name == "dependent" {
			dependent = sr
		}
	}

	if dependent == nil {
		t.Fatalf("expected step result for dependent")
	}

	if dependent.Status != report.StatusSkip {
		t.Fatalf("dependent status = %s want skipped", dependent.Status)
	}

	if !strings.Contains(dependent.SkipReason, "failed") || !strings.Contains(dependent.SkipReason, "main") {
		t.Fatalf("dependent skip reason should mention failed step main, got %q", dependent.SkipReason)
	}
}
