package runtime

import (
	"testing"

	browserprovider "github.com/tales-testing/tales/internal/provider/browser"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

func TestBrowserNamespaceExposesPerformance(t *testing.T) {
	t.Parallel()

	fcp := 812.5
	lcp := 1230.0
	cls := 0.02

	snap := &browserprovider.Snapshot{
		URL:   "https://example.com/web/dashboard",
		Title: "Dashboard",
		Performance: &driver.PerformanceMetrics{
			URL:                  "https://example.com/web/dashboard",
			Title:                "Dashboard",
			DOMContentLoadedMS:   1400,
			LoadEventMS:          1800,
			FCPMS:                &fcp,
			LCPMS:                &lcp,
			CLS:                  &cls,
			ResourcesCount:       42,
			TransferSizeBytes:    123456,
			EncodedBodySizeBytes: 100000,
			DecodedBodySizeBytes: 200000,
		},
	}

	ns := browserNamespaceValue(snap)

	if !ns.Type().IsObjectType() {
		t.Fatalf("expected object, got %s", ns.Type().FriendlyName())
	}

	perf := ns.GetAttr(keyPerformance)
	if !perf.Type().IsObjectType() {
		t.Fatalf("browser.performance should be object, got %s", perf.Type().FriendlyName())
	}

	if perf.GetAttr(keyURL).AsString() != "https://example.com/web/dashboard" {
		t.Fatalf("performance.url=%q", perf.GetAttr(keyURL).AsString())
	}

	fcpAttr := perf.GetAttr(keyFCPMS)
	if fcpAttr.IsNull() {
		t.Fatalf("fcp_ms should not be null when metric collected")
	}

	got, _ := fcpAttr.AsBigFloat().Float64()
	if got != 812.5 {
		t.Fatalf("fcp_ms=%v want 812.5", got)
	}

	resAttr := perf.GetAttr(keyResourcesCount)

	resCount, _ := resAttr.AsBigFloat().Float64()
	if resCount != 42 {
		t.Fatalf("resources_count=%v want 42", resCount)
	}
}

func TestBrowserNamespaceMissingPerformanceMetricsAreNull(t *testing.T) {
	t.Parallel()

	snap := &browserprovider.Snapshot{
		URL:   "https://example.com/",
		Title: "Home",
		Performance: &driver.PerformanceMetrics{
			URL:                "https://example.com/",
			Title:              "Home",
			DOMContentLoadedMS: 200,
			LoadEventMS:        500,
			// FCPMS, LCPMS, CLS intentionally nil — browser didn't surface them.
			ResourcesCount: 3,
		},
	}

	perf := browserNamespaceValue(snap).GetAttr(keyPerformance)

	for _, attr := range []string{keyFCPMS, keyLCPMS, keyCLS} {
		if !perf.GetAttr(attr).IsNull() {
			t.Fatalf("%s should be null when not available", attr)
		}
	}
}

func TestBrowserNamespaceWithNilSnapshot(t *testing.T) {
	t.Parallel()

	ns := browserNamespaceValue(nil)

	if ns.GetAttr(keyURL).AsString() != "" {
		t.Fatalf("expected empty url when snapshot nil")
	}

	perf := ns.GetAttr(keyPerformance)
	if !perf.GetAttr(keyFCPMS).IsNull() {
		t.Fatalf("expected null fcp_ms when snapshot nil")
	}
}
