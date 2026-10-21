package browser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// Status string constants — mirror mobile/capture.go so the runtime gets
// the same labels regardless of provider.
const (
	actionStatusPass = "pass"
	actionStatusFail = "fail"
	actionStatusSkip = "skipped"
)

// actionDefaultTimeout is the floor applied when neither the per-action
// timeout nor the target's configured timeout is set (or both resolved
// to zero). 30s matches the documented target default in
// website/.../providers/browser.mdx so the four resolution layers
// (per-expect → per-action → target → built-in) collapse to the same
// number from the user's perspective.
const actionDefaultTimeout = 30 * time.Second

// runActions iterates over the prepared actions, executing each against
// the driver. Failures stop the loop; subsequent actions are recorded as
// "skipped" so the visual report can show the full sequence.
func (p *Provider) runActions(ctx context.Context, drv driver.Driver, sc *ScenarioBrowserCtx, stepDir, defaultURL string, target Target, actions []provider.BrowserActionExec, stepFile string) ([]provider.ActionResult, error) {
	_ = sc

	results := make([]provider.ActionResult, 0, len(actions))

	for i, action := range actions {
		started := time.Now()
		result := newActionResult(i, action, started)

		debugf("action", "[%d/%d] kind=%s selector=%q value=%q", i, len(actions), action.Kind, action.Selector, action.Value)

		actionCtx, cancel := actionContext(ctx, action, target)

		point, err := p.performAction(actionCtx, drv, action, defaultURL, stepFile)

		cancel()

		result.Point = point

		result.Duration = time.Since(started)

		if err != nil {
			debugf("action", "[%d/%d] FAILED in %s: %v", i, len(actions), result.Duration, err)

			result.Status = actionStatusFail
			result.Err = err
			p.captureForAction(ctx, drv, stepDir, &result, true)
			results = append(results, result)
			appendSkipped(&results, actions[i+1:], i+1)

			return results, fmt.Errorf("action %d (%s): %w", i, action.Kind, err)
		}

		debugf("action", "[%d/%d] OK in %s", i, len(actions), result.Duration)

		result.Status = actionStatusPass
		p.captureForAction(ctx, drv, stepDir, &result, false)
		results = append(results, result)
	}

	return results, nil
}

func actionContext(parent context.Context, action provider.BrowserActionExec, target Target) (context.Context, context.CancelFunc) {
	timeout := action.Timeout
	if timeout == 0 {
		timeout = target.Driver.Timeout
	}

	if timeout == 0 {
		timeout = actionDefaultTimeout
	}

	return context.WithTimeout(parent, timeout)
}

// pointerActions maps the actions that drive the mouse to their Driver
// method. They run through actionability checks before any mouse event is
// sent, and report the point they used.
var pointerActions = map[model.BrowserActionKind]func(driver.Driver, context.Context, string, driver.PointerOptions) (driver.PointerResult, error){
	model.BrowserActionClick:   driver.Driver.Click,
	model.BrowserActionHover:   driver.Driver.Hover,
	model.BrowserActionCheck:   driver.Driver.Check,
	model.BrowserActionUncheck: driver.Driver.Uncheck,
}

// performAction runs one action. A pointer action also returns the point
// its mouse events went to, or the point its last actionability check
// tested when it failed; every other action returns a nil point.
func (p *Provider) performAction(ctx context.Context, drv driver.Driver, action provider.BrowserActionExec, defaultURL, stepFile string) (*provider.ActionPoint, error) {
	pointerAction, ok := pointerActions[action.Kind]
	if !ok {
		return nil, p.dispatchAction(ctx, drv, action, defaultURL, stepFile)
	}

	res, err := pointerAction(drv, ctx, action.Selector, driver.PointerOptions{Force: action.Force, Interval: action.Interval})
	if err != nil {
		var notActionable *driver.ActionabilityError
		if errors.As(err, &notActionable) {
			return actionPoint(notActionable.Point), wrapDriver(err)
		}

		return nil, wrapDriver(err)
	}

	return actionPoint(res.Point), nil
}

func actionPoint(point *driver.Point) *provider.ActionPoint {
	if point == nil {
		return nil
	}

	return &provider.ActionPoint{X: point.X, Y: point.Y}
}

