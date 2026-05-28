package chrome

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// perfInjectScript is installed via Page.addScriptToEvaluateOnNewDocument
// before the first navigation so every page in the session collects
// LCP and CLS into a single window-level slot. PerformanceObserver
// "buffered" subscribers receive entries that already fired before the
// observer was attached, but only when the observer is registered while
// the same document is alive — hence the early injection. All observer
// work is wrapped in try/catch so old runtimes degrade silently.
const perfInjectScript = `
(function(){
  try {
    if (!window.__talesPerf) {
      window.__talesPerf = { lcp_ms: null, cls: 0 };
    }
    if (typeof PerformanceObserver !== "function") { return; }
    try {
      new PerformanceObserver(function(list){
        var entries = list.getEntries();
        if (entries.length > 0) {
          window.__talesPerf.lcp_ms = entries[entries.length - 1].startTime;
        }
      }).observe({ type: "largest-contentful-paint", buffered: true });
    } catch (e) {}
    try {
      new PerformanceObserver(function(list){
        var entries = list.getEntries();
        for (var i = 0; i < entries.length; i++) {
          var e = entries[i];
          if (!e.hadRecentInput) {
            window.__talesPerf.cls = (window.__talesPerf.cls || 0) + (e.value || 0);
          }
        }
      }).observe({ type: "layout-shift", buffered: true });
    } catch (e) {}
  } catch (e) {}
})();
`

// perfCollectScript runs in the page context every time the provider
// asks for a snapshot. It pulls navigation, paint, and resource
// summaries from the standard Performance API and combines them with
// the observer-accumulated values stashed on window.__talesPerf.
const perfCollectScript = `
(function(){
  var nav = (performance.getEntriesByType("navigation") || [])[0] || {};
  var paint = performance.getEntriesByType("paint") || [];
  var fcp = null;
  for (var i = 0; i < paint.length; i++) {
    if (paint[i].name === "first-contentful-paint") {
      fcp = paint[i].startTime;
      break;
    }
  }
  var resources = performance.getEntriesByType("resource") || [];
  var transfer = 0, encoded = 0, decoded = 0;
  for (var j = 0; j < resources.length; j++) {
    transfer += resources[j].transferSize || 0;
    encoded += resources[j].encodedBodySize || 0;
    decoded += resources[j].decodedBodySize || 0;
  }
  var perf = window.__talesPerf || {};
  return {
    url: location.href,
    title: document.title,
    dom_content_loaded_ms: nav.domContentLoadedEventEnd || 0,
    load_event_ms: nav.loadEventEnd || 0,
    fcp_ms: fcp,
    lcp_ms: (perf.lcp_ms === undefined ? null : perf.lcp_ms),
    cls: (perf.cls === undefined ? null : perf.cls),
    resources_count: resources.length,
    transfer_size_bytes: transfer,
    encoded_body_size_bytes: encoded,
    decoded_body_size_bytes: decoded
  };
})()
`

// installPerfObservers returns a chromedp action that registers
// perfInjectScript with CDP so every navigation in the session sees
// it. The Identifier returned by the CDP call is discarded — we never
// need to remove the script.
func installPerfObservers() chromedp.ActionFunc {
	return func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(perfInjectScript).Do(ctx)
		if err != nil {
			return fmt.Errorf("install perf observers: %w", err)
		}

		return nil
	}
}

// perfPayload mirrors the JSON shape returned by perfCollectScript. The
// observer-backed fields (FCP, LCP, CLS) are pointers so a JS null
// surfaces as a Go nil pointer and the matcher layer can report the
// metric as unavailable.
type perfPayload struct {
	URL              string   `json:"url"`
	Title            string   `json:"title"`
	DOMContentLoaded float64  `json:"dom_content_loaded_ms"`
	LoadEvent        float64  `json:"load_event_ms"`
	FCP              *float64 `json:"fcp_ms"`
	LCP              *float64 `json:"lcp_ms"`
	CLS              *float64 `json:"cls"`
	ResourcesCount   int      `json:"resources_count"`
	TransferSize     int64    `json:"transfer_size_bytes"`
	EncodedBodySize  int64    `json:"encoded_body_size_bytes"`
	DecodedBodySize  int64    `json:"decoded_body_size_bytes"`
}

// Performance implements driver.Driver. The collect script never throws,
// so a non-nil error here means CDP itself misbehaved.
func (d *chromedpDriver) Performance(ctx context.Context) (*driver.PerformanceMetrics, error) {
	var payload perfPayload

	if err := d.run(ctx, chromedp.Evaluate(perfCollectScript, &payload)); err != nil {
		return nil, fmt.Errorf("collect performance metrics: %w", err)
	}

	return &driver.PerformanceMetrics{
		URL:                  payload.URL,
		Title:                payload.Title,
		DOMContentLoadedMS:   payload.DOMContentLoaded,
		LoadEventMS:          payload.LoadEvent,
		FCPMS:                payload.FCP,
		LCPMS:                payload.LCP,
		CLS:                  payload.CLS,
		ResourcesCount:       payload.ResourcesCount,
		TransferSizeBytes:    payload.TransferSize,
		EncodedBodySizeBytes: payload.EncodedBodySize,
		DecodedBodySizeBytes: payload.DecodedBodySize,
	}, nil
}
