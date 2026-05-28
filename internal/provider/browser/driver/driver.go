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
	// Click waits for the selector to be visible, then clicks it.
	Click(ctx context.Context, selector string) error
	// Fill waits for the selector, clears the input, then types value.
	Fill(ctx context.Context, selector, value string) error
	// Clear waits for the selector, then erases the input value.
	Clear(ctx context.Context, selector string) error
	// Press focuses the selector (when non-empty) and presses key.
	Press(ctx context.Context, selector, key string) error
	// Submit triggers form submission on the given form selector.
	Submit(ctx context.Context, selector string) error
	// Hover moves the mouse over the selector.
	Hover(ctx context.Context, selector string) error
	// Check toggles a checkbox / radio input on.
	Check(ctx context.Context, selector string) error
	// Uncheck toggles a checkbox / radio input off.
	Uncheck(ctx context.Context, selector string) error
	// SelectOption selects a value on a <select> element.
	SelectOption(ctx context.Context, selector, value string) error
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
	// Close releases the browsing context's resources.
	Close(ctx context.Context) error
}
