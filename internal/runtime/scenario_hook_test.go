package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// fakeHookProvider is a no-op provider that records ScenarioHook calls.
// It carries its own Type so it can coexist with the http fakeProvider in
// the same registry without colliding.
type fakeHookProvider struct {
	mu        sync.Mutex
	beginErr  error
	endErr    error
	artifacts []provider.ScenarioArtifact

	beginCalls []string
	endCalls   []hookEndCall
	endRunErrs []error
}

type hookEndCall struct {
	name   string
	runErr error
}

func (p *fakeHookProvider) Type() string { return "hookprobe" }

func (p *fakeHookProvider) Execute(_ context.Context, _ provider.Input) (*provider.Output, error) {
	return &provider.Output{
		Response: map[string]cty.Value{},
	}, nil
}

func (p *fakeHookProvider) BeginScenario(_ context.Context, sc *model.Scenario, _ provider.ScenarioContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.beginCalls = append(p.beginCalls, sc.Name)

	return p.beginErr
}

func (p *fakeHookProvider) EndScenario(_ context.Context, sc *model.Scenario, _ provider.ScenarioContext, runErr error) ([]provider.ScenarioArtifact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.endCalls = append(p.endCalls, hookEndCall{name: sc.Name, runErr: runErr})
	p.endRunErrs = append(p.endRunErrs, runErr)

	return p.artifacts, p.endErr
}

func (p *fakeHookProvider) beginCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.beginCalls)
}

func (p *fakeHookProvider) endCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.endCalls)
}

func TestScenarioHookInvokedOnPass(t *testing.T) {
	t.Parallel()

	hook := &fakeHookProvider{
		artifacts: []provider.ScenarioArtifact{{Type: "video", Path: "/tmp/preview.mp4"}},
	}
	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp, hook))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "pass",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hook.beginCount() != 1 || hook.endCount() != 1 {
		t.Fatalf("expected one begin/end, got begin=%d end=%d", hook.beginCount(), hook.endCount())
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusPass {
		t.Fatalf("scenario status = %s want pass", scenarioResult.Status)
	}

	if got := scenarioResult.Artifacts; len(got) != 1 || got[0].Path != "/tmp/preview.mp4" {
		t.Fatalf("expected artifact attached, got %+v", got)
	}
}

func TestScenarioHookSkippedScenariosBypassed(t *testing.T) {
	t.Parallel()

	hook := &fakeHookProvider{}
	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp, hook))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name: "skipped",
		File: "x.tales",
		SkipRules: []model.SkipRule{{
			Kind:      model.SkipIf,
			Condition: expr(`true`),
			Reason:    expr(`"intentionally skipped"`),
		}},
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hook.beginCount() != 0 {
		t.Fatalf("expected hook not to fire on skipped scenario, got %d begins", hook.beginCount())
	}

	if hook.endCount() != 0 {
		t.Fatalf("expected hook End not to fire on skipped scenario, got %d ends", hook.endCount())
	}
}

func TestScenarioHookEndRunsAfterStepFailure(t *testing.T) {
	t.Parallel()

	hook := &fakeHookProvider{}
	fp := &fakeProvider{failFor: map[string]bool{"main": true}}
	runner := NewRunner(provider.NewRegistry(fp, hook))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "fails",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Status != report.StatusFail {
		t.Fatalf("scenario status = %s want fail", result.Scenarios[0].Status)
	}

	if hook.beginCount() != 1 || hook.endCount() != 1 {
		t.Fatalf("expected hook to fire begin and end even on failure, got begin=%d end=%d", hook.beginCount(), hook.endCount())
	}
}

func TestScenarioHookBeginErrorFailsScenario(t *testing.T) {
	t.Parallel()

	hook := &fakeHookProvider{beginErr: errors.New("boom")}
	fp := &fakeProvider{failFor: map[string]bool{}}
	runner := NewRunner(provider.NewRegistry(fp, hook))

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "begin-fails",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Status != report.StatusFail {
		t.Fatalf("expected scenario to fail when BeginScenario errors, got %s", result.Scenarios[0].Status)
	}

	if hook.endCount() != 0 {
		t.Fatalf("expected EndScenario NOT to fire when its own Begin failed, got %d ends", hook.endCount())
	}

	// The step should NOT have executed when the scenario hook rejected the
	// scenario at the gate.
	if got := len(fp.calls); got != 0 {
		t.Fatalf("expected zero step executions when hook rejects, got %d", got)
	}
}

