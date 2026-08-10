package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// ctxRecordingProvider fails whenever the context it is handed is already
// done, which is exactly what a teardown step running on an exhausted
// --timeout budget used to see.
type ctxRecordingProvider struct {
	mu   sync.Mutex
	errs map[string]error
}

func newCtxRecordingProvider() *ctxRecordingProvider {
	return &ctxRecordingProvider{errs: map[string]error{}}
}

func (p *ctxRecordingProvider) Type() string { return "http" }

func (p *ctxRecordingProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	p.mu.Lock()
	p.errs[input.Step.Name] = ctx.Err()
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &provider.Output{
		StatusCode: 200,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal("{}"),
			"json":    cty.ObjectVal(map[string]cty.Value{"id": cty.StringVal("42")}),
		},
	}, nil
}

func (p *ctxRecordingProvider) contextErr(step string) (error, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	err, seen := p.errs[step]

	return err, seen
}

func teardownContextSuite() *model.Suite {
	return &model.Suite{Scenarios: []*model.Scenario{{
		Name:     "s",
		File:     "x.tales",
		Steps:    []*model.Step{newHTTPStep("main")},
		Teardown: []*model.Step{newHTTPStep("cleanup")},
	}}}
}

// An exhausted run budget must stop the run without also killing the cleanup
// it makes necessary.
func TestTeardownSurvivesCancelledRunContext(t *testing.T) {
	t.Parallel()

	fp := newCtxRecordingProvider()
	runner := NewRunner(provider.NewRegistry(fp))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := runner.Run(ctx, teardownContextSuite(), Options{Seed: 1, Parallel: 1, TeardownGrace: 5 * time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	ctxErr, seen := fp.contextErr("cleanup")
	if !seen {
		t.Fatal("teardown step never reached the provider")
	}

	if ctxErr != nil {
		t.Fatalf("teardown context error = %v, want nil (detached from the run context)", ctxErr)
	}

	teardown := result.Scenarios[0].Teardown
	if len(teardown) != 1 || teardown[0].Status != report.StatusPass {
		t.Fatalf("teardown should pass on the detached context, got %+v", teardown)
	}
}

// TeardownGrace = 0 keeps the historical behavior, so users who relied on a
// cancellation cutting cleanup short have an explicit way back.
func TestTeardownInheritsRunContextWithoutGrace(t *testing.T) {
	t.Parallel()

	fp := newCtxRecordingProvider()
	runner := NewRunner(provider.NewRegistry(fp))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := runner.Run(ctx, teardownContextSuite(), Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	ctxErr, seen := fp.contextErr("cleanup")
	if !seen {
		t.Fatal("teardown step never reached the provider")
	}

	if ctxErr == nil {
		t.Fatal("teardown context should stay cancelled when TeardownGrace is 0")
	}

	teardown := result.Scenarios[0].Teardown
	if len(teardown) != 1 || teardown[0].Status != report.StatusFail {
		t.Fatalf("teardown should fail on the cancelled context, got %+v", teardown)
	}
}

// The grace budget must not start ticking while the main steps run, otherwise
// a long scenario would arrive at its teardown with an already-expired one.
func TestCleanupContextIsBuiltLazily(t *testing.T) {
	t.Parallel()

	get, release := newLazyCleanupContext(context.Background(), 50*time.Millisecond)
	defer release()

	time.Sleep(80 * time.Millisecond)

	if err := get().Err(); err != nil {
		t.Fatalf("cleanup context expired before first use: %v", err)
	}
}
