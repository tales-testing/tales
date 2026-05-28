package provider

import (
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// BrowserExecution is the runtime-evaluated payload the browser provider
// receives via Input.Browser. Every HCL expression has been resolved to a Go
// value by the runtime so the provider stays free of HCL/cty dependencies.
type BrowserExecution struct {
	TargetName string
	Actions    []BrowserActionExec
	Expect     BrowserExpectExec
}

// BrowserActionExec is one ordered browser action ready to be executed.
type BrowserActionExec struct {
	Kind     model.BrowserActionKind
	Selector string
	Value    string
	URL      string
	Key      string
	Secure   bool
	Timeout  time.Duration
	Interval time.Duration
	X        int
	Y        int
	File     string
	Line     int
}

// BrowserExpectExec groups resolved expectations for a browser step.
type BrowserExpectExec struct {
	Visible    []BrowserVisibilityExec
	NotVisible []BrowserVisibilityExec
	Text       []BrowserValueExpectationExec
	Value      []BrowserValueExpectationExec
	Enabled    []BrowserStateExpectationExec
	Disabled   []BrowserStateExpectationExec
	Attribute  []BrowserAttributeExpectationExec
	URL        []BrowserURLExpectationExec
	Title      []BrowserTitleExpectationExec
	WebPerf    []BrowserWebPerfExpectationExec
}

// HasAny reports whether any expectation kind carries at least one entry.
func (e BrowserExpectExec) HasAny() bool {
	return len(e.Visible) > 0 || len(e.NotVisible) > 0 ||
		len(e.Text) > 0 || len(e.Value) > 0 ||
		len(e.Enabled) > 0 || len(e.Disabled) > 0 ||
		len(e.Attribute) > 0 || len(e.URL) > 0 || len(e.Title) > 0 ||
		len(e.WebPerf) > 0
}

// BrowserWebPerfExpectationExec asserts one web performance metric
// against an evaluated expected value (number, duration string, or
// matcher object). Metric is the canonical driver field name
// (e.g. "fcp_ms", "cls"); parser aliases have already been resolved.
type BrowserWebPerfExpectationExec struct {
	Metric   string
	Expected cty.Value
}

// BrowserVisibilityExec is a resolved visible / not_visible expectation.
type BrowserVisibilityExec struct {
	Selector string
	Timeout  time.Duration
	Interval time.Duration
}

// BrowserValueExpectationExec compares text/value content for an element.
type BrowserValueExpectationExec struct {
	Selector string
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}

// BrowserStateExpectationExec checks enabled/disabled state for an element.
type BrowserStateExpectationExec struct {
	Selector string
	Timeout  time.Duration
	Interval time.Duration
}

// BrowserAttributeExpectationExec matches a DOM attribute value.
type BrowserAttributeExpectationExec struct {
	Selector string
	Name     string
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}

// BrowserURLExpectationExec matches the document URL.
type BrowserURLExpectationExec struct {
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}

// BrowserTitleExpectationExec matches the document title.
type BrowserTitleExpectationExec struct {
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}
