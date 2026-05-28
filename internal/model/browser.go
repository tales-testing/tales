package model

// BrowserActionKind identifies the kind of UI action performed inside a
// browser step.
type BrowserActionKind string

const (
	// BrowserActionGoto navigates to a URL.
	BrowserActionGoto BrowserActionKind = "goto"
	// BrowserActionClick clicks an element identified by a CSS selector.
	BrowserActionClick BrowserActionKind = "click"
	// BrowserActionFill types text into a form field after clearing it.
	BrowserActionFill BrowserActionKind = "fill"
	// BrowserActionClear erases the current value of a form field.
	BrowserActionClear BrowserActionKind = "clear"
	// BrowserActionPress presses a keyboard key, optionally on an element.
	BrowserActionPress BrowserActionKind = "press"
	// BrowserActionSubmit submits a form element.
	BrowserActionSubmit BrowserActionKind = "submit"
	// BrowserActionScroll scrolls the page (selector or offset variant).
	BrowserActionScroll BrowserActionKind = "scroll"
	// BrowserActionWaitVisible waits until a selector becomes visible.
	BrowserActionWaitVisible BrowserActionKind = "wait_visible"
	// BrowserActionWaitNotVisible waits until a selector is missing or hidden.
	BrowserActionWaitNotVisible BrowserActionKind = "wait_not_visible"
	// BrowserActionHover hovers the mouse over an element.
	BrowserActionHover BrowserActionKind = "hover"
	// BrowserActionSelect selects an option in a <select> element by value.
	BrowserActionSelect BrowserActionKind = "select"
	// BrowserActionCheck checks a checkbox / radio input.
	BrowserActionCheck BrowserActionKind = "check"
	// BrowserActionUncheck unchecks a checkbox / radio input.
	BrowserActionUncheck BrowserActionKind = "uncheck"
	// BrowserActionReload reloads the current page.
	BrowserActionReload BrowserActionKind = "reload"
	// BrowserActionBack navigates back in the session history.
	BrowserActionBack BrowserActionKind = "back"
	// BrowserActionForward navigates forward in the session history.
	BrowserActionForward BrowserActionKind = "forward"
)

// BrowserStep is the provider-specific payload attached to a Step when
// Provider == "browser".
type BrowserStep struct {
	Target  Expression
	Actions []BrowserAction
	Expect  BrowserExpect
}

// BrowserAction is one ordered browser action inside an actions block.
type BrowserAction struct {
	Kind     BrowserActionKind
	File     string
	Line     int
	Selector Expression
	Value    Expression
	URL      Expression
	Key      Expression
	Secure   Expression
	Timeout  Expression
	Interval Expression
	X        Expression
	Y        Expression
}

// BrowserExpect groups assertions for a browser step. Each slice may be empty.
type BrowserExpect struct {
	Visible    []BrowserVisibility
	NotVisible []BrowserVisibility
	Text       []BrowserValueExpectation
	Value      []BrowserValueExpectation
	Enabled    []BrowserStateExpectation
	Disabled   []BrowserStateExpectation
	Attribute  []BrowserAttributeExpectation
	URL        []BrowserURLExpectation
	Title      []BrowserTitleExpectation
	WebPerf    []BrowserWebPerfExpectation
}

// BrowserWebPerfExpectation asserts a single web performance metric
// against an expected value (literal number / duration string or
// matcher object). Metric is the canonical snake_case name from the
// driver.PerformanceMetrics layer ("fcp_ms", "lcp_ms", "cls", …);
// surface aliases are resolved by the parser before this struct is
// built.
type BrowserWebPerfExpectation struct {
	Metric   string
	Expected Expression
}

// BrowserVisibility is one element visibility expectation with optional
// polling timeout.
type BrowserVisibility struct {
	Selector Expression
	Timeout  Expression
	Interval Expression
}

// BrowserValueExpectation matches an element's text content or input value
// against an expected value or matcher.
type BrowserValueExpectation struct {
	Selector Expression
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// BrowserStateExpectation asserts enabled / disabled state for an element.
type BrowserStateExpectation struct {
	Selector Expression
	Timeout  Expression
	Interval Expression
}

// BrowserAttributeExpectation matches a DOM attribute value.
type BrowserAttributeExpectation struct {
	Selector Expression
	Name     Expression
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// BrowserURLExpectation matches the document URL against a literal or matcher.
type BrowserURLExpectation struct {
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// BrowserTitleExpectation matches the document title against a literal or matcher.
type BrowserTitleExpectation struct {
	Expected Expression
	Timeout  Expression
	Interval Expression
}

// HasContent reports whether the browser step carries any operation worth
// executing.
func (b *BrowserStep) HasContent() bool {
	if b == nil {
		return false
	}

	if len(b.Actions) > 0 {
		return true
	}

	if len(b.Expect.Visible) > 0 || len(b.Expect.NotVisible) > 0 ||
		len(b.Expect.Text) > 0 || len(b.Expect.Value) > 0 ||
		len(b.Expect.Enabled) > 0 || len(b.Expect.Disabled) > 0 ||
		len(b.Expect.Attribute) > 0 || len(b.Expect.URL) > 0 ||
		len(b.Expect.Title) > 0 || len(b.Expect.WebPerf) > 0 {
		return true
	}

	return false
}
