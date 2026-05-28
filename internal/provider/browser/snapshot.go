package browser

import "github.com/tales-testing/tales/internal/provider/browser/driver"

// Snapshot is the post-step DOM snapshot the provider records so the
// runtime's capture helpers (text, attribute, browser.url, browser.title,
// browser.performance) can read consistent state without re-issuing CDP
// queries from the runtime layer.
type Snapshot struct {
	URL         string
	Title       string
	DOM         string
	Performance *driver.PerformanceMetrics
}
