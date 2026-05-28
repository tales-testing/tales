package driver

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// FakeDriver is an in-memory Driver used by unit tests. It records every
// call, lets each test wire selector-keyed return values, and exposes the
// recorded call list for assertions.
//
// Methods that aren't explicitly programmed return the NoopDriver defaults
// (zero value, nil error) — tests override only what they exercise.
type FakeDriver struct {
	NoopDriver

	mu sync.Mutex

	Calls           []Call
	Visibility      map[string]bool   // selector -> visible
	Texts           map[string]string // selector -> rendered text
	InputValues     map[string]string // selector -> form .value
	Attributes      map[string]map[string]string
	OuterHTMLValue  string
	URLValue        string
	TitleValue      string
	ScreenshotPNG   []byte
	FailOnSelector  map[string]error // make a specific click/fill/wait fail
	FailFirstClick  bool
	HasClickedFirst bool
}

// Call is one recorded driver call.
type Call struct {
	Method   string
	Selector string
	Value    string
	URL      string
	Key      string
	X        int
	Y        int
}

func newFake() *FakeDriver {
	return &FakeDriver{
		Visibility:  map[string]bool{},
		Texts:       map[string]string{},
		InputValues: map[string]string{},
		Attributes:  map[string]map[string]string{},
	}
}

// NewFakeDriver builds an empty FakeDriver.
func NewFakeDriver() *FakeDriver { return newFake() }

func (f *FakeDriver) record(call Call) {
	f.mu.Lock()
	f.Calls = append(f.Calls, call)
	f.mu.Unlock()
}

func (f *FakeDriver) failure(method, selector string) error {
	if err, ok := f.FailOnSelector[selector]; ok {
		return err
	}

	if method == methodClick && f.FailFirstClick && !f.HasClickedFirst {
		f.HasClickedFirst = true

		return errors.New("fake driver: forced first-click failure")
	}

	return nil
}

// Goto records the call and returns the configured error (if any).
func (f *FakeDriver) Goto(_ context.Context, url string) error {
	f.record(Call{Method: "Goto", URL: url})

	if err, ok := f.FailOnSelector[url]; ok {
		return err
	}

	return nil
}

const methodClick = "Click"

// Click records the call and returns the configured error (if any).
func (f *FakeDriver) Click(_ context.Context, selector string) error {
	f.record(Call{Method: methodClick, Selector: selector})

	return f.failure(methodClick, selector)
}

// Fill records the call and returns the configured error (if any).
func (f *FakeDriver) Fill(_ context.Context, selector, value string) error {
	f.record(Call{Method: "Fill", Selector: selector, Value: value})

	return f.failure("Fill", selector)
}

// Clear records the call.
func (f *FakeDriver) Clear(_ context.Context, selector string) error {
	f.record(Call{Method: "Clear", Selector: selector})

	return f.failure("Clear", selector)
}

// Press records the call.
func (f *FakeDriver) Press(_ context.Context, selector, key string) error {
	f.record(Call{Method: "Press", Selector: selector, Key: key})

	return f.failure("Press", selector)
}

// Submit records the call.
func (f *FakeDriver) Submit(_ context.Context, selector string) error {
	f.record(Call{Method: "Submit", Selector: selector})

	return f.failure("Submit", selector)
}

// Hover records the call.
func (f *FakeDriver) Hover(_ context.Context, selector string) error {
	f.record(Call{Method: "Hover", Selector: selector})

	return f.failure("Hover", selector)
}

// Check records the call.
func (f *FakeDriver) Check(_ context.Context, selector string) error {
	f.record(Call{Method: "Check", Selector: selector})

	return f.failure("Check", selector)
}

// Uncheck records the call.
func (f *FakeDriver) Uncheck(_ context.Context, selector string) error {
	f.record(Call{Method: "Uncheck", Selector: selector})

	return f.failure("Uncheck", selector)
}

// SelectOption records the call.
func (f *FakeDriver) SelectOption(_ context.Context, selector, value string) error {
	f.record(Call{Method: "SelectOption", Selector: selector, Value: value})

	return f.failure("SelectOption", selector)
}

