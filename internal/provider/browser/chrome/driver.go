package chrome

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// chromedpDriver is the production driver.Driver implementation backed by
// chromedp. It is bound to a single browsing context — a chromedp.Context
// derived from the per-target allocator and scoped to one scenario for
// cookie / storage isolation.
type chromedpDriver struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewDriver wraps an existing chromedp.Context so the browser provider can
// drive it through the abstract Driver interface. cancel must be called by
// the caller when the scenario context tears down.
func NewDriver(ctx context.Context, cancel context.CancelFunc) *chromedpDriver {
	return &chromedpDriver{ctx: ctx, cancel: cancel}
}

// run executes one or more chromedp actions against the bound context. The
// caller's ctx wins over the bound context (so per-action timeouts apply)
// while still inheriting the chromedp browser context via the parent
// chain — chromedp.Context is itself a context.Context, so the action is
// associated with the right browser through the deadline-shadowing parent.
func (d *chromedpDriver) run(ctx context.Context, actions ...chromedp.Action) error {
	// chromedp.FromContext locates the browser context registered on d.ctx;
	// it walks the parent chain, so wrapping the call site's ctx with d.ctx
	// keeps both the per-action deadline and the browser binding.
	cdpCtx, cancelMerge := mergeContext(ctx, d.ctx)
	defer cancelMerge()

	return chromedp.Run(cdpCtx, actions...) //nolint:wrapcheck // chromedp.Run returns a CDP-specific error chain; the caller wraps with action context.
}

// Goto implements driver.Driver.
func (d *chromedpDriver) Goto(ctx context.Context, url string) error {
	return d.run(ctx, chromedp.Navigate(url))
}

// Click implements driver.Driver.
func (d *chromedpDriver) Click(ctx context.Context, selector string) error {
	return d.run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	)
}

// Fill implements driver.Driver.
func (d *chromedpDriver) Fill(ctx context.Context, selector, value string) error {
	return d.run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Focus(selector, chromedp.ByQuery),
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, value, chromedp.ByQuery),
	)
}

// Clear implements driver.Driver.
func (d *chromedpDriver) Clear(ctx context.Context, selector string) error {
	return d.run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Clear(selector, chromedp.ByQuery),
	)
}

// Press implements driver.Driver. When selector is set the element is
// focused first; otherwise the keystroke goes to whichever element has the
// focus.
func (d *chromedpDriver) Press(ctx context.Context, selector, key string) error {
	if selector == "" {
		return d.run(ctx, chromedp.KeyEvent(key))
	}

	return d.run(ctx,
		chromedp.Focus(selector, chromedp.ByQuery),
		chromedp.KeyEvent(key),
	)
}

// Submit implements driver.Driver via HTMLFormElement.requestSubmit() with
// a fallback to .submit() for older surfaces.
func (d *chromedpDriver) Submit(ctx context.Context, selector string) error {
	script := fmt.Sprintf(`
		(function(){
			var el = document.querySelector(%q);
			if (!el) throw new Error("form not found: " + %q);
			if (typeof el.requestSubmit === "function") { el.requestSubmit(); return true; }
			el.submit(); return true;
		})()
	`, selector, selector)

	var ok bool

	return d.run(ctx, chromedp.Evaluate(script, &ok))
}

// Hover implements driver.Driver via the high-level chromedp helper.
func (d *chromedpDriver) Hover(ctx context.Context, selector string) error {
	return d.run(ctx, chromedp.Evaluate(fmt.Sprintf(`
		(function(){
			var el = document.querySelector(%q);
			if (!el) throw new Error("element not found: " + %q);
			var rect = el.getBoundingClientRect();
			var ev = new MouseEvent("mouseover", { bubbles: true, clientX: rect.left+1, clientY: rect.top+1 });
			el.dispatchEvent(ev);
		})()
	`, selector, selector), nil))
}

// Check implements driver.Driver: sets .checked = true and dispatches a
// change event.
func (d *chromedpDriver) Check(ctx context.Context, selector string) error {
	return d.setChecked(ctx, selector, true)
}

// Uncheck implements driver.Driver.
func (d *chromedpDriver) Uncheck(ctx context.Context, selector string) error {
	return d.setChecked(ctx, selector, false)
}

func (d *chromedpDriver) setChecked(ctx context.Context, selector string, on bool) error {
	script := fmt.Sprintf(`
		(function(){
			var el = document.querySelector(%q);
			if (!el) throw new Error("element not found: " + %q);
			el.checked = %t;
			el.dispatchEvent(new Event("change", { bubbles: true }));
		})()
	`, selector, selector, on)

	return d.run(ctx, chromedp.Evaluate(script, nil))
}

