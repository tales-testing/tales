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
	Kind     MobileActionKind
	File     string
	Line     int
	ID       Expression
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
	ID       Expression
	Timeout  Expression
	Interval Expression
}

// MobileValueExpectation compares text/value content for an element.
type MobileValueExpectation struct {
	ID       Expression
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// MobileStateExpectation checks enabled / disabled state for an element.
type MobileStateExpectation struct {
	ID       Expression
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
