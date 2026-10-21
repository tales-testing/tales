package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

var capturePNG = []byte{0x89, 0x50, 0x4E, 0x47}

// screenshotBehavior is what one Screenshot call does.
type screenshotBehavior func(ctx context.Context) ([]byte, error)

// hangScreenshot models the Chrome behavior behind issue #79: a screenshot
// requested while the page navigates by script is never answered, so the
// call only returns when its context ends.
func hangScreenshot(ctx context.Context) ([]byte, error) {
	<-ctx.Done()

	return nil, ctx.Err()
}

// navigatedScreenshot models a driver that saw the navigation and gave up.
func navigatedScreenshot(context.Context) ([]byte, error) {
	return nil, fmt.Errorf("capture screenshot: %w", driver.ErrNavigated)
}

func okScreenshot(context.Context) ([]byte, error) {
	return capturePNG, nil
}

// scriptedCaptureDriver plays one screenshotBehavior per Screenshot call
// (the last one repeats). WaitVisible fails at once on an expired context,
// as chromedp does, and records how much of its own deadline it was given.
type scriptedCaptureDriver struct {
	*driver.FakeDriver

	mu         sync.Mutex
	shots      []screenshotBehavior
	shotCalls  int
	waitBudget time.Duration
}

func newScriptedCaptureDriver(shots ...screenshotBehavior) *scriptedCaptureDriver {
	fake := driver.NewFakeDriver()
	fake.OuterHTMLValue = "<html><body>page</body></html>"

	return &scriptedCaptureDriver{FakeDriver: fake, shots: shots}
}

func (d *scriptedCaptureDriver) Screenshot(ctx context.Context) ([]byte, error) {
	d.mu.Lock()
	behavior := d.shots[min(d.shotCalls, len(d.shots)-1)]
	d.shotCalls++
	d.mu.Unlock()

	return behavior(ctx)
}

func (d *scriptedCaptureDriver) WaitVisible(ctx context.Context, selector string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if deadline, ok := ctx.Deadline(); ok {
		d.mu.Lock()
		d.waitBudget = time.Until(deadline)
		d.mu.Unlock()
	}

	if err, ok := d.FailOnSelector[selector]; ok {
		return err
	}

	return nil
}

func (d *scriptedCaptureDriver) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.shotCalls
}

// buildCaptureProvider wires drv into a provider whose capture bounds are
// shrunk so the hang cases run in milliseconds.
func buildCaptureProvider(t *testing.T, mode provider.CaptureMode, drv driver.Driver) *Provider {
	t.Helper()

	builder := SessionBuilderFunc{
		BuildFn: func(_ context.Context, target Target) (*Session, error) {
			return &Session{TargetName: target.Name, Target: target}, nil
		},
		ScenarioFn: func(_ context.Context, _ *Session, _ string) (*ScenarioBrowserCtx, error) {
			return &ScenarioBrowserCtx{Driver: drv}, nil
		},
	}

	p := New(WithSessionBuilder(builder), WithCaptureMode(mode), WithArtifactsBase(t.TempDir()))
	p.captureBudget = 300 * time.Millisecond
	p.captureAttemptTimeout = 100 * time.Millisecond

	t.Cleanup(func() { _ = p.Close() })

	return p
}

func executeCaptureStep(ctx context.Context, p *Provider, actions ...provider.BrowserActionExec) (*provider.Output, error) {
	return p.Execute(ctx, provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "navigate_then_wait", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  &provider.BrowserExecution{TargetName: "chrome", Actions: actions},
	})
}

// gotoThenWait is the shape of issue #79: the page navigates by script
// right after goto, so the capture of goto runs during that navigation.
func gotoThenWait() []provider.BrowserActionExec {
	return []provider.BrowserActionExec{
		{Kind: model.BrowserActionGoto, URL: "http://127.0.0.1/start.html"},
		{Kind: model.BrowserActionWaitVisible, Selector: "#email", Timeout: time.Second},
	}
}

