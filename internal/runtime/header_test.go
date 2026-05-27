package runtime

import (
	"context"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

type headerProvider struct{}

func (p *headerProvider) Type() string {
	return "http"
}

func (p *headerProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	return &provider.Output{
		StatusCode: 200,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status": cty.NumberIntVal(200),
			"headers": cty.ObjectVal(map[string]cty.Value{
				"Content-Type": cty.StringVal("application/json"),
				"content-type": cty.StringVal("application/json"),
				"X-Request-ID": cty.StringVal("req-123"),
			}),
			"body": cty.StringVal("{}"),
			"json": cty.EmptyObjectVal,
		},
	}, nil
}

func TestCaptureResponseHeadersByCanonicalAndLowercaseKey(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&headerProvider{}))
	step := newRetryHTTPStatusStep("main", 200)
	step.Capture = map[string]model.Expression{
		"request_id":   expr(`response.headers["X-Request-ID"]`),
		"content_type": expr(`response.headers["content-type"]`),
	}

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "headers",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario should pass: %#v", result.Scenarios[0].Failure)
	}
}

func TestMissingResponseHeaderAccessFailsCleanly(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&headerProvider{}))
	step := newRetryHTTPStatusStep("main", 200)
	step.Capture = map[string]model.Expression{
		"missing": expr(`response.headers["X-Missing"]`),
	}

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "headers",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failure := result.Scenarios[0].Failure
	if failure == nil || failure.Kind != "capture" || failure.Path != "missing" {
		t.Fatalf("expected clean capture failure, got %#v", failure)
	}
}

func TestCanMissingHeaderReturnsFalseInTeardownWhen(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&headerProvider{}))
	cleanup := newRetryHTTPStatusStep("cleanup", 200)
	cleanup.When = expr(`can(result.main.response.headers["X-Missing"])`)

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:     "headers",
		File:     "test.tales",
		Steps:    []*model.Step{newRetryHTTPStatusStep("main", 200)},
		Teardown: []*model.Step{cleanup},
	}}}, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Teardown[0].Status != report.StatusSkip {
		t.Fatalf("teardown should be skipped, got %s", result.Scenarios[0].Teardown[0].Status)
	}
}
