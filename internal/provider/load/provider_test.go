package load

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// roundTripperFunc lets a test inject a synthetic transport.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newOKResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func TestLoadProviderFixedRequestsExecutesExactCount(t *testing.T) {
	t.Parallel()

	var calls int64

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt64(&calls, 1)

		return newOKResponse("ok"), nil
	})

	p := &Provider{client: &http.Client{Transport: rt}}

	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://example.com/health"),
		},
		Load: &provider.LoadExecution{
			Mode:        "requests",
			Requests:    50,
			Concurrency: 5,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := atomic.LoadInt64(&calls); got != 50 {
		t.Fatalf("transport called %d times, want 50", got)
	}

	if got := mustNumber(t, out.Response, "requests"); got != 50 {
		t.Fatalf("response.requests=%v want 50", got)
	}

	if got := mustNumber(t, out.Response, "status_2xx_ratio"); got != 1.0 {
		t.Fatalf("status_2xx_ratio=%v want 1.0", got)
	}

	if got := mustNumber(t, out.Response, "error_ratio"); got != 0 {
		t.Fatalf("error_ratio=%v want 0", got)
	}
}

func TestLoadProviderResponseExposesShortcutsAndJSON(t *testing.T) {
	t.Parallel()

	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return newOKResponse("ok"), nil
	})

	p := &Provider{client: &http.Client{Transport: rt}}

	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://example.com/"),
		},
		Load: &provider.LoadExecution{
			Mode:        "requests",
			Requests:    10,
			Concurrency: 2,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, k := range []string{"p95", "rps", "error_ratio", "status_2xx_ratio", "json"} {
		if _, ok := out.Response[k]; !ok {
			t.Fatalf("response missing key %q (have: %v)", k, mapKeys(out.Response))
		}
	}

	jsonVal := out.Response["json"]
	if !jsonVal.Type().IsObjectType() {
		t.Fatalf("response.json should be object, got %s", jsonVal.Type().FriendlyName())
	}

	jsonMap := jsonVal.AsValueMap()
	if _, ok := jsonMap["latency"]; !ok {
		t.Fatalf("response.json.latency missing")
	}

	if _, ok := jsonMap["status"]; !ok {
		t.Fatalf("response.json.status missing")
	}
}

func TestLoadProviderCountsErrors(t *testing.T) {
	t.Parallel()

	var failures int64

	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		if atomic.AddInt64(&failures, 1)%2 == 0 {
			return nil, io.EOF
		}

		return newOKResponse("ok"), nil
	})

	p := &Provider{client: &http.Client{Transport: rt}}

	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://example.com/"),
		},
		Load: &provider.LoadExecution{
			Mode:        "requests",
			Requests:    10,
			Concurrency: 1,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	errors := mustNumber(t, out.Response, "errors")
	if errors == 0 {
		t.Fatalf("expected non-zero errors when every other call fails")
	}

	ratio := mustNumber(t, out.Response, "error_ratio")
	if ratio == 0 || ratio > 1 {
		t.Fatalf("error_ratio=%v want 0<r<=1", ratio)
	}
}

func TestLoadProviderEmptyConfigError(t *testing.T) {
	t.Parallel()

	p := &Provider{client: &http.Client{}}

	_, err := p.Execute(context.Background(), provider.Input{})
	if err == nil {
		t.Fatalf("expected error when Load is nil")
	}
}

func TestLoadProviderDurationStopsRunner(t *testing.T) {
	t.Parallel()

	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return newOKResponse("ok"), nil
	})

	p := &Provider{client: &http.Client{Transport: rt}}

	start := time.Now()

	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://example.com/"),
		},
		Load: &provider.LoadExecution{
			Mode:        "duration",
			Duration:    150 * time.Millisecond,
			Concurrency: 2,
			Rate:        50,
		},
	})

	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := mustNumber(t, out.Response, "requests"); got == 0 {
		t.Fatalf("expected at least one measured request")
	}

	if elapsed > 2*time.Second {
		t.Fatalf("duration cap not honored: elapsed=%v", elapsed)
	}
}

func mustNumber(t *testing.T, response map[string]cty.Value, key string) float64 {
	t.Helper()

	v, ok := response[key]
	if !ok {
		t.Fatalf("response missing key %q", key)
	}

	f, _ := v.AsBigFloat().Float64()

	return f
}

func mapKeys(m map[string]cty.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}
