package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// checkStateScript reads the checked state of a checkbox-like element: an
// input[type=checkbox|radio], a <label> of one (its control is read, the
// label itself is what gets clicked), or an element with an ARIA
// checkbox / switch / radio role (aria-checked is read).
const checkStateScript = `(function (selector) {
  var element;

  try {
    element = document.querySelector(selector);
  } catch (error) {
    return { fatal: "invalid selector: " + error.message };
  }

  if (!element) {
    return { found: false };
  }

  var control = element;

  if (element.localName === "label" && element.control) {
    control = element.control;
  }

  if (control.localName === "input" && (control.type === "checkbox" || control.type === "radio")) {
    return { found: true, checked: control.checked, radio: control.type === "radio", input: element === control };
  }

  var role = control.getAttribute("role");

  if (role === "checkbox" || role === "switch" || role === "menuitemcheckbox") {
    return { found: true, checked: control.getAttribute("aria-checked") === "true", radio: false };
  }

  if (role === "radio" || role === "menuitemradio") {
    return { found: true, checked: control.getAttribute("aria-checked") === "true", radio: true };
  }

  return { fatal: "element is not a checkbox or a radio button (input[type=checkbox], input[type=radio], a label of one, or an element with role checkbox, switch or radio)" };
})`

// checkState is the answer of checkStateScript.
type checkState struct {
	Found   bool `json:"found"`
	Checked bool `json:"checked"`
	Radio   bool `json:"radio"`
	// Input is true when the selector names the <input> itself rather
	// than its label or an ARIA widget.
	Input bool   `json:"input"`
	Fatal string `json:"fatal"`
}

// labelHint is appended to the failure of a check / uncheck whose <input>
// cannot be clicked. Styled checkboxes hide the input or draw a substitute
// over it; the label is what a user clicks.
const labelHint = "; a styled checkbox is clicked through its <label>: point the selector at the label"

// errStateUnchanged is returned by awaitCheckState when the state never
// reached the wanted one before ctx expired.
var errStateUnchanged = errors.New("state did not change")

// setChecked brings a checkbox-like element to the wanted state the way a
// user does: nothing when it already is, otherwise a click (with the
// actionability checks of every pointer action), then a check that the
// click did flip it. A radio button cannot be unchecked by a click.
func (d *chromedpDriver) setChecked(ctx context.Context, action pointerAction, selector string, want bool, opts driver.PointerOptions) (driver.PointerResult, error) {
	start := time.Now()
	read := func(ctx context.Context) (checkState, error) { return d.checkState(ctx, selector) }

	state, err := awaitCheckState(ctx, opts.Interval, read, func(s checkState) bool { return s.Found })
	if err != nil {
		if state.Fatal != "" {
			return driver.PointerResult{}, fmt.Errorf("%s %s: %s", action.name, selector, state.Fatal)
		}

		return driver.PointerResult{}, &driver.ActionabilityError{
			Action:   action.name,
			Selector: selector,
			Reason:   probeResult{Reason: reasonNotFound}.describe(),
			Timeout:  budgetOf(ctx, start),
		}
	}

	if state.Checked == want {
		return driver.PointerResult{}, nil
	}

	if !want && state.Radio {
		return driver.PointerResult{}, fmt.Errorf("%s %s: a radio button cannot be unchecked by clicking it; check another option of its group", action.name, selector)
	}

	res, err := d.pointer(ctx, start, action, selector, opts)
	if err != nil {
		var notActionable *driver.ActionabilityError
		if state.Input && errors.As(err, &notActionable) {
			notActionable.Reason += labelHint
		}

		return res, err
	}

	if _, err := awaitCheckState(ctx, opts.Interval, read, func(s checkState) bool { return s.Found && s.Checked == want }); err != nil {
		return res, fmt.Errorf("%s %s: the click at %s did not change its checked state", action.name, selector, res.Point)
	}

	return res, nil
}

// checkState evaluates checkStateScript once.
func (d *chromedpDriver) checkState(ctx context.Context, selector string) (checkState, error) {
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return checkState{}, fmt.Errorf("encode selector: %w", err)
	}

	var state checkState

	err = d.run(ctx, chromedp.Evaluate(fmt.Sprintf("(%s)(%s)", checkStateScript, selectorJSON), &state))

	return state, err
}

// awaitCheckState reads the state until done accepts it, the state is
// fatal (not a checkbox), or ctx expires. Read errors (a navigation in
// flight) are retried. It returns the last state read.
func awaitCheckState(ctx context.Context, interval time.Duration, read func(context.Context) (checkState, error), done func(checkState) bool) (checkState, error) {
	if interval <= 0 {
		interval = defaultProbeInterval
	}

	var last checkState

	for {
		state, err := read(ctx)

		switch {
		case err != nil && ctx.Err() != nil:
			return last, errStateUnchanged
		case err != nil:
			// Keep the previous answer and read again.
		case state.Fatal != "":
			return state, errors.New(state.Fatal)
		case done(state):
			return state, nil
		default:
			last = state
		}

		select {
		case <-ctx.Done():
			return last, errStateUnchanged
		case <-time.After(interval):
		}
	}
}