// ScrollIntoView records the call.
func (f *FakeDriver) ScrollIntoView(_ context.Context, selector string) error {
	f.record(Call{Method: "ScrollIntoView", Selector: selector})

	return f.failure("ScrollIntoView", selector)
}

// ScrollBy records the call.
func (f *FakeDriver) ScrollBy(_ context.Context, x, y int) error {
	f.record(Call{Method: "ScrollBy", X: x, Y: y})

	return nil
}

// Reload records the call.
func (f *FakeDriver) Reload(_ context.Context) error {
	f.record(Call{Method: "Reload"})

	return nil
}

// Back records the call.
func (f *FakeDriver) Back(_ context.Context) error {
	f.record(Call{Method: "Back"})

	return nil
}

// Forward records the call.
func (f *FakeDriver) Forward(_ context.Context) error {
	f.record(Call{Method: "Forward"})

	return nil
}

// WaitVisible polls Visibility for the selector. Returns an error when the
// selector is not configured visible.
func (f *FakeDriver) WaitVisible(_ context.Context, selector string) error {
	f.record(Call{Method: "WaitVisible", Selector: selector})

	if err, ok := f.FailOnSelector[selector]; ok {
		return err
	}

	if !f.Visibility[selector] {
		return errors.New("selector " + selector + " not visible")
	}

	return nil
}

// WaitNotVisible records and inverts Visibility.
func (f *FakeDriver) WaitNotVisible(_ context.Context, selector string) error {
	f.record(Call{Method: "WaitNotVisible", Selector: selector})

	if f.Visibility[selector] {
		return errors.New("selector " + selector + " still visible")
	}

	return nil
}

// Visible reports the configured visibility for selector.
func (f *FakeDriver) Visible(_ context.Context, selector string) (bool, error) {
	f.record(Call{Method: "Visible", Selector: selector})

	return f.Visibility[selector], nil
}

// Text returns the configured text for selector, or "" if not set.
func (f *FakeDriver) Text(_ context.Context, selector string) (string, error) {
	f.record(Call{Method: "Text", Selector: selector})

	return f.Texts[selector], nil
}

// InputValue returns the configured form .value for selector, or "" if not set.
func (f *FakeDriver) InputValue(_ context.Context, selector string) (string, error) {
	f.record(Call{Method: "InputValue", Selector: selector})

	return f.InputValues[selector], nil
}

// Attribute returns the named attribute on selector, or ("", false) when
// not configured.
func (f *FakeDriver) Attribute(_ context.Context, selector, name string) (string, bool, error) {
	f.record(Call{Method: "Attribute", Selector: selector, Value: name})

	attrs := f.Attributes[selector]
	if attrs == nil {
		return "", false, nil
	}

	v, ok := attrs[name]

	return v, ok, nil
}

// URL returns the configured URL value.
func (f *FakeDriver) URL(_ context.Context) (string, error) { return f.URLValue, nil }

// Title returns the configured title value.
func (f *FakeDriver) Title(_ context.Context) (string, error) { return f.TitleValue, nil }

// OuterHTML returns the configured outerHTML value. The selector is
// ignored — tests typically only ask for "html".
func (f *FakeDriver) OuterHTML(_ context.Context, selector string) (string, error) {
	f.record(Call{Method: "OuterHTML", Selector: selector})

	return f.OuterHTMLValue, nil
}

// Screenshot returns the configured PNG bytes.
func (f *FakeDriver) Screenshot(_ context.Context) ([]byte, error) {
	f.record(Call{Method: "Screenshot"})

	return f.ScreenshotPNG, nil
}

// Close records the call.
func (f *FakeDriver) Close(_ context.Context) error {
	f.record(Call{Method: "Close"})

	return nil
}

// MethodsCalled returns the sequence of Call.Method values for assertions.
func (f *FakeDriver) MethodsCalled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, 0, len(f.Calls))
	for _, c := range f.Calls {
		out = append(out, c.Method)
	}

	return out
}

// CallsJoined returns a human-friendly summary used in test error messages.
func (f *FakeDriver) CallsJoined() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	parts := make([]string, 0, len(f.Calls))
	for _, c := range f.Calls {
		parts = append(parts, c.Method)
	}

	return strings.Join(parts, ",")
}
