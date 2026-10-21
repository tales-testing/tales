package browser

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// Capture bounds. A capture is evidence for the report, never part of what
// the step asserts, so it must not be able to hold the run. Chrome does not
// answer a screenshot whose document a navigation replaces (issue #79), and
// the capture used to wait for that answer on the step context, whose only
// deadline is the suite --timeout: the action that followed then started
// with an expired context and failed in microseconds, blamed for a hang it
// did not cause.
const (
	// defaultCaptureBudget caps one capture (screenshot + DOM) as a whole.
	defaultCaptureBudget = 5 * time.Second
	// defaultCaptureAttemptTimeout caps one driver call inside a capture.
	defaultCaptureAttemptTimeout = 2 * time.Second
	// captureMaxAttempts caps how often one driver call is retried after
	// running out of time or being interrupted by a navigation.
	captureMaxAttempts = 3
)

// captureForAction is a best-effort screenshot + DOM capture. Capture mode
// gates whether we capture at all on success; failures always capture when
// the mode is anything but None.
func (p *Provider) captureForAction(ctx context.Context, drv driver.Driver, stepDir string, result *provider.ActionResult, forFailure bool) {
	if p.captureMode == provider.CaptureNone {
		return
	}

	if !forFailure && p.captureMode != provider.CaptureActions {
		return
	}

	if stepDir == "" {
		return
	}

	dir := actionArtifactDir(stepDir, result.Index, result.Kind, result.SelectorID)
	frame := p.capture(ctx, drv)

	if len(frame.png) > 0 {
		if a, werr := writeScreenshot(dir, frame.png); werr == nil {
			result.Screenshot = a.Path
		}
	}

	if frame.dom != "" {
		if a, werr := writeDOM(dir, frame.dom); werr == nil {
			result.Hierarchy = a.Path
		}
	}
}

// captureStepLevel writes the end-of-step screenshot + DOM under the step's
// step/ subdirectory. Returns an artifact-list ready for output.Response.
func (p *Provider) captureStepLevel(ctx context.Context, drv driver.Driver, stepDir string, forFailure bool) []Artifact {
	if p.captureMode == provider.CaptureNone {
		return nil
	}

	if !forFailure && p.captureMode == provider.CaptureFailures {
		return nil
	}

	if stepDir == "" {
		return nil
	}

	dir := stepLevelArtifactDir(stepDir)
	out := make([]Artifact, 0, 2)
	frame := p.capture(ctx, drv)

	if len(frame.png) > 0 {
		if a, werr := writeScreenshot(dir, frame.png); werr == nil {
			out = append(out, a)
		}
	}

	if frame.dom != "" {
		if a, werr := writeDOM(dir, frame.dom); werr == nil {
			out = append(out, a)
		}
	}

	return out
}

// capturedFrame is what one capture read from the page. A part the browser
// did not deliver in time is left empty.
type capturedFrame struct {
	png []byte
	dom string
}

// capture reads the screenshot and the DOM under the capture budget, each
// read with its own short deadline, so a page that navigates under the
// capture costs at most the budget and leaves the next action its whole
// timeout.
func (p *Provider) capture(ctx context.Context, drv driver.Driver) capturedFrame {
	ctx, cancel := context.WithTimeout(ctx, p.captureBudget)
	defer cancel()

	var frame capturedFrame

	png, err := retryCapture(ctx, p.captureAttemptTimeout, drv.Screenshot)
	if err != nil {
		debugf("capture", "screenshot skipped: %v", err)
	} else {
		frame.png = png
	}

	dom, err := retryCapture(ctx, p.captureAttemptTimeout, func(ctx context.Context) (string, error) {
		return drv.OuterHTML(ctx, "html")
	})
	if err != nil {
		debugf("capture", "dom skipped: %v", err)
	} else {
		frame.dom = dom
	}

	return frame
}

// retryCapture runs one capture read under attemptTimeout. An attempt that
// runs out of time, or that the page's own navigation interrupts
// (driver.ErrNavigated), is retried against the new document, at most
// captureMaxAttempts times and never past ctx. Any other error is returned
// at once: asking again would not change it.
func retryCapture[T any](ctx context.Context, attemptTimeout time.Duration, read func(context.Context) (T, error)) (T, error) {
	var (
		value T
		err   error
	)

	for attempt := 1; attempt <= captureMaxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		value, err = read(attemptCtx)
		expired := attemptCtx.Err() != nil

		cancel()

		if err == nil || ctx.Err() != nil {
			break
		}

		if !expired && !errors.Is(err, driver.ErrNavigated) {
			break
		}

		debugf("capture", "attempt %d/%d interrupted: %v", attempt, captureMaxAttempts, err)
	}

	return value, err
}

// resolveURL joins a base URL with a possibly-relative target. Behavior:
//   - empty base → target verbatim
//   - empty target → "" (caller's responsibility to validate before reaching here)
//   - absolute target (has scheme) → target verbatim
//   - relative target → resolved against base via net/url
//
// A malformed base falls back to verbatim concatenation so we never silently
// crash inside an action handler.
func resolveURL(base, target string) string {
	if target == "" || base == "" {
		return target
	}

	if strings.Contains(target, "://") {
		return target
	}

	b, err := url.Parse(base)
	if err != nil {
		return base + target
	}

	t, err := url.Parse(target)
	if err != nil {
		return base + target
	}

	return b.ResolveReference(t).String()
}
