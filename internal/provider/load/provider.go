package load

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	httpprovider "github.com/tales-testing/tales/internal/provider/http"
	"github.com/zclconf/go-cty/cty"
)

// providerType is the provider label registered with the runtime.
const providerType = "load"

// Response key names shared between buildResponse and the runtime
// shortcut layer. Centralized so the source of truth lives next to
// the runner output.
const (
	keyStatus2xxRatio = "status_2xx_ratio"
	keyStatus3xxRatio = "status_3xx_ratio"
	keyStatus4xxRatio = "status_4xx_ratio"
	keyStatus5xxRatio = "status_5xx_ratio"
	keyLatency        = "latency"
	keyBytes          = "bytes"
	keyRequests       = "requests"
	keyDurationMS     = "duration_ms"
	keyRPS            = "rps"
	keyErrors         = "errors"
	keyErrorRatio     = "error_ratio"
	keyStatus         = "status"
)

// Provider runs concurrent HTTP requests and exposes aggregate metrics
// (percentiles, RPS, error and status-class ratios) suitable for smoke
// performance budgets. It is not a substitute for k6/Gatling — V1
// trades feature breadth for a Go-native dependency-free footprint.
type Provider struct {
	client *http.Client
}

// New builds a load provider with a fresh *http.Client. The client has
// no global timeout; per-request budgets come from run.timeout (the
// HTTP block's timeout attribute is interpreted as the per-call cap).
func New() *Provider {
	return &Provider{
		client: &http.Client{
			Transport: http.DefaultTransport,
		},
	}
}

// Type returns the provider label.
func (p *Provider) Type() string {
	return providerType
}

// Execute runs the configured load test and returns a provider.Output
// whose Response carries both the canonical result JSON (under "json")
// and shortcut keys used by expect { p95 = lt(...) … }.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.Load == nil {
		return nil, errors.New("load step is missing execution configuration")
	}

	template, err := httpprovider.BuildRequestTemplate(input)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	timeout := input.Timeout
	if t, ok := requestTimeout(input.Request); ok && (timeout == 0 || t < timeout) {
		timeout = t
	}

	cfg := runConfig{
		mode:        input.Load.Mode,
		duration:    input.Load.Duration,
		requests:    input.Load.Requests,
		concurrency: input.Load.Concurrency,
		rate:        input.Load.Rate,
		warmup:      input.Load.Warmup,
		timeout:     timeout,
	}

	start := time.Now()

	result, err := executeRun(ctx, p.client, template, cfg)
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}

	if result.requests == 0 {
		return nil, errNoSamples
	}

	response := buildResponse(result)

	return &provider.Output{
		Duration: time.Since(start),
		Request: map[string]cty.Value{
			"method":  cty.StringVal(template.Method),
			"url":     cty.StringVal(template.URL),
			"headers": stringMapValue(template.ReportHeaders),
		},
		Response: response,
	}, nil
}