func TestScenarioHookEndOrderIsLIFO(t *testing.T) {
	t.Parallel()

	var (
		seq    atomic.Int32
		hookA  = &orderHook{name: "A", seq: &seq}
		hookB  = &orderHook{name: "B", seq: &seq}
		hookC  = &orderHook{name: "C", seq: &seq}
		runner = NewRunner(provider.NewRegistry(&fakeProvider{}, hookA, hookB, hookC))
	)

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "order",
		File:  "x.tales",
		Steps: []*model.Step{newHTTPStep("main")},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hookA.beginAt == 0 || hookB.beginAt == 0 || hookC.beginAt == 0 {
		t.Fatalf("expected every hook to be Begun, got A=%d B=%d C=%d", hookA.beginAt, hookB.beginAt, hookC.beginAt)
	}

	// Begin order is just registry.All() ordering (unstable across Go map
	// iteration), so we only assert each hook saw its End AFTER its Begin
	// and that across hooks the End order is the reverse of the Begin
	// order (LIFO).
	hooks := []*orderHook{hookA, hookB, hookC}
	for _, h := range hooks {
		if h.endAt <= h.beginAt {
			t.Fatalf("hook %s end (%d) must follow its begin (%d)", h.name, h.endAt, h.beginAt)
		}
	}

	// Compute the reverse order of Begin and assert End matches.
	for i := range hooks {
		for j := i + 1; j < len(hooks); j++ {
			if hooks[i].beginAt < hooks[j].beginAt {
				if hooks[i].endAt < hooks[j].endAt {
					t.Fatalf("hooks %s and %s violate LIFO: begin order %d/%d, end order %d/%d",
						hooks[i].name, hooks[j].name,
						hooks[i].beginAt, hooks[j].beginAt,
						hooks[i].endAt, hooks[j].endAt,
					)
				}
			}
		}
	}
}

type orderHook struct {
	name    string
	seq     *atomic.Int32
	beginAt int32
	endAt   int32
}

func (h *orderHook) Type() string { return "hookprobe-" + h.name }

func (h *orderHook) Execute(_ context.Context, _ provider.Input) (*provider.Output, error) {
	return &provider.Output{Response: map[string]cty.Value{}}, nil
}

func (h *orderHook) BeginScenario(_ context.Context, _ *model.Scenario, _ provider.ScenarioContext) error {
	h.beginAt = h.seq.Add(1)

	return nil
}

func (h *orderHook) EndScenario(_ context.Context, _ *model.Scenario, _ provider.ScenarioContext, _ error) ([]provider.ScenarioArtifact, error) {
	h.endAt = h.seq.Add(1)

	return nil, nil
}

// teardownOrderProvider records the order in which the runner executes
// scenario steps, EndScenario hooks, and teardown steps. It is used to
// pin the contract that the hook fires AFTER the main steps but BEFORE
// the teardown steps so a recorder's output is finalized before any
// teardown assertion can observe it.
type teardownOrderProvider struct {
	seq     atomic.Int32
	stepAt  int32
	endAt   int32
	tearAt  int32
	calls   atomic.Int32
	mu      sync.Mutex
	teardow string
	main    string
}

func (p *teardownOrderProvider) Type() string { return "http" }

func (p *teardownOrderProvider) Execute(_ context.Context, input provider.Input) (*provider.Output, error) {
	tick := p.seq.Add(1)

	p.mu.Lock()
	switch input.Step.Name {
	case p.main:
		p.stepAt = tick
	case p.teardow:
		p.tearAt = tick
	}
	p.mu.Unlock()

	p.calls.Add(1)

	return &provider.Output{Response: map[string]cty.Value{}}, nil
}

func (p *teardownOrderProvider) BeginScenario(_ context.Context, _ *model.Scenario, _ provider.ScenarioContext) error {
	return nil
}

func (p *teardownOrderProvider) EndScenario(_ context.Context, _ *model.Scenario, _ provider.ScenarioContext, _ error) ([]provider.ScenarioArtifact, error) {
	p.endAt = p.seq.Add(1)

	return nil, nil
}

func TestScenarioHookEndsBeforeTeardown(t *testing.T) {
	t.Parallel()

	hook := &teardownOrderProvider{main: "main", teardow: "cleanup"}
	runner := NewRunner(provider.NewRegistry(hook))

	mainStep := newHTTPStep("main")
	teardownStep := newHTTPStep("cleanup")

	suite := &model.Suite{Scenarios: []*model.Scenario{{
		Name:     "ordering",
		File:     "x.tales",
		Steps:    []*model.Step{mainStep},
		Teardown: []*model.Step{teardownStep},
	}}}

	if _, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hook.stepAt == 0 || hook.endAt == 0 || hook.tearAt == 0 {
		t.Fatalf("expected every phase to fire: step=%d end=%d teardown=%d", hook.stepAt, hook.endAt, hook.tearAt)
	}

	if !(hook.stepAt < hook.endAt && hook.endAt < hook.tearAt) {
		t.Fatalf("expected step (%d) < EndScenario (%d) < teardown (%d)", hook.stepAt, hook.endAt, hook.tearAt)
	}
}
