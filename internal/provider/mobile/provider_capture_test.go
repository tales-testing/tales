package mobile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

// recordingDriver counts every screenshot/hierarchy call so capture-mode
// tests can assert exact behavior. The hierarchy slot is reused: callers
// configure `hierarchy` once and it is returned for every Hierarchy() call.
type recordingDriver struct {
	hierarchy        *tree.ViewNode
	hierarchyErr     error
	hierarchyCalls   atomic.Int32
	screenshotPNG    []byte
	screenshotErr    error
	screenshotCalls  atomic.Int32
	tapErr           error
	inputTextErr     error
	tappedAtLeastOne atomic.Int32
}

func (d *recordingDriver) Health(_ context.Context) error { return nil }

func (d *recordingDriver) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	d.hierarchyCalls.Add(1)

	if d.hierarchyErr != nil {
		return nil, d.hierarchyErr
	}

	return d.hierarchy, nil
}

func (d *recordingDriver) Tap(_ context.Context, _ string, _, _ float64) error {
	d.tappedAtLeastOne.Add(1)

	return d.tapErr
}

func (d *recordingDriver) InputText(_ context.Context, _ string, _ string) error {
	return d.inputTextErr
}

func (d *recordingDriver) EraseText(_ context.Context, _ string, _ int) error {
	return nil
}

func (d *recordingDriver) Screenshot(_ context.Context) ([]byte, error) {
	d.screenshotCalls.Add(1)

	if d.screenshotErr != nil {
		return nil, d.screenshotErr
	}

	if d.screenshotPNG == nil {
		return []byte("PNG"), nil
	}

	return d.screenshotPNG, nil
}

func newCaptureProvider(t *testing.T, drv *recordingDriver, mode CaptureMode) *Provider {
	t.Helper()

	target := sampleProviderTarget()
	lc := &fakeLifecycle{udid: "UDID"}
	builder := SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
		return &Session{
			Target:    target,
			UDID:      lc.udid,
			Driver:    drv,
			Lifecycle: lc.toAppleLifecycle(),
		}, nil
	})

	return New(WithSessionBuilder(builder), WithArtifactsBase(t.TempDir()), WithCaptureMode(mode))
}

func captureActions() []provider.MobileActionExec {
	return []provider.MobileActionExec{
		{Kind: model.MobileActionTap, ID: "welcome.register"},
		{Kind: model.MobileActionInputText, ID: "welcome.register", Value: "hello"},
	}
}

func captureInput(actions []provider.MobileActionExec) provider.Input {
	return provider.Input{
		Scenario: "demo",
		Step:     newStep("step"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions:    actions,
		},
	}
}

func TestCaptureNone_SkipsEveryCaptureOnSuccess(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode()}
	p := newCaptureProvider(t, drv, CaptureNone)

	_, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := drv.screenshotCalls.Load(); got != 0 {
		t.Fatalf("none mode should not screenshot on success; got %d calls", got)
	}
}

func TestCaptureNone_SkipsEvenOnFailure(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode(), tapErr: errors.New("boom")}
	p := newCaptureProvider(t, drv, CaptureNone)

	_, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err == nil {
		t.Fatal("expected action failure to surface")
	}

	if got := drv.screenshotCalls.Load(); got != 0 {
		t.Fatalf("none mode is strict: failure path must not screenshot; got %d calls", got)
	}
}

func TestCaptureFailures_NoPerActionCaptureOnSuccess(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode()}
	p := newCaptureProvider(t, drv, CaptureFailures)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := drv.screenshotCalls.Load(); got != 0 {
		t.Fatalf("failures mode should not screenshot on success; got %d calls", got)
	}

	if len(out.ActionResults) != 2 {
		t.Fatalf("expected 2 action results, got %d", len(out.ActionResults))
	}

	for _, r := range out.ActionResults {
		if r.Screenshot != "" {
			t.Errorf("action %d should have no screenshot path in failures mode, got %q", r.Index, r.Screenshot)
		}

		if r.Status != actionStatusPass {
			t.Errorf("action %d expected status pass, got %q", r.Index, r.Status)
		}
	}
}

