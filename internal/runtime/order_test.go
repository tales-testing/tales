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

// recordingProvider records the order in which steps are executed and the
// peak number of Execute calls in flight, so tests can assert file-order
// execution and the absence of intra-scenario parallelism.
type recordingProvider struct {
	mu        sync.Mutex
	calls     []string
	active    int
	maxActive int
	failFor   map[string]bool
}

func (p *recordingProvider) Type() string { return "http" }

func (p *recordingProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	p.mu.Lock()
	p.calls = append(p.calls, input.Step.Name)
	p.active++

	if p.active > p.maxActive {
		p.maxActive = p.active
	}

	fail := p.failFor[input.Step.Name]
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.active--
		p.mu.Unlock()
	}()

	if fail {
		return nil, fmt.Errorf("forced failure for %q", input.Step.Name)
	}

	return okProviderOutput(input.Request), nil
}

func (p *recordingProvider) recorded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.calls...)
}

func (p *recordingProvider) peakConcurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.maxActive
}

// barrierProvider blocks every Execute call until `want` of them are in
// flight at once, so a test can prove that two scenarios run concurrently.
type barrierProvider struct {
	mu       sync.Mutex
	want     int
	count    int
	gate     chan struct{}
	timedOut bool
}

func (p *barrierProvider) Type() string { return "http" }

func (p *barrierProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	p.mu.Lock()
	p.count++

	if p.count == p.want {
		close(p.gate)
	}
	p.mu.Unlock()

	select {
	case <-p.gate:
	case <-time.After(3 * time.Second):
		p.mu.Lock()
		p.timedOut = true
		p.mu.Unlock()
	}

	return okProviderOutput(input.Request), nil
}

func (p *barrierProvider) barrierTimedOut() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.timedOut
}

func okProviderOutput(request map[string]cty.Value) *provider.Output {
	return &provider.Output{
		StatusCode: 200,
		Request:    request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal("{}"),
			"json":    cty.ObjectVal(map[string]cty.Value{"id": cty.StringVal("1")}),
		},
	}
}

func assertCallOrder(t *testing.T, want, got []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("call order: want %v, got %v", want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order: want %v, got %v", want, got)
		}
	}
}

func findStepResult(steps []*report.StepResult, name string) *report.StepResult {
	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}

	return nil
}

// TestScenarioStepsRunInFileOrder verifies that independent steps execute in
// the order they are declared, with no depends_on needed, even when scenario
// parallelism is high.
func TestScenarioStepsRunInFileOrder(t *testing.T) {
	t.Parallel()

	rp := &recordingProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(rp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "ordered",
		File:  "order.tales",
		Steps: []*model.Step{newHTTPStep("first"), newHTTPStep("second"), newHTTPStep("third")},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 4}); err != nil {
		t.Fatalf("run: %v", err)
	}

	assertCallOrder(t, []string{"first", "second", "third"}, rp.recorded())
}

// TestScenarioNoIntraScenarioParallelism verifies that steps inside a
// scenario never run concurrently.
func TestScenarioNoIntraScenarioParallelism(t *testing.T) {
	t.Parallel()

	rp := &recordingProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(rp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "ordered",
		File:  "order.tales",
		Steps: []*model.Step{newHTTPStep("a"), newHTTPStep("b"), newHTTPStep("c")},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 4}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if peak := rp.peakConcurrency(); peak != 1 {
		t.Fatalf("steps inside a scenario must run one at a time, peak concurrency = %d", peak)
	}
}

// TestScenarioParallelismPreserved verifies that scenarios still run
// concurrently when --parallel > 1, while each scenario stays ordered.
func TestScenarioParallelismPreserved(t *testing.T) {
	t.Parallel()

	bp := &barrierProvider{want: 2, gate: make(chan struct{})}
	runner := NewRunner(provider.NewRegistry(bp))
	suite := &model.Suite{Scenarios: []*model.Scenario{
		{Name: "scenario_a", File: "a.tales", Steps: []*model.Step{newHTTPStep("a_step")}},
		{Name: "scenario_b", File: "b.tales", Steps: []*model.Step{newHTTPStep("b_step")}},
	}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 2}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if bp.barrierTimedOut() {
		t.Fatal("scenarios did not run concurrently: the 2-way barrier timed out")
	}
}

// TestScenarioStopsOnFirstFailure verifies that a failing step halts the
// scenario: later steps are not executed and are reported as skipped.
func TestScenarioStopsOnFirstFailure(t *testing.T) {
	t.Parallel()

	rp := &recordingProvider{failFor: map[string]bool{"second": true}}
	runner := NewRunner(provider.NewRegistry(rp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "stops",
		File:  "stop.tales",
		Steps: []*model.Step{newHTTPStep("first"), newHTTPStep("second"), newHTTPStep("third")},
	}}}

	result, _ := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})

	assertCallOrder(t, []string{"first", "second"}, rp.recorded())

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusFail {
		t.Fatalf("scenario status = %s, want fail", scenarioResult.Status)
	}

	third := findStepResult(scenarioResult.Steps, "third")
	if third == nil {
		t.Fatal(`"third" should still be reported`)
	}

	if third.Status != report.StatusSkip {
		t.Fatalf(`"third" status = %s, want skipped`, third.Status)
	}
}

// TestScenarioStepResultsInFileOrder verifies that step results are reported
// in file order, not alphabetically (the legacy DAG runner sorted by name).
func TestScenarioStepResultsInFileOrder(t *testing.T) {
	t.Parallel()

	rp := &recordingProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(rp))
	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "ordered",
		File:  "order.tales",
		Steps: []*model.Step{newHTTPStep("charlie"), newHTTPStep("alpha"), newHTTPStep("bravo")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	steps := result.Scenarios[0].Steps
	want := []string{"charlie", "alpha", "bravo"}

	if len(steps) != len(want) {
		t.Fatalf("want %d step results, got %d", len(want), len(steps))
	}

	for i, name := range want {
		if steps[i].Name != name {
			t.Fatalf("step result[%d] = %q, want %q", i, steps[i].Name, name)
		}
	}
}

// TestKeywordStepsRunInFileOrder verifies that keyword sub-steps execute in
// the order they are declared inside the keyword block.
func TestKeywordStepsRunInFileOrder(t *testing.T) {
	t.Parallel()

	rp := &recordingProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(rp))
	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"flow": {
				Name: "flow",
				Steps: []*model.Step{
					newHTTPStep("kw_charlie"),
					newHTTPStep("kw_alpha"),
					newHTTPStep("kw_bravo"),
				},
			},
		},
		Scenarios: []*model.Scenario{{
			Name: "calls keyword",
			File: "kw.tales",
			Steps: []*model.Step{{
				Provider: "keyword",
				Name:     "run_flow",
				Keyword: &model.KeywordCall{
					Name:   expr(`"flow"`),
					Inputs: expr(`{}`),
				},
			}},
		}},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario failed: %+v", result.Scenarios[0].Failure)
	}

	assertCallOrder(t, []string{"kw_charlie", "kw_alpha", "kw_bravo"}, rp.recorded())
}
