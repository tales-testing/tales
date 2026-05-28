// Package load implements a Go-native HTTP load benchmark provider.
// It replays one request concurrently for a duration or request count
// and reports latency percentiles, error ratio, and status-class
// ratios so users can pin smoke performance budgets in their suites.
package load

import (
	"math"
	"slices"
	"time"
)

// LatencySummary aggregates per-request latencies into the percentiles
// surfaced to assertions. All fields are milliseconds.
type LatencySummary struct {
	MinMS  float64
	MaxMS  float64
	MeanMS float64
	P50MS  float64
	P90MS  float64
	P95MS  float64
	P99MS  float64
}

// computeLatency computes the percentile summary from raw durations.
// Percentiles use the nearest-rank method documented in the load
// provider reference. Empty input yields an all-zero summary; the
// runner is responsible for flagging "no samples collected" upstream.
func computeLatency(samples []time.Duration) LatencySummary {
	if len(samples) == 0 {
		return LatencySummary{}
	}

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	slices.Sort(sorted)

	toMS := func(d time.Duration) float64 {
		return float64(d) / float64(time.Millisecond)
	}

	var total time.Duration
	for _, s := range sorted {
		total += s
	}

	return LatencySummary{
		MinMS:  toMS(sorted[0]),
		MaxMS:  toMS(sorted[len(sorted)-1]),
		MeanMS: toMS(total) / float64(len(sorted)),
		P50MS:  toMS(percentile(sorted, 50)),
		P90MS:  toMS(percentile(sorted, 90)),
		P95MS:  toMS(percentile(sorted, 95)),
		P99MS:  toMS(percentile(sorted, 99)),
	}
}

// percentile returns the nearest-rank percentile from a sorted slice.
// rank = ceil(p/100 * n); the returned value is sorted[max(rank-1, 0)].
// For n == 1 every percentile collapses to the single sample. For
// p <= 0 the function returns the minimum; for p >= 100 the maximum.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	if p <= 0 {
		return sorted[0]
	}

	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	idx := max(rank-1, 0)

	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}
