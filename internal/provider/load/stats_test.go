package load

import (
	"testing"
	"time"
)

func TestComputeLatencyEmpty(t *testing.T) {
	t.Parallel()

	got := computeLatency(nil)
	if got != (LatencySummary{}) {
		t.Fatalf("empty input should yield zero summary, got %+v", got)
	}
}

func TestComputeLatencySingleSample(t *testing.T) {
	t.Parallel()

	sample := 42 * time.Millisecond
	got := computeLatency([]time.Duration{sample})

	if got.MinMS != 42 || got.MaxMS != 42 || got.MeanMS != 42 {
		t.Fatalf("min/max/mean mismatch: %+v", got)
	}

	if got.P50MS != 42 || got.P95MS != 42 || got.P99MS != 42 {
		t.Fatalf("percentiles should collapse to sample, got %+v", got)
	}
}

func TestComputeLatencyKnownPercentiles(t *testing.T) {
	t.Parallel()

	samples := make([]time.Duration, 100)
	for i := range samples {
		samples[i] = time.Duration(i+1) * time.Millisecond
	}

	got := computeLatency(samples)

	if got.MinMS != 1 {
		t.Fatalf("min=%v want 1", got.MinMS)
	}

	if got.MaxMS != 100 {
		t.Fatalf("max=%v want 100", got.MaxMS)
	}

	// Nearest-rank on 100 ascending samples: p50=ceil(50)=50,
	// p95=95, p99=99.
	if got.P50MS != 50 {
		t.Fatalf("p50=%v want 50", got.P50MS)
	}

	if got.P95MS != 95 {
		t.Fatalf("p95=%v want 95", got.P95MS)
	}

	if got.P99MS != 99 {
		t.Fatalf("p99=%v want 99", got.P99MS)
	}

	if got.MeanMS != 50.5 {
		t.Fatalf("mean=%v want 50.5", got.MeanMS)
	}
}

func TestPercentileBoundaryRanks(t *testing.T) {
	t.Parallel()

	samples := []time.Duration{10, 20, 30, 40, 50}
	if got := percentile(samples, 0); got != 10 {
		t.Fatalf("p0=%v want 10", got)
	}

	if got := percentile(samples, 100); got != 50 {
		t.Fatalf("p100=%v want 50", got)
	}

	// rank = ceil(20/100 * 5) = 1 → idx 0
	if got := percentile(samples, 20); got != 10 {
		t.Fatalf("p20=%v want 10", got)
	}

	// rank = ceil(40/100 * 5) = 2 → idx 1
	if got := percentile(samples, 40); got != 20 {
		t.Fatalf("p40=%v want 20", got)
	}
}
