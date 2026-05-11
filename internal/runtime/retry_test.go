package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

type retryProvider struct {
	mu        sync.Mutex
	calls     map[string]int
	passAfter map[string]int
}

func (p *retryProvider) Type() string {
	return "http"
}

func (p *retryProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	p.mu.Lock()
	p.calls[input.Step.Name]++
	call := p.calls[input.Step.Name]
	passAfter := p.passAfter[input.Step.Name]
	p.mu.Unlock()

	status := 500
	id := "failed"
	if passAfter == 0 || call >= passAfter {
		status = 200
		id = "ok"
	}

	return &provider.Output{
		StatusCode: status,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(int64(status)),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal(fmt.Sprintf(`{"id":"%s"}`, id)),
			"json": cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal(id),
			}),
		},
	}, nil
}

func (p *retryProvider) callCount(step string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls[step]
}

func TestRetryStepSucceedsOnFirstAttempt(t *testing.T) {
	t.Parallel()

	providerImpl := &retryProvider{calls: map[string]int{}, passAfter: map[string]int{"main": 1}}
	result := runRetryScenario(t, providerImpl, retryStep("main", 3, 0))
	step := result.Scenarios[0].Steps[0]

	if step.Status != report.StatusPass {
		t.Fatalf("step should pass, got %s", step.Status)
	}

	if step.Attempts != 1 {
		t.Fatalf("attempts=%d", step.Attempts)
	}

	if providerImpl.callCount("main") != 1 {
		t.Fatalf("provider calls=%d", providerImpl.callCount("main"))
	}
}

func TestRetryStepSucceedsAfterAttempts(t *testing.T) {
	t.Parallel()

	providerImpl := &retryProvider{calls: map[string]int{}, passAfter: map[string]int{"main": 3}}
	result := runRetryScenario(t, providerImpl, retryStep("main", 5, 0))
	step := result.Scenarios[0].Steps[0]

	if step.Status != report.StatusPass {
		t.Fatalf("step should pass, got %s", step.Status)
	}

	if step.Attempts != 3 {
		t.Fatalf("attempts=%d", step.Attempts)
	}

	if providerImpl.callCount("main") != 3 {
		t.Fatalf("provider calls=%d", providerImpl.callCount("main"))
	}
}

func TestRetryStepFailsAfterAllAttemptsWithLastError(t *testing.T) {
	t.Parallel()

	providerImpl := &retryProvider{calls: map[string]int{}, passAfter: map[string]int{"main": 10}}
	result := runRetryScenario(t, providerImpl, retryStep("main", 3, 0))
	step := result.Scenarios[0].Steps[0]

	if step.Status != report.StatusFail {
		t.Fatalf("step should fail, got %s", step.Status)
	}

	if step.Attempts != 3 {
		t.Fatalf("attempts=%d", step.Attempts)
	}

	if step.Failure == nil || step.Failure.Got != int64(500) {
		t.Fatalf("expected last status failure, got %#v", step.Failure)
	}
}

func TestRetryCaptureUsesEventualSuccess(t *testing.T) {
	t.Parallel()

	providerImpl := &retryProvider{calls: map[string]int{}, passAfter: map[string]int{"main": 2}}
	result := runRetryScenario(t, providerImpl, retryStep("main", 3, 0))
	step := result.Scenarios[0].Steps[0]

	if step.Status != report.StatusPass {
		t.Fatalf("step should pass, got %s", step.Status)
	}

	if responseJSON, ok := step.Response["json"].(map[string]interface{}); !ok || responseJSON["id"] != "ok" {
		t.Fatalf("expected captured success response, got %#v", step.Response)
	}
}

func TestRetryDoesNotRunTeardownMultipleTimes(t *testing.T) {
	t.Parallel()

	providerImpl := &retryProvider{calls: map[string]int{}, passAfter: map[string]int{"main": 10, "cleanup": 1}}
	runner := NewRunner(provider.NewRegistry(providerImpl))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:     "retry teardown",
		File:     "test.tales",
		Steps:    []*model.Step{retryStep("main", 3, 0)},
		Teardown: []*model.Step{newRetryHTTPStatusStep("cleanup", 200)},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Teardown[0].Status != report.StatusPass {
		t.Fatalf("teardown should pass")
	}

	if providerImpl.callCount("cleanup") != 1 {
		t.Fatalf("cleanup calls=%d", providerImpl.callCount("cleanup"))
	}
}

func runRetryScenario(t *testing.T, providerImpl *retryProvider, step *model.Step) *report.SuiteResult {
	t.Helper()

	runner := NewRunner(provider.NewRegistry(providerImpl))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "retry",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return result
}

func retryStep(name string, attempts int, interval time.Duration) *model.Step {
	step := newRetryHTTPStatusStep(name, 200)
	step.Retry = &model.Retry{Attempts: attempts, Interval: interval}
	step.Capture = map[string]model.Expression{
		"id": expr(`response.json.id`),
	}

	return step
}

func newRetryHTTPStatusStep(name string, status int) *model.Step {
	return &model.Step{
		Provider: "http",
		Name:     name,
		Request: &model.Request{
			Method: expr(`"GET"`),
			URL:    expr(`"http://example.test"`),
		},
		Expect:  &model.Expect{Status: expr(fmt.Sprintf("%d", status))},
		Capture: map[string]model.Expression{},
	}
}