func TestCaptureFailures_PreservesStepLevelArtifactOnFailure(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode(), tapErr: errors.New("boom")}
	p := newCaptureProvider(t, drv, CaptureFailures)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err == nil {
		t.Fatal("expected action failure")
	}

	if got := drv.screenshotCalls.Load(); got != 1 {
		t.Fatalf("failures mode should write exactly one step-level screenshot on failure; got %d calls", got)
	}

	if len(out.ActionResults) != 2 {
		t.Fatalf("expected 2 results (failed + skipped), got %d", len(out.ActionResults))
	}

	if out.ActionResults[0].Status != actionStatusFail {
		t.Errorf("first action should be marked fail, got %q", out.ActionResults[0].Status)
	}

	if out.ActionResults[1].Status != actionStatusSkip {
		t.Errorf("second action should be marked skipped, got %q", out.ActionResults[1].Status)
	}
}

func TestCaptureActions_OneScreenshotPerActionAndOnFailure(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode()}
	p := newCaptureProvider(t, drv, CaptureActions)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// 2 actions × 1 capture each. The end-of-step hierarchy fetch is via
	// Hierarchy(), not Screenshot(), so the count stays clean.
	if got := drv.screenshotCalls.Load(); got != 2 {
		t.Fatalf("actions mode should screenshot once per action; got %d calls", got)
	}

	if len(out.ActionResults) != 2 {
		t.Fatalf("expected 2 action results, got %d", len(out.ActionResults))
	}

	for _, r := range out.ActionResults {
		if r.Screenshot == "" {
			t.Errorf("action %d %q expected a screenshot path", r.Index, r.Kind)

			continue
		}

		if _, statErr := os.Stat(r.Screenshot); statErr != nil {
			t.Errorf("action %d screenshot %q is not on disk: %v", r.Index, r.Screenshot, statErr)
		}

		// Path must nest under the action directory layout.
		if !strings.Contains(r.Screenshot, filepath.Join("actions", "")) {
			t.Errorf("screenshot %q is not under actions/ subdirectory", r.Screenshot)
		}
	}
}

func TestCaptureActions_FailedActionStillCaptures(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode(), tapErr: errors.New("boom")}
	p := newCaptureProvider(t, drv, CaptureActions)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err == nil {
		t.Fatal("expected action failure")
	}

	if out.ActionResults[0].Status != actionStatusFail {
		t.Fatalf("first action should be fail, got %q", out.ActionResults[0].Status)
	}

	if out.ActionResults[0].Screenshot == "" {
		t.Errorf("failed action should have a best-effort screenshot path")
	}
}

func TestCaptureSteps_AppendsSyntheticStepEndAction(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode()}
	p := newCaptureProvider(t, drv, CaptureSteps)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Two real actions get no per-action screenshot in steps mode; one
	// synthetic step_end captures.
	if got := drv.screenshotCalls.Load(); got != 1 {
		t.Fatalf("steps mode should screenshot exactly once (at step end); got %d calls", got)
	}

	if len(out.ActionResults) != 3 {
		t.Fatalf("expected 2 actions + 1 step_end = 3 results, got %d", len(out.ActionResults))
	}

	end := out.ActionResults[2]
	if end.Kind != "step_end" {
		t.Errorf("third result should be step_end, got %q", end.Kind)
	}

	if end.Screenshot == "" {
		t.Errorf("step_end result should have a screenshot path")
	}
}

func TestSecureActionValueIsMaskedInResult(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode()}
	p := newCaptureProvider(t, drv, CaptureFailures)

	actions := []provider.MobileActionExec{
		{Kind: model.MobileActionInputText, ID: "welcome.register", Value: "hunter2", Secure: true},
	}

	out, err := p.Execute(context.Background(), captureInput(actions))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(out.ActionResults) != 1 {
		t.Fatalf("expected 1 action result, got %d", len(out.ActionResults))
	}

	got := out.ActionResults[0]
	if got.Value != "***" {
		t.Errorf("secure action value should be masked to ***, got %q", got.Value)
	}

	if strings.Contains(got.Label, "hunter2") {
		t.Errorf("secure action label leaked raw value: %q", got.Label)
	}
}

func TestCaptureActions_ScreenshotFailureDoesNotMaskActionFailure(t *testing.T) {
	t.Parallel()

	drv := &recordingDriver{hierarchy: newButtonNode(), tapErr: errors.New("boom"), screenshotErr: errors.New("driver gone")}
	p := newCaptureProvider(t, drv, CaptureActions)

	out, err := p.Execute(context.Background(), captureInput(captureActions()))
	if err == nil {
		t.Fatal("expected the action failure to surface, not the screenshot error")
	}

	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected original action error %q, got %v", "boom", err)
	}

	if out.ActionResults[0].Status != actionStatusFail {
		t.Errorf("action should still report fail, got %q", out.ActionResults[0].Status)
	}
}