// dispatchAction calls the right Driver method for the action kind. Each
// case wraps the driver-side error through wrapDriver so the runner sees
// consistent context regardless of which primitive surfaced the failure.
//
//nolint:gocyclo,exhaustive // Action surface is broad on purpose; the default returns a typed error for unknown kinds.
func (p *Provider) dispatchAction(ctx context.Context, drv driver.Driver, action provider.BrowserActionExec, defaultURL, stepFile string) error {
	switch action.Kind {
	case model.BrowserActionGoto:
		return wrapDriver(drv.Goto(ctx, resolveURL(defaultURL, action.URL)))
	case model.BrowserActionFill:
		return wrapDriver(drv.Fill(ctx, action.Selector, action.Value))
	case model.BrowserActionClear:
		return wrapDriver(drv.Clear(ctx, action.Selector))
	case model.BrowserActionPress:
		return press(ctx, drv, action)
	case model.BrowserActionSubmit:
		return wrapDriver(drv.Submit(ctx, action.Selector))
	case model.BrowserActionScroll:
		if action.Selector != "" {
			return wrapDriver(drv.ScrollIntoView(ctx, action.Selector))
		}

		return wrapDriver(drv.ScrollBy(ctx, action.X, action.Y))
	case model.BrowserActionWaitVisible:
		return wrapDriver(drv.WaitVisible(ctx, action.Selector))
	case model.BrowserActionWaitNotVisible:
		return wrapDriver(drv.WaitNotVisible(ctx, action.Selector))
	case model.BrowserActionWaitEnabled:
		// Same predicate as the `enabled` expectation, so a wait and an
		// assertion on the same element can never disagree.
		return matchEnabled(ctx, drv, action.Selector, true, action.Timeout, action.Interval)
	case model.BrowserActionWaitDisabled:
		return matchEnabled(ctx, drv, action.Selector, false, action.Timeout, action.Interval)
	case model.BrowserActionSelect:
		return wrapDriver(drv.SelectOption(ctx, action.Selector, action.Value))
	case model.BrowserActionUploadFile:
		paths, err := resolveUploadPaths(stepFile, action.Paths)
		if err != nil {
			return err
		}

		return wrapDriver(drv.SetFileInputs(ctx, action.Selector, paths))
	case model.BrowserActionReload:
		return wrapDriver(drv.Reload(ctx))
	case model.BrowserActionBack:
		return wrapDriver(drv.Back(ctx))
	case model.BrowserActionForward:
		return wrapDriver(drv.Forward(ctx))
	}

	return fmt.Errorf("unknown browser action %q", action.Kind)
}

// press checks the key before any driver sees it, so an unknown key fails
// the action whichever driver runs it. A literal key is already checked at
// load time; this catches a key computed from an expression, which a driver
// would otherwise type as text.
func press(ctx context.Context, drv driver.Driver, action provider.BrowserActionExec) error {
	if err := model.ValidateBrowserKey(action.Key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	return wrapDriver(drv.Press(ctx, action.Selector, action.Key))
}

func wrapDriver(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("driver: %w", err)
}

func newActionResult(index int, action provider.BrowserActionExec, started time.Time) provider.ActionResult {
	result := provider.ActionResult{
		Index:      index,
		Kind:       string(action.Kind),
		SelectorID: action.Selector,
		Secure:     action.Secure,
		StartedAt:  started,
	}

	if action.Value != "" {
		if action.Secure {
			result.Value = "***"
		} else {
			result.Value = action.Value
		}
	}

	result.Label = actionLabel(action)

	return result
}

func actionLabel(action provider.BrowserActionExec) string {
	//nolint:exhaustive // Only kinds with bespoke labels are listed; the catch-all below covers the rest.
	switch action.Kind {
	case model.BrowserActionGoto:
		return fmt.Sprintf("goto %s", action.URL)
	case model.BrowserActionFill:
		if action.Secure {
			return fmt.Sprintf("fill %s ***", action.Selector)
		}

		return fmt.Sprintf("fill %s %q", action.Selector, action.Value)
	case model.BrowserActionPress:
		if action.Selector == "" {
			return fmt.Sprintf("press %s", action.Key)
		}

		return fmt.Sprintf("press %s on %s", action.Key, action.Selector)
	case model.BrowserActionScroll:
		if action.Selector != "" {
			return fmt.Sprintf("scroll into view %s", action.Selector)
		}

		return fmt.Sprintf("scroll by (%d,%d)", action.X, action.Y)
	case model.BrowserActionUploadFile:
		return fmt.Sprintf("upload_file %s %v", action.Selector, action.Paths)
	case model.BrowserActionReload, model.BrowserActionBack, model.BrowserActionForward:
		return string(action.Kind)
	}

	label := string(action.Kind)
	if action.Selector != "" {
		label = fmt.Sprintf("%s %s", action.Kind, action.Selector)
	}

	if action.Force {
		label += " (force)"
	}

	return label
}

func appendSkipped(results *[]provider.ActionResult, remaining []provider.BrowserActionExec, baseIndex int) {
	for i, action := range remaining {
		r := newActionResult(baseIndex+i, action, time.Time{})
		r.Status = actionStatusSkip
		*results = append(*results, r)
	}
}
