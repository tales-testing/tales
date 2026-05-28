package browser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/zclconf/go-cty/cty"
)

// expectDefaultTimeout is the timeout used when an expectation block omits it.
const expectDefaultTimeout = 10 * time.Second

// expectDefaultInterval is the poll interval used when an expectation block
// omits it.
const expectDefaultInterval = 250 * time.Millisecond

// handleExpect evaluates every expectation in order. The first failure
// short-circuits the loop and returns the typed error so the runner can
// report it.
//
//nolint:gocyclo // One branch per expectation kind keeps the dispatch flat; per-kind helpers exist only for the comparison primitives.
func (p *Provider) handleExpect(ctx context.Context, drv driver.Driver, expect provider.BrowserExpectExec) error {
	for _, v := range expect.Visible {
		if err := waitFor(ctx, v.Timeout, v.Interval, func(ctx context.Context) error {
			return drv.WaitVisible(ctx, v.Selector)
		}); err != nil {
			return fmt.Errorf("expect.visible %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.NotVisible {
		if err := waitFor(ctx, v.Timeout, v.Interval, func(ctx context.Context) error {
			return drv.WaitNotVisible(ctx, v.Selector)
		}); err != nil {
			return fmt.Errorf("expect.not_visible %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.Text {
		if err := matchText(ctx, drv, v.Selector, v.Expected, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.text %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.Value {
		if err := matchInputValue(ctx, drv, v.Selector, v.Expected, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.value %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.Enabled {
		if err := matchEnabled(ctx, drv, v.Selector, true, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.enabled %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.Disabled {
		if err := matchEnabled(ctx, drv, v.Selector, false, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.disabled %s: %w", v.Selector, err)
		}
	}

	for _, v := range expect.Attribute {
		if err := matchAttribute(ctx, drv, v.Selector, v.Name, v.Expected, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.attribute %s[%s]: %w", v.Selector, v.Name, err)
		}
	}

	for _, v := range expect.URL {
		if err := matchURL(ctx, drv, v.Expected, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.url: %w", err)
		}
	}

	for _, v := range expect.Title {
		if err := matchTitle(ctx, drv, v.Expected, v.Timeout, v.Interval); err != nil {
			return fmt.Errorf("expect.title: %w", err)
		}
	}

	if len(expect.WebPerf) > 0 {
		metrics, err := drv.Performance(ctx)
		if err != nil {
			return fmt.Errorf("expect.web_perf: collect performance: %w", err)
		}

		for _, v := range expect.WebPerf {
			if err := matchWebPerf(metrics, v); err != nil {
				return fmt.Errorf("expect.web_perf.%s: %w", v.Metric, err)
			}
		}
	}

	return nil
}

// matchWebPerf asserts a single performance metric against the
// expected value. Observer-backed metrics (FCP / LCP / CLS) that the
// browser did not surface are reported with a clear "not available"
// error rather than silently passing or failing.
func matchWebPerf(metrics *driver.PerformanceMetrics, exp provider.BrowserWebPerfExpectationExec) error {
	if metrics == nil {
		return fmt.Errorf("performance metrics unavailable")
	}

	value, ok := webPerfMetricValue(metrics, exp.Metric)
	if !ok {
		return fmt.Errorf("browser performance metric %q is not available", exp.Metric)
	}

	if err := assertion.MatchJSON(exp.Expected, value, false, exp.Metric); err != nil {
		return fmt.Errorf("metric mismatch: %w", err)
	}

	return nil
}

// webPerfMetricValue resolves a canonical metric name to its current
// cty value. ok=false when the metric is intrinsically optional and
// the browser did not surface a value (pointer fields nil).
func webPerfMetricValue(m *driver.PerformanceMetrics, metric string) (cty.Value, bool) {
	switch metric {
	case "dom_content_loaded_ms":
		return cty.NumberFloatVal(m.DOMContentLoadedMS), true
	case "load_event_ms":
		return cty.NumberFloatVal(m.LoadEventMS), true
	case "fcp_ms":
		if m.FCPMS == nil {
			return cty.NilVal, false
		}

		return cty.NumberFloatVal(*m.FCPMS), true
	case "lcp_ms":
		if m.LCPMS == nil {
			return cty.NilVal, false
		}

		return cty.NumberFloatVal(*m.LCPMS), true
	case "cls":
		if m.CLS == nil {
			return cty.NilVal, false
		}

		return cty.NumberFloatVal(*m.CLS), true
	case "resources_count":
		return cty.NumberIntVal(int64(m.ResourcesCount)), true
	case "transfer_size_bytes":
		return cty.NumberIntVal(m.TransferSizeBytes), true
	case "encoded_body_size_bytes":
		return cty.NumberIntVal(m.EncodedBodySizeBytes), true
	case "decoded_body_size_bytes":
		return cty.NumberIntVal(m.DecodedBodySizeBytes), true
	}

	return cty.NilVal, false
}

func matchText(ctx context.Context, drv driver.Driver, selector string, expected cty.Value, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		got, err := drv.Text(ctx, selector)
		if err != nil {
			return fmt.Errorf("read text: %w", err)
		}

		if err := assertion.MatchJSON(expected, cty.StringVal(got), false, selector); err != nil {
			return fmt.Errorf("text mismatch: %w", err)
		}

		return nil
	})
}

// matchInputValue reads the form `.value` property (input, textarea,
// select) rather than the rendered text — that is what `expect.value` is
// meant to assert against.
func matchInputValue(ctx context.Context, drv driver.Driver, selector string, expected cty.Value, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		got, err := drv.InputValue(ctx, selector)
		if err != nil {
			return fmt.Errorf("read value: %w", err)
		}

		if err := assertion.MatchJSON(expected, cty.StringVal(got), false, selector); err != nil {
			return fmt.Errorf("value mismatch: %w", err)
		}

		return nil
	})
}

func matchAttribute(ctx context.Context, drv driver.Driver, selector, name string, expected cty.Value, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		got, ok, err := drv.Attribute(ctx, selector, name)
		if err != nil {
			return fmt.Errorf("read attribute: %w", err)
		}

		if !ok {
			return fmt.Errorf("attribute %q not found", name)
		}

		if err := assertion.MatchJSON(expected, cty.StringVal(got), false, selector); err != nil {
			return fmt.Errorf("attribute mismatch: %w", err)
		}

		return nil
	})
}

func matchURL(ctx context.Context, drv driver.Driver, expected cty.Value, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		got, err := drv.URL(ctx)
		if err != nil {
			return fmt.Errorf("read url: %w", err)
		}

		if err := assertion.MatchJSON(expected, cty.StringVal(got), false, "url"); err != nil {
			return fmt.Errorf("url mismatch: %w", err)
		}

		return nil
	})
}

func matchTitle(ctx context.Context, drv driver.Driver, expected cty.Value, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		got, err := drv.Title(ctx)
		if err != nil {
			return fmt.Errorf("read title: %w", err)
		}

		if err := assertion.MatchJSON(expected, cty.StringVal(got), false, "title"); err != nil {
			return fmt.Errorf("title mismatch: %w", err)
		}

		return nil
	})
}