func TestCaptureHangDoesNotStealTheNextActionTimeout(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(hangScreenshot)
	p := buildCaptureProvider(t, provider.CaptureActions, drv)

	// The step context stands for the suite --timeout. Before the fix the
	// capture waited on it, so wait_visible started with an expired
	// context and failed with "context deadline exceeded".
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()

	out, err := executeCaptureStep(ctx, p, gotoThenWait()...)
	if err != nil {
		t.Fatalf("the action after a hung capture must keep its own timeout, got: %v", err)
	}

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s: the hung capture was not bounded", elapsed)
	}

	if drv.waitBudget < 500*time.Millisecond {
		t.Fatalf("wait_visible got %s of its 1s timeout", drv.waitBudget)
	}

	gotoResult := out.ActionResults[0]
	if gotoResult.Screenshot != "" {
		t.Errorf("Screenshot = %q, want none for a capture that never answered", gotoResult.Screenshot)
	}

	if gotoResult.Hierarchy == "" {
		t.Error("the DOM must still be captured when only the screenshot hangs")
	}
}

func TestCaptureRetriesAScreenshotInterruptedByANavigation(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(navigatedScreenshot, okScreenshot)
	p := buildCaptureProvider(t, provider.CaptureActions, drv)

	out, err := executeCaptureStep(context.Background(), p, gotoThenWait()[0])
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if out.ActionResults[0].Screenshot == "" {
		t.Fatal("the screenshot must be retried against the new document")
	}

	if got := drv.calls(); got != 2 {
		t.Fatalf("screenshot calls = %d, want 2 (interrupted, then retried)", got)
	}
}

func TestCaptureDoesNotRetryAGenuineError(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(func(context.Context) ([]byte, error) {
		return nil, errors.New("target closed")
	})
	p := buildCaptureProvider(t, provider.CaptureActions, drv)

	out, err := executeCaptureStep(context.Background(), p, gotoThenWait()[0])
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if out.ActionResults[0].Screenshot != "" {
		t.Fatal("no screenshot can be written when the driver fails")
	}

	if got := drv.calls(); got != 1 {
		t.Fatalf("screenshot calls = %d, want 1: asking again would not change the answer", got)
	}
}

func TestCaptureGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(navigatedScreenshot)
	p := buildCaptureProvider(t, provider.CaptureActions, drv)

	out, err := executeCaptureStep(context.Background(), p, gotoThenWait()[0])
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if out.ActionResults[0].Screenshot != "" {
		t.Fatal("a page that keeps navigating must leave the frame empty")
	}

	if got := drv.calls(); got != captureMaxAttempts {
		t.Fatalf("screenshot calls = %d, want %d", got, captureMaxAttempts)
	}
}

func TestFailureCaptureHangKeepsTheActionError(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(hangScreenshot)
	drv.FailOnSelector = map[string]error{"#email": errors.New("selector #email not visible")}
	p := buildCaptureProvider(t, provider.CaptureFailures, drv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()

	_, err := executeCaptureStep(ctx, p, gotoThenWait()...)
	if err == nil {
		t.Fatal("wait_visible was programmed to fail")
	}

	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the action error must not be replaced by the capture's deadline: %v", err)
	}

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s: the failure capture was not bounded", elapsed)
	}
}

func TestStepLevelCaptureIsBounded(t *testing.T) {
	t.Parallel()

	drv := newScriptedCaptureDriver(hangScreenshot)
	p := buildCaptureProvider(t, provider.CaptureSteps, drv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()

	out, err := executeCaptureStep(ctx, p, gotoThenWait()...)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s: the step-level capture was not bounded", elapsed)
	}

	artifacts := out.Response["artifacts"]
	if artifacts.LengthInt() != 1 {
		t.Fatalf("artifacts = %#v, want the DOM only", artifacts)
	}
}
