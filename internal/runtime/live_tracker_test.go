package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
)

// TestRunnerSurfacesStalledScenariosOnDeadline proves that when --timeout
// fires while a provider is blocked, the resulting SuiteResult carries the
// names of scenarios that were still in flight. This is what the CLI uses
// to print "scenarios still running when timeout hit: [...]" — without it,
// the user is left guessing which provider caused the hang.
func TestRunnerSurfacesStalledScenariosOnDeadline(t *testing.T) {
	t.Parallel()

	const budget = 100 * time.Millisecond

	prov := &hangProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "stuck on first step",
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

	result, err := runner.Run(ctx, suite, Options{Seed: 1, Parallel: 1})
	if err != nil && result == nil {
		t.Fatalf("runner.Run returned no result: %v", err)
	}

	if len(result.StalledScenarios) != 1 || result.StalledScenarios[0] != "stuck on first step" {
		t.Fatalf("stalled list should contain the blocked scenario, got %v", result.StalledScenarios)
	}
}

// TestRunnerNoStalledScenariosOnCleanRun guards against a false-positive
// regression: a suite that finishes before any deadline must report an
// empty stalled list, not the scenario names that just happened to be
// running last.
func TestRunnerNoStalledScenariosOnCleanRun(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&fakeProvider{}))
	suite := &model.Suite{Scenarios: []*model.Scenario{
		{Name: "one", File: "a.tales", Steps: []*model.Step{newHTTPStep("first")}},
		{Name: "two", File: "b.tales", Steps: []*model.Step{newHTTPStep("first")}},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 2})
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	if len(result.StalledScenarios) != 0 {
		t.Fatalf("clean run must produce an empty stalled list, got %v", result.StalledScenarios)
	}
}

// TestRunnerEmitsHeartbeatWhileScenarioBlocked proves that the --verbose
// path produces at least one Heartbeat event while a scenario is blocked
// inside a provider, so the user is told "X is still running" instead of
// staring at a blank terminal. The 50ms interval is just enough to fire
// before the test deadline-cancels the hanging scenario.
func TestRunnerEmitsHeartbeatWhileScenarioBlocked(t *testing.T) {
	t.Parallel()

	const (
		heartbeat = 50 * time.Millisecond
		budget    = 250 * time.Millisecond
	)

	prov := &hangProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "slow scenario",
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

	sink := &recordingSink{}

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	_, _ = runner.Run(ctx, suite, Options{
		Seed:              1,
		Parallel:          1,
		Events:            sink,
		HeartbeatInterval: heartbeat,
	})

	heartbeats := 0

	for _, ev := range sink.snapshot() {
		if ev == "heartbeat" {
			heartbeats++
		}
	}

	if heartbeats == 0 {
		t.Fatal("expected at least one heartbeat event while scenario was blocked")
	}
}

// TestRunnerNoHeartbeatWhenIntervalIsZero guards the opt-in: a suite running
// without --verbose must never trigger Heartbeat, even when scenarios take
// time. This is what keeps existing CI logs quiet.
func TestRunnerNoHeartbeatWhenIntervalIsZero(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&fakeProvider{}))
	suite := &model.Suite{Scenarios: []*model.Scenario{
		{Name: "fast", File: "a.tales", Steps: []*model.Step{newHTTPStep("first")}},
	}}

	sink := &recordingSink{}

	_, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1, Events: sink})
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	for _, ev := range sink.snapshot() {
		if ev == "heartbeat" {
			t.Fatal("heartbeat must not fire when HeartbeatInterval is 0")
		}
	}
}

// TestRunnerNoStalledScenariosOnUserCancel proves that context.Canceled
// (e.g. Ctrl-C) does not produce a stalled list — that diagnostic is
// reserved for the --timeout / DeadlineExceeded path, where the user
// explicitly asked for a wall-clock budget.
func TestRunnerNoStalledScenariosOnUserCancel(t *testing.T) {
	t.Parallel()

	prov := &hangProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

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

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, _ := runner.Run(ctx, suite, Options{Seed: 1, Parallel: 1})

	if result == nil {
		t.Fatal("runner.Run must return a result even when ctx is cancelled")
	}

	if len(result.StalledScenarios) != 0 {
		t.Fatalf("user cancel (context.Canceled) must NOT produce a stalled list, got %v", result.StalledScenarios)
	}
}
