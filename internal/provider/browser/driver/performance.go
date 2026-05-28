package driver

// PerformanceMetrics holds the web performance snapshot collected from a
// browsing context after the step actions have settled. Pointer fields
// (FCPMS, LCPMS, CLS) are nil when the browser did not surface the
// metric (for instance an LCP entry never fired on a simple page); the
// matcher layer reports those as "metric is not available" rather than
// returning zero.
type PerformanceMetrics struct {
	URL                  string
	Title                string
	DOMContentLoadedMS   float64
	LoadEventMS          float64
	FCPMS                *float64
	LCPMS                *float64
	CLS                  *float64
	ResourcesCount       int
	TransferSizeBytes    int64
	EncodedBodySizeBytes int64
	DecodedBodySizeBytes int64
}