// buildResponse assembles the response.json object and surfaces every
// shortcut at the top level so users can write either
// `expect { p95 = lt(...) }` or `expect { json = { latency = { p95_ms = lt(...) } } }`.
func buildResponse(r *runResult) map[string]cty.Value {
	latency := computeLatency(r.latencies)

	var rps float64
	if r.totalDuration > 0 {
		rps = float64(r.requests) / r.totalDuration.Seconds()
	}

	errorRatio := 0.0
	if r.requests > 0 {
		errorRatio = float64(r.errors) / float64(r.requests)
	}

	status := map[string]cty.Value{
		"1xx": cty.NumberIntVal(int64(r.statusCounts[1])),
		"2xx": cty.NumberIntVal(int64(r.statusCounts[2])),
		"3xx": cty.NumberIntVal(int64(r.statusCounts[3])),
		"4xx": cty.NumberIntVal(int64(r.statusCounts[4])),
		"5xx": cty.NumberIntVal(int64(r.statusCounts[5])),
	}

	latencyObj := cty.ObjectVal(map[string]cty.Value{
		"min_ms":  cty.NumberFloatVal(latency.MinMS),
		"max_ms":  cty.NumberFloatVal(latency.MaxMS),
		"mean_ms": cty.NumberFloatVal(latency.MeanMS),
		"p50_ms":  cty.NumberFloatVal(latency.P50MS),
		"p90_ms":  cty.NumberFloatVal(latency.P90MS),
		"p95_ms":  cty.NumberFloatVal(latency.P95MS),
		"p99_ms":  cty.NumberFloatVal(latency.P99MS),
	})

	bytesObj := cty.ObjectVal(map[string]cty.Value{
		"in":  cty.NumberIntVal(r.bytesIn),
		"out": cty.NumberIntVal(r.bytesOut),
	})

	statusRatio := func(class int) float64 {
		if r.requests == 0 {
			return 0
		}

		return float64(r.statusCounts[class]) / float64(r.requests)
	}

	jsonPayload := cty.ObjectVal(map[string]cty.Value{
		keyRequests:       cty.NumberIntVal(int64(r.requests)),
		keyDurationMS:     cty.NumberFloatVal(float64(r.totalDuration) / float64(time.Millisecond)),
		keyRPS:            cty.NumberFloatVal(rps),
		keyErrors:         cty.NumberIntVal(int64(r.errors)),
		keyErrorRatio:     cty.NumberFloatVal(errorRatio),
		keyStatus:         cty.ObjectVal(status),
		keyStatus2xxRatio: cty.NumberFloatVal(statusRatio(2)),
		keyStatus3xxRatio: cty.NumberFloatVal(statusRatio(3)),
		keyStatus4xxRatio: cty.NumberFloatVal(statusRatio(4)),
		keyStatus5xxRatio: cty.NumberFloatVal(statusRatio(5)),
		keyLatency:        latencyObj,
		keyBytes:          bytesObj,
	})

	return map[string]cty.Value{
		"json":            jsonPayload,
		keyRequests:       cty.NumberIntVal(int64(r.requests)),
		keyDurationMS:     cty.NumberFloatVal(float64(r.totalDuration) / float64(time.Millisecond)),
		keyRPS:            cty.NumberFloatVal(rps),
		keyErrors:         cty.NumberIntVal(int64(r.errors)),
		keyErrorRatio:     cty.NumberFloatVal(errorRatio),
		keyStatus2xxRatio: cty.NumberFloatVal(statusRatio(2)),
		keyStatus3xxRatio: cty.NumberFloatVal(statusRatio(3)),
		keyStatus4xxRatio: cty.NumberFloatVal(statusRatio(4)),
		keyStatus5xxRatio: cty.NumberFloatVal(statusRatio(5)),
		"p50":             cty.NumberFloatVal(latency.P50MS),
		"p90":             cty.NumberFloatVal(latency.P90MS),
		"p95":             cty.NumberFloatVal(latency.P95MS),
		"p99":             cty.NumberFloatVal(latency.P99MS),
		"min":             cty.NumberFloatVal(latency.MinMS),
		"max":             cty.NumberFloatVal(latency.MaxMS),
		"mean":            cty.NumberFloatVal(latency.MeanMS),
		keyBytes:          bytesObj,
		keyStatus:         cty.ObjectVal(status),
		keyLatency:        latencyObj,
	}
}

// requestTimeout reads an optional request.timeout attribute as a
// duration. Both number-of-seconds and Go duration strings are tried;
// the load step's HCL surface uses the latter.
func requestTimeout(request map[string]cty.Value) (time.Duration, bool) {
	v, ok := request["timeout"]
	if !ok || v.IsNull() {
		return 0, false
	}

	if v.Type() == cty.String {
		d, err := time.ParseDuration(v.AsString())
		if err != nil {
			return 0, false
		}

		return d, true
	}

	if v.Type() == cty.Number {
		f, _ := v.AsBigFloat().Float64()

		return time.Duration(f) * time.Second, true
	}

	return 0, false
}

func stringMapValue(values map[string]string) cty.Value {
	if len(values) == 0 {
		return cty.EmptyObjectVal
	}

	out := make(map[string]cty.Value, len(values))
	for k, v := range values {
		out[k] = cty.StringVal(v)
	}

	return cty.ObjectVal(out)
}
