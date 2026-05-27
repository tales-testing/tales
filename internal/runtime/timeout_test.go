package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// hangProvider blocks on the incoming context. It models a stalled HTTP
// upstream / mobile driver / SQL query and lets the test prove that
// canceling the runner's context unblocks the run.
type hangProvider struct {
	executions atomic.Int64
}

func (p *hangProvider) Type() string {
	return "http"
}

func (p *hangProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = input
	p.executions.Add(1)

	<-ctx.Done()

	return &provider.Output{
		StatusCode: 0,
		Request:    map[string]cty.Value{},
		Response:   map[string]cty.Value{},
	}, ctx.Err()
}

// TestRunnerHonorsContextDeadline proves that wrapping the runner's input
// context with a deadline (the path exercised by --timeout in the CLI) lets
// a hanging step be canceled instead of blocking the whole process. The
// budget is sized large enough to dwarf scheduling jitter while staying small
// enough that the test cannot mask a regression: a broken implementation
// would block until the test-wide go-test timeout fires.
func TestRunnerHonorsContextDeadline(t *testing.T) {
	t.Parallel()

	const budget = 100 * time.Millisecond

	providerImpl := &hangProvider{}
	runner := NewRunner(provider.NewRegistry(providerImpl))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "hangs forever",
		File: "test.tales",
		Steps: []*model.Step{{
			Provider: "http",
			Name:     "stall",
			Request: &model.Request{
				Method: expr(`"GET"`),
				URL:    expr(`"http://example.test"`),
			},
			Expect: &model.Expect{Status: expr(`200`)},
		}},
	}}}

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	start := time.Now()
	result, err := runner.Run(ctx, suite, Options{Seed: 1, Parallel: 1})
	elapsed := time.Since(start)

	if err != nil && result == nil {
		t.Fatalf("runner.Run returned no result: %v", err)
	}

	if elapsed > budget+500*time.Millisecond {
		t.Fatalf("runner.Run did not honor the context deadline: elapsed=%s budget=%s", elapsed, budget)
	}

	if !result.Failed() {
		t.Fatal("a suite canceled by deadline must be reported as failed")
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != report.StatusFail {
		t.Fatalf("stalled step should be marked failed, got %s", step.Status)
	}
}
