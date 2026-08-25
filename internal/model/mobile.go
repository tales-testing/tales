package model

// MobileActionKind identifies the kind of UI action performed inside a mobile step.
type MobileActionKind string

const (
	// MobileActionTap taps an element identified by accessibility ID.
	MobileActionTap MobileActionKind = "tap"
	// MobileActionInputText types text into an element identified by accessibility ID.
	MobileActionInputText MobileActionKind = "input_text"
	// MobileActionClearText erases the current value of an element identified by accessibility ID.
	MobileActionClearText MobileActionKind = "clear_text"
	// MobileActionWaitVisible waits until an element exists and is visible.
	MobileActionWaitVisible MobileActionKind = "wait_visible"
	// MobileActionWaitNotVisible waits until an element is missing or not visible.
	MobileActionWaitNotVisible MobileActionKind = "wait_not_visible"
	// MobileActionWaitEnabled waits until an element exists and is enabled.
	// Screens that arm asynchronously (a capture button waiting on the
	// camera, a submit button unlocked by a background validation) leave
	// the control visible but inert in between, and a tap on an inert
	// control is swallowed with no error.
	MobileActionWaitEnabled MobileActionKind = "wait_enabled"
	// MobileActionWaitDisabled waits until an element exists and is disabled,
	// e.g. until a submit button locks itself while the request is in flight.
	MobileActionWaitDisabled MobileActionKind = "wait_disabled"
	// MobileActionSwipe drags one finger across an element in a direction.
	MobileActionSwipe MobileActionKind = "swipe"
	// MobileActionScroll scrolls an element's content in a direction.
	MobileActionScroll MobileActionKind = "scroll"
	// MobileActionLongPress presses and holds an element for a duration.
	MobileActionLongPress MobileActionKind = "long_press"
	// MobileActionDoubleTap taps an element twice in quick succession.
	MobileActionDoubleTap MobileActionKind = "double_tap"
	// MobileActionPressKey presses a hardware keyboard key (return, tab…).
	MobileActionPressKey MobileActionKind = "press_key"
	// MobileActionPressButton presses a device button (home, lock).
	MobileActionPressButton MobileActionKind = "press_button"
	// MobileActionSetOrientation changes the device orientation.
	MobileActionSetOrientation MobileActionKind = "set_orientation"
	// MobileActionDismissKeyboard tells the driver to dismiss the soft
	// keyboard if one is up. Idempotent: a no-op when no keyboard is
	// present. Useful before a /hierarchy snapshot on screens whose
	// keyboard subtree makes the full-application snapshot exceed the
	// driver's bounded timeout.
	MobileActionDismissKeyboard MobileActionKind = "dismiss_keyboard"
	// MobileActionScrollTo asks the driver to scroll the element
	// identified by id or label into the viewport. Idempotent (no-op
	// when the element is already in the safe area). Indispensable
	// before an input_text on a tall SwiftUI Form where the target
	// field is offscreen: the focus tap would otherwise miss, the
	// follow-up synth path would trip an XCTest API violation and tear
	// the runner down.
	MobileActionScrollTo MobileActionKind = "scroll_to"
)

// MobileStep is the provider-specific payload attached to a Step when Provider == "mobile".
type MobileStep struct {
	Platform    Expression
	Target      Expression
	Launch      *MobileLaunch
	Terminate   *MobileTerminate
	Actions     []MobileAction
	Permissions []MobilePermission
	Expect      MobileExpect
}

// MobilePermission is one privacy permission declared in a step's
// `permissions` block. Service is a simctl privacy service name (camera,
// photos, location, …); Decision evaluates to "allow" or "deny".
type MobilePermission struct {
	Service  string
	Decision Expression
	File     string
	Line     int
}

// MobileLaunch describes the optional launch block of a mobile step.
type MobileLaunch struct {
	ClearState Expression
}

// MobileTerminate is the marker block requesting application termination.
type MobileTerminate struct{}

// MobileAction is one ordered UI action inside an actions block.
type MobileAction struct {
	Kind MobileActionKind
	File string
	Line int
	ID   Expression
	// Label locates the element by accessibilityLabel instead of
	// accessibilityIdentifier. Mutually exclusive with ID; parser
	// enforces XOR. Reserved for iOS system controllers whose buttons
	// expose a label but leave the identifier empty
	// (PHPickerViewController, document picker, share sheet, ...).
	Label Expression
	// Text locates the element by its visible text, falling back to
	// the accessibility label when the node has none. Mutually
	// exclusive with ID and Label; the parser enforces the XOR.
	Text     Expression
	Value    Expression
	Secure   Expression
	Timeout  Expression
	Interval Expression
	// Direction is "up" / "down" / "left" / "right" for swipe and scroll.
	Direction Expression
	// Distance is the swipe/scroll travel as a fraction (0,1] of the
	// target element's relevant dimension. Optional; defaults applied
	// by the runtime.
	Distance Expression
	// Duration is the gesture duration for swipe / scroll / long_press.
	Duration Expression
	// First requests pre-order first-match resolution for the action's
	// element id, mirroring XCUITest's firstMatch. When true, sibling
	// elements that share the id no longer error with ErrDuplicate; the
	// first match in DFS order is used. Only consulted by actions that
	// resolve an element by id; the strict default is preserved when the
	// expression is unset or evaluates to false.
	First Expression
}

// MobileExpect groups visibility expectations for a mobile step.
type MobileExpect struct {
	Visible    []MobileVisibility
	NotVisible []MobileVisibility
	Text       []MobileValueExpectation
	Value      []MobileValueExpectation
	Enabled    []MobileStateExpectation
	Disabled   []MobileStateExpectation
}

// MobileVisibility describes one element visibility expectation with optional polling timeout.
type MobileVisibility struct {
	ID    Expression
	Label Expression
	// Text locates the element by its visible text, falling back to
	// the accessibility label when the node has none. Mutually
	// exclusive with ID and Label; the parser enforces the XOR.
	Text     Expression
	Timeout  Expression
	Interval Expression
}

// MobileValueExpectation compares text/value content for an element.
type MobileValueExpectation struct {
	ID    Expression
	Label Expression
	// Text locates the element by its visible text, falling back to
	// the accessibility label when the node has none. Mutually
	// exclusive with ID and Label; the parser enforces the XOR.
	Text     Expression
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// MobileStateExpectation checks enabled / disabled state for an element.
type MobileStateExpectation struct {
	ID    Expression
	Label Expression
	// Text locates the element by its visible text, falling back to
	// the accessibility label when the node has none. Mutually
	// exclusive with ID and Label; the parser enforces the XOR.
	Text     Expression
	Timeout  Expression
	Interval Expression
}

// HasContent reports whether the mobile step carries any operation worth executing.
func (m *MobileStep) HasContent() bool {
	if m == nil {
		return false
	}

	if m.Launch != nil || m.Terminate != nil {
		return true
	}

	if len(m.Actions) > 0 {
		return true
	}

	if len(m.Expect.Visible) > 0 || len(m.Expect.NotVisible) > 0 ||
		len(m.Expect.Text) > 0 || len(m.Expect.Value) > 0 ||
		len(m.Expect.Enabled) > 0 || len(m.Expect.Disabled) > 0 {
		return true
	}

	return false
}
