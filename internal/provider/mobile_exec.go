package provider

import (
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// MobileExecution is the runtime-evaluated payload the mobile provider
// receives via Input.Mobile. It mirrors model.MobileStep but every HCL
// expression has been resolved to a Go value by the runtime so the provider
// stays free of HCL/cty dependencies.
type MobileExecution struct {
	Platform   string
	TargetName string
	Launch     *MobileLaunchExec
	Terminate  *MobileTerminateExec
	Actions    []MobileActionExec
	Expect     MobileExpectExec
}

// MobileLaunchExec carries the resolved launch block fields.
type MobileLaunchExec struct {
	ClearState bool
}

// MobileTerminateExec marks that the step must terminate the application.
type MobileTerminateExec struct{}

// MobileActionExec is one ordered UI action ready to be executed.
type MobileActionExec struct {
	Kind     model.MobileActionKind
	ID       string
	Value    string
	Secure   bool
	Timeout  time.Duration
	Interval time.Duration
	File     string
	Line     int
	// Direction is "up" / "down" / "left" / "right" for swipe and scroll.
	Direction string
	// Distance is the resolved swipe/scroll travel fraction in (0,1].
	// Zero means "use the provider default".
	Distance float64
	// Duration is the resolved gesture duration for swipe / scroll /
	// long_press. Zero means "use the provider default".
	Duration time.Duration
}

// MobileExpectExec groups visibility expectations for the step.
type MobileExpectExec struct {
	Visible    []MobileVisibilityExec
	NotVisible []MobileVisibilityExec
	Text       []MobileValueExpectationExec
	Value      []MobileValueExpectationExec
	Enabled    []MobileStateExpectationExec
	Disabled   []MobileStateExpectationExec
}

// HasAny reports whether any expectation kind carries at least one entry.
// Used by the provider to skip the expectation phase entirely when the step
// has none.
func (e MobileExpectExec) HasAny() bool {
	return len(e.Visible) > 0 || len(e.NotVisible) > 0 ||
		len(e.Text) > 0 || len(e.Value) > 0 ||
		len(e.Enabled) > 0 || len(e.Disabled) > 0
}

// MobileVisibilityExec is a resolved visible / not_visible expectation.
type MobileVisibilityExec struct {
	ID       string
	Timeout  time.Duration
	Interval time.Duration
}

// MobileValueExpectationExec compares text/value content for an element.
type MobileValueExpectationExec struct {
	ID       string
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}

// MobileStateExpectationExec checks enabled/disabled state for an element.
type MobileStateExpectationExec struct {
	ID       string
	Timeout  time.Duration
	Interval time.Duration
}
