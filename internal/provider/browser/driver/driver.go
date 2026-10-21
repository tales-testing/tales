// Package driver defines the browser-driver abstraction used by the Tales
// browser provider. The real implementation drives Chrome via the Chrome
// DevTools Protocol (chromedp). Tests use the NoopDriver embedding base to
// build small in-memory fakes per test case.
package driver

import "context"

// Driver is the transport-agnostic interface the browser provider talks to.
// One Driver instance is bound to a single browsing context — e.g., a
// chromedp.Context derived from the per-target allocator and scoped to one
// scenario for cookie / storage isolation.
type Driver interface {
	// Goto navigates to the given URL and waits for the document to settle.
	Goto(ctx context.Context, url string) error
	// Click waits until the selector is actionable (see PointerOptions),
	// then moves the mouse to it, presses and releases the left button.
	Click(ctx context.Context, selector string, opts PointerOptions) (PointerResult, error)
	// Fill waits for the selector, clears the input, then types value.
	Fill(ctx context.Context, selector, value string) error
	// Clear waits for the selector, then erases the input value.
	Clear(ctx context.Context, selector string) error
	// Press focuses the selector (when non-empty) and presses key, which is
	// a single character or one of model.BrowserKeyNames. Any other string
	// is an error: it must never be typed as text.
	Press(ctx context.Context, selector, key string) error
	// Submit triggers form submission on the given form selector.
	Submit(ctx context.Context, selector string) error
	// Hover waits until the selector is actionable (enabled or not),
	// then moves the mouse over it.
	Hover(ctx context.Context, selector string, opts PointerOptions) (PointerResult, error)
	// Check clicks a checkbox / radio input that is not checked yet, as
	// Click does, then verifies that it is.
	Check(ctx context.Context, selector string, opts PointerOptions) (PointerResult, error)
	// Uncheck clicks a checked checkbox, as Click does, then verifies
	// that it is no longer checked.
	Uncheck(ctx context.Context, selector string, opts PointerOptions) (PointerResult, error)
	// SelectOption selects a value on a <select> element.
	SelectOption(ctx context.Context, selector, value string) error
	// SetFileInputs sets the files of an <input type="file"> element. paths
	// must be absolute and already validated by the caller. The element's
	// value is read-only from the DOM, so this goes through CDP rather than
	// through Fill.
	SetFileInputs(ctx context.Context, selector string, paths []string) error
	// ScrollIntoView scrolls the matching element into view.
	ScrollIntoView(ctx context.Context, selector string) error
	// ScrollBy scrolls the viewport by x / y pixels.
	ScrollBy(ctx context.Context, x, y int) error
	// Reload reloads the current page.
	Reload(ctx context.Context) error
	// Back navigates back in the session history.
	Back(ctx context.Context) error
	// Forward navigates forward in the session history.
	Forward(ctx context.Context) error
	// WaitVisible polls until selector is visible.
	WaitVisible(ctx context.Context, selector string) error
	// WaitNotVisible polls until selector is gone or hidden.
	WaitNotVisible(ctx context.Context, selector string) error
	// Visible reports the current visibility of selector.
	Visible(ctx context.Context, selector string) (bool, error)
	// Text returns the rendered text content of selector.
	Text(ctx context.Context, selector string) (string, error)
	// InputValue returns the JS .value of selector, used to match form
	// inputs / textareas / selects whose current value is independent
	// of the rendered text.
	InputValue(ctx context.Context, selector string) (string, error)
	// Attribute returns the value of an HTML attribute on selector.
	Attribute(ctx context.Context, selector, name string) (string, bool, error)
	// URL returns the current document URL.
	URL(ctx context.Context) (string, error)
	// Title returns the current document title.
	Title(ctx context.Context) (string, error)
	// OuterHTML returns the outerHTML of selector (typically "html").
	OuterHTML(ctx context.Context, selector string) (string, error)
	// Screenshot captures the viewport as PNG bytes.
	Screenshot(ctx context.Context) ([]byte, error)
	// Performance returns a snapshot of web performance metrics (FCP,
	// LCP, CLS, navigation timings, resource summary) for the current
	// document. The implementation may return (nil, nil) when no
	// metrics are available — callers should treat that the same as a
	// successful collect of an empty snapshot.
	Performance(ctx context.Context) (*PerformanceMetrics, error)
	// StartDiagnostics starts recording the page's console messages,
	// uncaught exceptions and network events, discarding what an earlier
	// recording kept. The recording goes on until the next
	// StartDiagnostics or Close, bounded in memory (see Recorder), and
	// never keeps header values, cookies, bodies or URL query strings.
	StartDiagnostics(ctx context.Context) error
	// Diagnostics returns a copy of what was recorded since the last
	// StartDiagnostics, or nil when nothing is being recorded. It reads
	// memory only, so it answers even when the page does not.
	Diagnostics() *Diagnostics
	// PageState reads what has the focus, the viewport size, the scroll
	// offsets and the document URL. (nil, nil) means the driver cannot
	// tell.
	PageState(ctx context.Context) (*PageState, error)
	// Close releases the browsing context's resources.
	Close(ctx context.Context) error
}
