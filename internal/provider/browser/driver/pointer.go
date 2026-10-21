package driver

import (
	"fmt"
	"math"
	"time"
)

// PointerOptions tunes a pointer action: click, hover, check, uncheck.
type PointerOptions struct {
	// Force skips the actionability checks (visible, enabled, stable,
	// receives events). The element must still be present, with a box
	// inside the viewport for the mouse to be sent to.
	Force bool
	// Interval is the pause between two actionability probes. Zero
	// selects the driver's default.
	Interval time.Duration
}

// Point is a position in CSS pixels from the top-left corner of the
// viewport: the coordinate space of the mouse events a driver dispatches.
type Point struct {
	X float64
	Y float64
}

// String renders the point rounded to whole pixels, e.g. "(912, 684)".
func (p Point) String() string {
	return fmt.Sprintf("(%d, %d)", int(math.Round(p.X)), int(math.Round(p.Y)))
}

// PointerResult describes a pointer action that completed.
type PointerResult struct {
	// Point is where the mouse events were dispatched. Nil when the
	// action needed none, e.g. check on a box that is already checked.
	Point *Point
}

// ActionabilityError reports a pointer action whose element never became
// actionable before the action's deadline. Reason names the check that was
// still failing when the deadline expired, so the failure points at the
// cause instead of surfacing later on an unrelated wait.
type ActionabilityError struct {
	// Action is the action kind, e.g. "click".
	Action string
	// Selector is the CSS selector of the target element.
	Selector string
	// Reason is the last failing check, in words, e.g.
	// `point (912, 684) is covered by <div class="toast">`.
	Reason string
	// Point is the point the last probe tested, when it got that far.
	Point *Point
	// Timeout is the budget that expired. Zero when unknown.
	Timeout time.Duration
}

// Error implements error.
func (e *ActionabilityError) Error() string {
	if e.Timeout <= 0 {
		return fmt.Sprintf("%s %s: not actionable: %s", e.Action, e.Selector, e.Reason)
	}

	return fmt.Sprintf("%s %s: not actionable after %s: %s", e.Action, e.Selector, e.Timeout.Round(10*time.Millisecond), e.Reason)
}
