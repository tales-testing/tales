package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
)

// recordingSink captures every event emitted by the runner so we can assert
// against the order and contents. The sink is exercised from multiple
// scenario goroutines so it must lock around mutations.
type recordingSink struct {
	mu     sync.Mutex
	events []string
}

func (s *recordingSink) SuiteStarted(total, parallel int, seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, "suite-started")
}

func (s *recordingSink) ScenarioStarted(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, "scenario-started:"+name)
}

func (s *recordingSink) ScenarioEnded(name string, status report.Status, duration time.Duration, _ string) {
	_ = duration

	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, "scenario-ended:"+name+":"+string(status))
}

func (s *recordingSink) Heartbeat(active []ActiveScenario) {
	_ = active

	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, "heartbeat")
}

func (s *recordingSink) SuiteEnded(duration time.Duration) {
	_ = duration

	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, "suite-ended")
}

func (s *recordingSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, len(s.events))
	copy(out, s.events)

	return out
}

// TestEventSinkOrder proves that with the sink installed the runner emits
// suite-started first, suite-ended last, and every scenario gets a started
// + ended pair. The "before the first scenario PASS" bug we are trying to
// fix is exactly the absence of these events — if a hang prevented runner
// completion, suite-started would still have fired, making the hang visible.
func TestEventSinkOrder(t *testing.T) {
	t.Parallel()

	prov := &fakeProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

	suite := &model.Suite{Scenarios: []*model.Scenario{
		{Name: "alpha", File: "a.tales", Steps: []*model.Step{newHTTPStep("first")}},
		{Name: "beta", File: "b.tales", Steps: []*model.Step{newHTTPStep("first")}},
	}}

	sink := &recordingSink{}

	_, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1, Events: sink})
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	events := sink.snapshot()

	if len(events) == 0 || events[0] != "suite-started" {
		t.Fatalf("first event must be suite-started, got %v", events)
	}

	if events[len(events)-1] != "suite-ended" {
		t.Fatalf("last event must be suite-ended, got %v", events)
	}

	for _, scenarioName := range []string{"alpha", "beta"} {
		startSeen := false
		endSeen := false

		for _, ev := range events {
			if ev == "scenario-started:"+scenarioName {
				startSeen = true
			}

			if strings.HasPrefix(ev, "scenario-ended:"+scenarioName+":") {
				endSeen = true
			}
		}

		if !startSeen || !endSeen {
			t.Fatalf("scenario %q is missing a started/ended pair: %v", scenarioName, events)
		}
	}
}

// TestEventSinkNilSafe ensures the historical zero-config Options{} path is
// preserved: every existing caller passing no sink must still work.
func TestEventSinkNilSafe(t *testing.T) {
	t.Parallel()

	runner := NewRunner(provider.NewRegistry(&fakeProvider{}))
	suite := &model.Suite{Scenarios: []*model.Scenario{
		{Name: "one", File: "a.tales", Steps: []*model.Step{newHTTPStep("first")}},
	}}

	_, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("nil sink must not break runner.Run: %v", err)
	}
}
