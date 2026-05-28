package report

import (
	"fmt"
	"io"
)

// printLoadSummary writes a concise benchmark recap right after the
// step header line so a load run carries its own readable digest
// without rewriting the regular request/response dump format. It
// degrades to a no-op when the expected response keys are missing
// (e.g. the load step failed before measurements were collected).
func printLoadSummary(out io.Writer, step *StepResult) error {
	if step == nil || step.Response == nil {
		return nil
	}

	requests, hasReq := numericField(step.Response, "requests")
	if !hasReq {
		return nil
	}

	errors, _ := numericField(step.Response, "errors")
	rps, _ := numericField(step.Response, "rps")
	errorRatio, _ := numericField(step.Response, "error_ratio")

	if _, err := fmt.Fprintf(out, "    requests: %s\n", formatInt(requests)); err != nil {
		return fmt.Errorf("print load requests: %w", err)
	}

	if _, err := fmt.Fprintf(out, "    rps: %.1f\n", rps); err != nil {
		return fmt.Errorf("print load rps: %w", err)
	}

	if _, err := fmt.Fprintf(out, "    errors: %s (%.2f%%)\n", formatInt(errors), errorRatio*100); err != nil {
		return fmt.Errorf("print load errors: %w", err)
	}

	if err := printLoadStatusBreakdown(out, step.Response); err != nil {
		return err
	}

	if err := printLoadLatencyLine(out, step.Response); err != nil {
		return err
	}

	return nil
}

func printLoadStatusBreakdown(out io.Writer, response map[string]interface{}) error {
	status, ok := response["status"].(map[string]interface{})
	if !ok {
		return nil
	}

	s1xx, _ := numericField(status, "1xx")
	s2xx, _ := numericField(status, "2xx")
	s3xx, _ := numericField(status, "3xx")
	s4xx, _ := numericField(status, "4xx")
	s5xx, _ := numericField(status, "5xx")

	if _, err := fmt.Fprintf(out, "    status: 1xx=%s 2xx=%s 3xx=%s 4xx=%s 5xx=%s\n",
		formatInt(s1xx), formatInt(s2xx), formatInt(s3xx), formatInt(s4xx), formatInt(s5xx)); err != nil {
		return fmt.Errorf("print load status: %w", err)
	}

	return nil
}

func printLoadLatencyLine(out io.Writer, response map[string]interface{}) error {
	latency, ok := response["latency"].(map[string]interface{})
	if !ok {
		return nil
	}

	p50, _ := numericField(latency, "p50_ms")
	p95, _ := numericField(latency, "p95_ms")
	p99, _ := numericField(latency, "p99_ms")
	maxMS, _ := numericField(latency, "max_ms")

	if _, err := fmt.Fprintf(out, "    latency: p50=%.1fms p95=%.1fms p99=%.1fms max=%.1fms\n",
		p50, p95, p99, maxMS); err != nil {
		return fmt.Errorf("print load latency: %w", err)
	}

	return nil
}

// numericField pulls a numeric field out of a JSON-shaped map. Values
// are stored as float64 by the cty -> Go conversion in
// diagnostic.FromCTYMap. Returns ok=false when missing or non-numeric.
func numericField(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}

	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	case int64:
		return float64(f), true
	}

	return 0, false
}

func formatInt(f float64) string {
	return fmt.Sprintf("%d", int64(f))
}
