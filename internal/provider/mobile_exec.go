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
