package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

type fakeProvider struct {
	mu      sync.Mutex
	calls   []string
	closes  int
	failFor map[string]bool
}

func (p *fakeProvider) Type() string { return "http" }

func (p *fakeProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closes++

	return nil
}

func (p *fakeProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx
	p.mu.Lock()
	p.calls = append(p.calls, input.Step.Name)
	p.mu.Unlock()
	if p.failFor[input.Step.Name] {
		return nil, fmt.Errorf("forced failure")
	}
	return &provider.Output{
		Duration:   0,
		StatusCode: 200,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal("{}"),
			"json": cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal("42"),
			}),
		},
	}, nil
}

func (p *fakeProvider) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.closes
}

func expr(src string) model.Expression {
	e, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		panic(diags.Error())
	}
	return model.Expression{Expr: e, File: "test.hcl", Line: 1}
}

func newHTTPStep(name string) *model.Step {
	return &model.Step{
		Provider: nameProvider(),
		Name:     name,
		Request: &model.Request{
			Method: expr(`"GET"`),
			URL:    expr(`"http://example.test"`),
		},
		Expect: &model.Expect{Status: expr(`200`)},
		Capture: map[string]model.Expression{
			"id": expr(`response.json.id`),
		},
	}
}

func nameProvider() string {
	return "http"
}

func TestTeardownRunsAfterSuccess(t *testing.T) {
	t.Parallel()
	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "s",
		File: "x.tales",
		Steps: []*model.Step{
			newHTTPStep("main"),
		},
		Teardown: []*model.Step{newHTTPStep("cleanup")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scenarios[0].Teardown) != 1 || result.Scenarios[0].Teardown[0].Status != "pass" {
		t.Fatalf("teardown should run and pass")
	}
}

func TestRunnerClosesProvidersAfterSuccess(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := fp.closeCount(); got != 1 {
		t.Fatalf("expected provider close after success, got %d", got)
	}
}

func TestRunnerClosesProvidersAfterFailure(t *testing.T) {
	t.Parallel()

	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "s",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run should return a suite result, got fatal error: %v", err)
	}
	if !result.Failed() {
		t.Fatal("expected scenario failure")
	}

	if got := fp.closeCount(); got != 1 {
		t.Fatalf("expected provider close after failure, got %d", got)
	}
}

func TestTeardownRunsAfterFailure(t *testing.T) {
	t.Parallel()
	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "s",
		File: "x.tales",
		Steps: []*model.Step{
			newHTTPStep("main"),
		},
		Teardown: []*model.Step{newHTTPStep("cleanup")},
	}}}

	result, _ := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if len(result.Scenarios[0].Teardown) != 1 {
		t.Fatalf("teardown should run")
	}
	if result.Scenarios[0].Teardown[0].Name != "cleanup" {
		t.Fatalf("unexpected teardown result")
	}
}

func TestTeardownWhenCanSkipsInvalidCleanup(t *testing.T) {
	t.Parallel()
	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp))
	cleanup := newHTTPStep("cleanup")
	cleanup.When = expr(`can(result.main.id)`)
	cleanup.Request.URL = expr(`"http://example.test/"`)
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "s",
		File: "x.tales",
		Steps: []*model.Step{
			newHTTPStep("main"),
		},
		Teardown: []*model.Step{cleanup},
	}}}

	result, _ := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if len(result.Scenarios[0].Teardown) != 1 {
		t.Fatalf("expected teardown status")
	}
	if result.Scenarios[0].Teardown[0].Status != "skipped" {
		t.Fatalf("teardown should be skipped when can() is false")
	}
}

func TestTeardownCanUseCapturedValue(t *testing.T) {
	t.Parallel()
	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp))
	cleanup := newHTTPStep("cleanup")
	cleanup.When = expr(`can(result.main.id)`)
	cleanup.Request.URL = expr(`"http://example.test/${result.main.id}"`)
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:     "s",
		File:     "x.tales",
		Steps:    []*model.Step{newHTTPStep("main")},
		Teardown: []*model.Step{cleanup},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scenarios[0].Teardown[0].Status != "pass" {
		t.Fatalf("teardown should pass with captured data")
	}
}
