package load

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	httpprovider "github.com/tales-testing/tales/internal/provider/http"
)

// runConfig is the resolved input to the load runner. mode determines
// whether the runner stops after a duration or a fixed request count.
type runConfig struct {
	mode        string
	duration    time.Duration
	requests    int
	concurrency int
	rate        float64
	warmup      time.Duration
	timeout     time.Duration
}

// runResult is the aggregated outcome of one load run. Latencies are
// kept as time.Duration so callers can pick the precision they need.
type runResult struct {
	requests       int
	errors         int
	statusCounts   [6]int // index = HTTP status class (0..5); 0 = client/transport error or no status
	bytesIn        int64
	bytesOut       int64
	totalDuration  time.Duration
	latencies      []time.Duration
	firstErrorMsg  string
	firstErrorOnce sync.Once
}

// rateLimiter is the global token bucket shared by all workers. nil
// means no limiting. tick blocks until the next token is available or
// the context is canceled.
type rateLimiter struct {
	ticker *time.Ticker
}

func newRateLimiter(rps float64) *rateLimiter {
	if rps <= 0 {
		return nil
	}

	interval := time.Duration(float64(time.Second) / rps)
	if interval <= 0 {
		interval = time.Nanosecond
	}

	return &rateLimiter{ticker: time.NewTicker(interval)}
}

func (r *rateLimiter) Stop() {
	if r != nil && r.ticker != nil {
		r.ticker.Stop()
	}
}

func (r *rateLimiter) wait(ctx context.Context) error {
	if r == nil {
		return nil
	}

	select {
	case <-r.ticker.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("rate limiter ctx: %w", ctx.Err())
	}
}

// executeRun runs the load test against template and returns the
// aggregated result. The client is owned by the caller (the provider
// reuses one *http.Client across runs for keep-alive).
func executeRun(
	ctx context.Context,
	client *http.Client,
	template *httpprovider.RequestTemplate,
	cfg runConfig,
) (*runResult, error) {
	limiter := newRateLimiter(cfg.rate)
	defer limiter.Stop()

	if cfg.warmup > 0 {
		warmCtx, cancel := context.WithTimeout(ctx, cfg.warmup)
		runWorkers(warmCtx, client, template, cfg, limiter, nil)

		cancel()
	}

	result := &runResult{}

	var workCtx context.Context

	var cancel context.CancelFunc

	switch cfg.mode {
	case modeDuration:
		workCtx, cancel = context.WithTimeout(ctx, cfg.duration)
		defer cancel()
	default:
		workCtx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	start := time.Now()

	runWorkers(workCtx, client, template, cfg, limiter, result)

	result.totalDuration = time.Since(start)

	return result, nil
}

// modeDuration / modeRequests label the two stopping rules so the
// runtime and the runner share a single source of truth.
const (
	modeDuration = "duration"
	modeRequests = "requests"
)

// runWorkers spawns cfg.concurrency goroutines that fire requests
// against template. When result is nil the round is treated as warmup:
// latencies and counters are discarded. When result is non-nil, the
// workers either run until ctx is canceled (duration mode) or until
// cfg.requests measured calls have been completed (requests mode).
func runWorkers(
	ctx context.Context,
	client *http.Client,
	template *httpprovider.RequestTemplate,
	cfg runConfig,
	limiter *rateLimiter,
	result *runResult,
) {
	var (
		remaining   int64
		stopOnLimit bool
	)

	if result != nil && cfg.mode == modeRequests && cfg.requests > 0 {
		remaining = int64(cfg.requests)
		stopOnLimit = true
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	worker := func() {
		defer wg.Done()

		for {
			if ctx.Err() != nil {
				return
			}

			if stopOnLimit {
				if atomic.AddInt64(&remaining, -1) < 0 {
					return
				}
			}

			if err := limiter.wait(ctx); err != nil {
				return
			}

			latency, status, bytesIn, bytesOut, err := doOne(ctx, client, template, cfg.timeout)

			if result == nil {
				continue
			}

			mu.Lock()
			recordSample(result, latency, status, bytesIn, bytesOut, err)
			mu.Unlock()
		}
	}

	workers := cfg.concurrency
	if workers <= 0 {
		workers = 1
	}

	for range workers {
		wg.Add(1)

		go worker()
	}

	wg.Wait()
}

// recordSample is called under the result lock with the outcome of one
// completed request. It increments the totals and appends the latency
// to the histogram source.
func recordSample(r *runResult, latency time.Duration, status int, bytesIn, bytesOut int64, err error) {
	r.requests++
	r.latencies = append(r.latencies, latency)
	r.bytesIn += bytesIn
	r.bytesOut += bytesOut

	if err != nil {
		r.errors++
		r.statusCounts[0]++
		r.firstErrorOnce.Do(func() {
			r.firstErrorMsg = err.Error()
		})

		return
	}

	switch {
	case status >= 100 && status < 200:
		r.statusCounts[1]++
	case status >= 200 && status < 300:
		r.statusCounts[2]++
	case status >= 300 && status < 400:
		r.statusCounts[3]++
	case status >= 400 && status < 500:
		r.statusCounts[4]++
	case status >= 500 && status < 600:
		r.statusCounts[5]++
	}
}

// doOne issues a single request and reports the wall-clock latency,
// status code, received body size, and request body size. Body
// reading is bounded only by the client's response body close
// semantics; we drain to io.Discard to free the connection.
func doOne(
	ctx context.Context,
	client *http.Client,
	template *httpprovider.RequestTemplate,
	timeout time.Duration,
) (time.Duration, int, int64, int64, error) {
	reqCtx := ctx

	if timeout > 0 {
		var cancel context.CancelFunc

		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := buildRequest(reqCtx, template)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	var bytesIn int64

	if err == nil && resp != nil {
		bytesIn, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	if err != nil {
		return latency, 0, 0, int64(len(template.Body)), fmt.Errorf("request: %w", err)
	}

	return latency, resp.StatusCode, bytesIn, int64(len(template.Body)), nil
}

// buildRequest produces a fresh *http.Request from the template. Each
// call wraps the body bytes in a new bytes.Reader so concurrent
// workers do not share a reader.
func buildRequest(ctx context.Context, t *httpprovider.RequestTemplate) (*http.Request, error) {
	var body io.Reader
	if len(t.Body) > 0 {
		body = bytes.NewReader(t.Body)
	}

	req, err := http.NewRequestWithContext(ctx, t.Method, t.URL, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}

	if t.BasicAuth != nil {
		req.SetBasicAuth(t.BasicAuth.Username, t.BasicAuth.Password)
	}

	return req, nil
}

var errNoSamples = errors.New("load step collected no samples — check duration/requests/concurrency/rate")
