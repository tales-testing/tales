package chrome

import (
	"context"
	_ "embed" // actionability.js is embedded as probeScript.
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// probeScript is the actionability probe: a JS function taking
// (selector, options) and answering a probeResult. See actionability.js.
//
//go:embed actionability.js
var probeScript string

// defaultProbeInterval is the pause between two actionability probes when
// the action sets no `interval`. A probe itself spans two animation
// frames, so an element that settles is clicked within about 130ms.
const defaultProbeInterval = 100 * time.Millisecond

// Reason codes answered by the probe when the element is not actionable
// yet. Each one is retried until the action's deadline.
const (
	reasonNotFound        = "not_found"
	reasonNotVisible      = "not_visible"
	reasonDisabled        = "disabled"
	reasonNotStable       = "not_stable"
	reasonNoBox           = "no_box"
	reasonOutsideViewport = "outside_viewport"
	reasonCovered         = "covered"
	reasonHitsAncestor    = "hits_ancestor"
	// reasonProbeFailed is set on the Go side when the probe itself did
	// not answer, e.g. because a navigation destroyed its context.
	reasonProbeFailed = "probe_failed"
)

// pointerAction selects what a pointer action sends once its element is
// actionable, and which checks it needs.
type pointerAction struct {
	// name is the action kind used in error messages ("click").
	name string
	// press sends a left-button press and release after the move.
	// Without it, only the move is sent (hover).
	press bool
	// requireEnabled refuses a disabled form control. A click on one is
	// swallowed by the browser; hovering one is legitimate (tooltips).
	requireEnabled bool
}

var (
	clickAction   = pointerAction{name: "click", press: true, requireEnabled: true}
	hoverAction   = pointerAction{name: "hover"}
	checkAction   = pointerAction{name: "check", press: true, requireEnabled: true}
	uncheckAction = pointerAction{name: "uncheck", press: true, requireEnabled: true}
)

// probeOptions is the second argument of the probe script.
type probeOptions struct {
	Force          bool `json:"force"`
	RequireEnabled bool `json:"requireEnabled"`
	// ScrollAttempt counts the consecutive probes that found the point
	// covered; the probe varies its scroll alignment with it.
	ScrollAttempt int `json:"scrollAttempt"`
}

// probeResult is the answer of the probe script.
type probeResult struct {
	Ready  bool     `json:"ready"`
	Reason string   `json:"reason"`
	Fatal  string   `json:"fatal"`
	Hit    string   `json:"hit"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	// detail carries the error of a probe that did not answer.
	detail string
}

// point returns the point the probe tested, or nil when it did not get
// that far.
func (r probeResult) point() *driver.Point {
	if r.X == nil || r.Y == nil {
		return nil
	}

	return &driver.Point{X: *r.X, Y: *r.Y}
}

// describe renders the reason in words for an ActionabilityError.
func (r probeResult) describe() string {
	switch r.Reason {
	case reasonNotFound:
		return "element not found"
	case reasonNotVisible:
		return "element is not visible"
	case reasonDisabled:
		return "element is disabled"
	case reasonNotStable:
		return "element is not stable (its box moved between two animation frames)"
	case reasonNoBox:
		return "element has no box to point at (display: none?)"
	case reasonOutsideViewport:
		return "element is outside of the viewport"
	case reasonCovered:
		return fmt.Sprintf("point %s is covered by %s", r.pointString(), r.Hit)
	case reasonHitsAncestor:
		return fmt.Sprintf("point %s hits its ancestor %s, not the element (pointer-events: none, or clipped?)", r.pointString(), r.Hit)
	case reasonProbeFailed:
		return "the page did not answer the actionability probe: " + r.detail
	case "":
		return "the page did not answer the actionability probe"
	}

	return r.Reason
}

func (r probeResult) pointString() string {
	if p := r.point(); p != nil {
		return p.String()
	}

	return "(?, ?)"
}

// awaitActionable runs probe until it answers ready, answers a fatal
// problem, or ctx expires. It returns the last answer, and a non-nil error
// unless that answer is ready. A probe that errors (the page navigated
// mid-probe) is retried; when ctx is what interrupted it, the answer
// before it is kept, so the error names a real check rather than the
// deadline. scrollAttempt is handed to probe as the count of consecutive
// "covered" answers.
func awaitActionable(ctx context.Context, interval time.Duration, probe func(ctx context.Context, scrollAttempt int) (probeResult, error)) (probeResult, error) {
	if interval <= 0 {
		interval = defaultProbeInterval
	}

	var (
		last    probeResult
		covered int
	)

	for {
		res, err := probe(ctx, covered)

		switch {
		case err != nil && ctx.Err() != nil:
			return last, fmt.Errorf("wait for actionability: %w", ctx.Err())
		case err != nil:
			last = probeResult{Reason: reasonProbeFailed, detail: err.Error()}
		case res.Fatal != "":
			return res, errors.New(res.Fatal)
		case res.Ready:
			return res, nil
		default:
			last = res
		}

		if last.Reason == reasonCovered || last.Reason == reasonHitsAncestor {
			covered++
		} else {
			covered = 0
		}

		select {
		case <-ctx.Done():
			return last, fmt.Errorf("wait for actionability: %w", ctx.Err())
		case <-time.After(interval):
		}
	}
}

// actionabilityError turns the outcome of awaitActionable into the error
// the action reports. budget is the time the action had.
func actionabilityError(action pointerAction, selector string, last probeResult, err error, budget time.Duration) error {
	if last.Fatal != "" {
		return fmt.Errorf("%s %s: %w", action.name, selector, err)
	}

	return &driver.ActionabilityError{
		Action:   action.name,
		Selector: selector,
		Reason:   last.describe(),
		Point:    last.point(),
		Timeout:  budget,
	}
}

// budgetOf is the time left before ctx's deadline, zero without one.
func budgetOf(ctx context.Context, start time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}

	return deadline.Sub(start)
}

// pointer waits until selector is actionable for action, then sends the
// mouse to it: a move, then a press and release for the actions that
// click. The point is recomputed by every probe, after its scroll. start
// is when the action began, so a failure reports the action's whole
// budget.
func (d *chromedpDriver) pointer(ctx context.Context, start time.Time, action pointerAction, selector string, opts driver.PointerOptions) (driver.PointerResult, error) {
	res, err := awaitActionable(ctx, opts.Interval, func(ctx context.Context, scrollAttempt int) (probeResult, error) {
		return d.probe(ctx, selector, probeOptions{
			Force:          opts.Force,
			RequireEnabled: action.requireEnabled,
			ScrollAttempt:  scrollAttempt,
		})
	})
	if err != nil {
		return driver.PointerResult{}, actionabilityError(action, selector, res, err, budgetOf(ctx, start))
	}

	point := res.point()
	if point == nil {
		return driver.PointerResult{}, fmt.Errorf("%s %s: the actionability probe answered no point", action.name, selector)
	}

	if err := d.run(ctx, mouseActions(action, *point)...); err != nil {
		return driver.PointerResult{Point: point}, fmt.Errorf("%s %s: dispatch mouse events at %s: %w", action.name, selector, point, err)
	}

	return driver.PointerResult{Point: point}, nil
}

// probe evaluates the probe script once.
func (d *chromedpDriver) probe(ctx context.Context, selector string, opts probeOptions) (probeResult, error) {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return probeResult{}, fmt.Errorf("encode selector: %w", err)
	}

	optsJSON, err := json.Marshal(opts)
	if err != nil {
		return probeResult{}, fmt.Errorf("encode probe options: %w", err)
	}

	var res probeResult

	expression := fmt.Sprintf("(%s)(%s, %s)", probeScript, selectorJSON, optsJSON)

	err = d.run(ctx, chromedp.Evaluate(expression, &res, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))

	return res, err
}

// mouseActions are the CDP mouse events of a pointer action at point:
// the move a real pointer makes before it presses, so hover state and
// mouseover / mouseenter handlers see the pointer arrive, then the press
// and release of a single left click.
func mouseActions(action pointerAction, point driver.Point) []chromedp.Action {
	actions := []chromedp.Action{
		input.DispatchMouseEvent(input.MouseMoved, point.X, point.Y),
	}

	if action.press {
		actions = append(actions,
			input.DispatchMouseEvent(input.MousePressed, point.X, point.Y).
				WithButton(input.Left).WithButtons(1).WithClickCount(1),
			input.DispatchMouseEvent(input.MouseReleased, point.X, point.Y).
				WithButton(input.Left).WithClickCount(1),
		)
	}

	return actions
}