func matchEnabled(ctx context.Context, drv driver.Driver, selector string, wantEnabled bool, timeout, interval time.Duration) error {
	return pollUntilMatch(ctx, timeout, interval, func(ctx context.Context) error {
		_, ok, err := drv.Attribute(ctx, selector, "disabled")
		if err != nil {
			return fmt.Errorf("read disabled attribute: %w", err)
		}

		// HTML boolean-attribute semantics: the `disabled` attribute
		// makes the element disabled if it is present at all,
		// regardless of its value. `<input disabled>`, `<input
		// disabled="">`, and `<input disabled="false">` are all
		// disabled.
		isDisabled := ok
		if wantEnabled && isDisabled {
			return errors.New("element is disabled")
		}

		if !wantEnabled && !isDisabled {
			return errors.New("element is not disabled")
		}

		return nil
	})
}

// pollUntilMatch repeatedly evaluates fn until it returns nil or the budget
// is exhausted. The last error is returned on timeout.
func pollUntilMatch(parent context.Context, timeout, interval time.Duration, fn func(context.Context) error) error {
	if timeout <= 0 {
		timeout = expectDefaultTimeout
	}

	if interval <= 0 {
		interval = expectDefaultInterval
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var lastErr error

	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(interval):
		}
	}
}

// waitFor is a shortcut for poll-style driver primitives (WaitVisible /
// WaitNotVisible) that already return when ready. We still wrap with a
// timeout so a misbehaving driver cannot stall forever.
func waitFor(parent context.Context, timeout, _ time.Duration, fn func(context.Context) error) error {
	if timeout <= 0 {
		timeout = expectDefaultTimeout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	return fn(ctx)
}
