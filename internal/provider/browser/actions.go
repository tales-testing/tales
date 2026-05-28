package browser

import (
	"context"
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
func (p *Provider) runActions(ctx context.Context, drv driver.Driver, sc *ScenarioBrowserCtx, stepDir, defaultURL string, target Target, actions []provider.BrowserActionExec) ([]provider.ActionResult, error) {
	_ = sc

	results := make([]provider.ActionResult, 0, len(actions))

	for i, action := range actions {
		started := time.Now()
		result := newActionResult(i, action, started)

		debugf("action", "[%d/%d] kind=%s selector=%q value=%q", i, len(actions), action.Kind, action.Selector, action.Value)

		actionCtx, cancel := actionContext(ctx, action, target)

		err := p.dispatchAction(actionCtx, drv, action, defaultURL)

		cancel()

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

// dispatchAction calls the right Driver method for the action kind. Each
// case wraps the driver-side error through wrapDriver so the runner sees
// consistent context regardless of which primitive surfaced the failure.
//
//nolint:gocyclo,exhaustive // Action surface is broad on purpose; the default returns a typed error for unknown kinds.
func (p *Provider) dispatchAction(ctx context.Context, drv driver.Driver, action provider.BrowserActionExec, defaultURL string) error {
	switch action.Kind {
	case model.BrowserActionGoto:
		return wrapDriver(drv.Goto(ctx, resolveURL(defaultURL, action.URL)))
	case model.BrowserActionClick:
		return wrapDriver(drv.Click(ctx, action.Selector))
	case model.BrowserActionFill:
		return wrapDriver(drv.Fill(ctx, action.Selector, action.Value))
	case model.BrowserActionClear:
		return wrapDriver(drv.Clear(ctx, action.Selector))
	case model.BrowserActionPress:
		return wrapDriver(drv.Press(ctx, action.Selector, action.Key))
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
	case model.BrowserActionHover:
		return wrapDriver(drv.Hover(ctx, action.Selector))
	case model.BrowserActionSelect:
		return wrapDriver(drv.SelectOption(ctx, action.Selector, action.Value))
	case model.BrowserActionCheck:
		return wrapDriver(drv.Check(ctx, action.Selector))
	case model.BrowserActionUncheck:
		return wrapDriver(drv.Uncheck(ctx, action.Selector))
	case model.BrowserActionReload:
		return wrapDriver(drv.Reload(ctx))
	case model.BrowserActionBack:
		return wrapDriver(drv.Back(ctx))
	case model.BrowserActionForward:
		return wrapDriver(drv.Forward(ctx))
	}

	return fmt.Errorf("unknown browser action %q", action.Kind)
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
	case model.BrowserActionReload, model.BrowserActionBack, model.BrowserActionForward:
		return string(action.Kind)
	}

	if action.Selector != "" {
		return fmt.Sprintf("%s %s", action.Kind, action.Selector)
	}

	return string(action.Kind)
}

func appendSkipped(results *[]provider.ActionResult, remaining []provider.BrowserActionExec, baseIndex int) {
	for i, action := range remaining {
		r := newActionResult(baseIndex+i, action, time.Time{})
		r.Status = actionStatusSkip
		*results = append(*results, r)
	}
}
