package browser

import (
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/zclconf/go-cty/cty"
)

func ltMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"__tales_matcher": cty.StringVal("lt"),
		"value":           threshold,
	})
}

func gteMatcher(threshold cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"__tales_matcher": cty.StringVal("gte"),
		"value":           threshold,
	})
}

func TestMatchWebPerfPassFail(t *testing.T) {
	t.Parallel()

	fcp := 812.0

	metrics := &driver.PerformanceMetrics{
		DOMContentLoadedMS: 1200,
		LoadEventMS:        1800,
		FCPMS:              &fcp,
		ResourcesCount:     5,
	}

	if err := matchWebPerf(metrics, provider.BrowserWebPerfExpectationExec{
		Metric:   "fcp_ms",
		Expected: ltMatcher(cty.StringVal("1800ms")),
	}); err != nil {
		t.Fatalf("fcp 812 should be < 1800ms: %v", err)
	}

	err := matchWebPerf(metrics, provider.BrowserWebPerfExpectationExec{
		Metric:   "fcp_ms",
		Expected: ltMatcher(cty.StringVal("500ms")),
	})
	if err == nil {
		t.Fatalf("fcp 812 should fail < 500ms")
	}
}

func TestMatchWebPerfUnavailableMetricReportsClearly(t *testing.T) {
	t.Parallel()

	metrics := &driver.PerformanceMetrics{
		DOMContentLoadedMS: 1000,
		LoadEventMS:        2000,
		// LCP / CLS unset.
	}

	err := matchWebPerf(metrics, provider.BrowserWebPerfExpectationExec{
		Metric:   "lcp_ms",
		Expected: ltMatcher(cty.StringVal("2500ms")),
	})
	if err == nil {
		t.Fatalf("expected unavailable metric error")
	}

	if !strings.Contains(err.Error(), `metric "lcp_ms" is not available`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchWebPerfResourcesCountWithGte(t *testing.T) {
	t.Parallel()

	metrics := &driver.PerformanceMetrics{
		ResourcesCount: 7,
	}

	if err := matchWebPerf(metrics, provider.BrowserWebPerfExpectationExec{
		Metric:   "resources_count",
		Expected: gteMatcher(cty.NumberIntVal(1)),
	}); err != nil {
		t.Fatalf("resources_count 7 should be >= 1: %v", err)
	}
}

func TestMatchWebPerfNilMetricsError(t *testing.T) {
	t.Parallel()

	err := matchWebPerf(nil, provider.BrowserWebPerfExpectationExec{
		Metric:   "fcp_ms",
		Expected: ltMatcher(cty.StringVal("1s")),
	})
	if err == nil {
		t.Fatalf("expected error on nil metrics")
	}
}