// SelectOption implements driver.Driver by setting the <select> value and
// dispatching change.
func (d *chromedpDriver) SelectOption(ctx context.Context, selector, value string) error {
	script := fmt.Sprintf(`
		(function(){
			var el = document.querySelector(%q);
			if (!el) throw new Error("element not found: " + %q);
			el.value = %q;
			el.dispatchEvent(new Event("change", { bubbles: true }));
		})()
	`, selector, selector, value)

	return d.run(ctx, chromedp.Evaluate(script, nil))
}

// ScrollIntoView implements driver.Driver.
func (d *chromedpDriver) ScrollIntoView(ctx context.Context, selector string) error {
	return d.run(ctx, chromedp.ScrollIntoView(selector, chromedp.ByQuery))
}

// ScrollBy implements driver.Driver.
func (d *chromedpDriver) ScrollBy(ctx context.Context, x, y int) error {
	script := fmt.Sprintf("window.scrollBy({left: %d, top: %d, behavior: 'instant'})", x, y)

	return d.run(ctx, chromedp.Evaluate(script, nil))
}

// Reload implements driver.Driver.
func (d *chromedpDriver) Reload(ctx context.Context) error {
	return d.run(ctx, chromedp.Reload())
}

// Back implements driver.Driver.
func (d *chromedpDriver) Back(ctx context.Context) error {
	return d.run(ctx, chromedp.NavigateBack())
}

// Forward implements driver.Driver.
func (d *chromedpDriver) Forward(ctx context.Context) error {
	return d.run(ctx, chromedp.NavigateForward())
}

// WaitVisible implements driver.Driver.
func (d *chromedpDriver) WaitVisible(ctx context.Context, selector string) error {
	return d.run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery))
}

// WaitNotVisible implements driver.Driver via chromedp.WaitNotPresent.
func (d *chromedpDriver) WaitNotVisible(ctx context.Context, selector string) error {
	return d.run(ctx, chromedp.WaitNotPresent(selector, chromedp.ByQuery))
}

// Visible implements driver.Driver.
func (d *chromedpDriver) Visible(ctx context.Context, selector string) (bool, error) {
	var nodes []*cdp.Node

	err := d.run(ctx, chromedp.Nodes(selector, &nodes, chromedp.AtLeast(0), chromedp.ByQueryAll))
	if err != nil {
		return false, err
	}

	return len(nodes) > 0, nil
}

// Text implements driver.Driver.
func (d *chromedpDriver) Text(ctx context.Context, selector string) (string, error) {
	var out string

	if err := d.run(ctx, chromedp.Text(selector, &out, chromedp.ByQuery)); err != nil {
		return "", err
	}

	return out, nil
}

// Attribute implements driver.Driver.
func (d *chromedpDriver) Attribute(ctx context.Context, selector, name string) (string, bool, error) {
	var (
		value string
		ok    bool
	)

	err := d.run(ctx, chromedp.AttributeValue(selector, name, &value, &ok, chromedp.ByQuery))
	if err != nil {
		return "", false, err
	}

	return value, ok, nil
}

// URL implements driver.Driver.
func (d *chromedpDriver) URL(ctx context.Context) (string, error) {
	var out string

	if err := d.run(ctx, chromedp.Location(&out)); err != nil {
		return "", err
	}

	return out, nil
}

// Title implements driver.Driver.
func (d *chromedpDriver) Title(ctx context.Context) (string, error) {
	var out string

	if err := d.run(ctx, chromedp.Title(&out)); err != nil {
		return "", err
	}

	return out, nil
}

// OuterHTML implements driver.Driver.
func (d *chromedpDriver) OuterHTML(ctx context.Context, selector string) (string, error) {
	var out string

	if err := d.run(ctx, chromedp.OuterHTML(selector, &out, chromedp.ByQuery)); err != nil {
		return "", err
	}

	return out, nil
}

// Screenshot implements driver.Driver.
func (d *chromedpDriver) Screenshot(ctx context.Context) ([]byte, error) {
	var out []byte

	if err := d.run(ctx, chromedp.CaptureScreenshot(&out)); err != nil {
		return nil, err
	}

	return out, nil
}

// Close implements driver.Driver. Cancels the bound chromedp.Context,
// releasing the browser page resources. Idempotent.
func (d *chromedpDriver) Close(_ context.Context) error {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}

	return nil
}
