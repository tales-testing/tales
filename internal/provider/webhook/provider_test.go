package webhook

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

func startReceiver(t *testing.T, p *Provider, id, path string) *provider.Output {
	t.Helper()

	out, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "start", ID: id, Path: path, Address: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	return out
}

func listenURL(out *provider.Output) string {
	return out.Response["listen_url"].AsString()
}

func TestProviderStartReceiveWaitStop(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	start := startReceiver(t, p, "webhook_a", "/hook")
	url := listenURL(start)

	resp, err := http.Post(url, "application/json", strings.NewReader(`{"event":"order.created","id":7}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("receiver returned %d, want 200", resp.StatusCode)
	}

	waitOut, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "wait", Target: "webhook_a", Count: 1, Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	if got := waitOut.Request["method"].AsString(); got != http.MethodPost {
		t.Fatalf("method = %q, want POST", got)
	}

	if got := waitOut.Request["path"].AsString(); got != "/hook" {
		t.Fatalf("path = %q, want /hook", got)
	}

	body := waitOut.Request["body"]
	if raw := body.GetAttr("raw").AsString(); raw != `{"event":"order.created","id":7}` {
		t.Fatalf("raw body = %q", raw)
	}

	jsonVal := waitOut.Request[jsonKey]
	if jsonVal.GetAttr("event").AsString() != "order.created" {
		t.Fatalf("json.event mismatch: %#v", jsonVal)
	}

	summary := waitOut.Response[jsonKey]
	if !summary.GetAttr("received").True() {
		t.Fatal("summary.received should be true")
	}

	count, _ := summary.GetAttr("count").AsBigFloat().Int64()
	if count != 1 {
		t.Fatalf("summary.count = %d, want 1", count)
	}

	stopOut, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "stop", Target: "webhook_a"},
	})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	if !stopOut.Response["stopped"].True() {
		t.Fatal("stop should report stopped=true")
	}

	// The listener must be gone after stop.
	if _, err := http.Post(url, "application/json", strings.NewReader(`{}`)); err == nil {
		t.Fatal("expected post to fail after stop")
	}
}

func TestProviderWaitPreservesHeadersAndQuery(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	start := startReceiver(t, p, "webhook_h", "/hook")

	req, _ := http.NewRequest(http.MethodPost, listenURL(start)+"?foo=bar&foo=baz", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("X-Multi", "one")
	req.Header.Add("X-Multi", "two")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	_ = resp.Body.Close()

	out, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "wait", Target: "webhook_h", Count: 1, Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	headers := out.Request["headers"]
	if got := headers.GetAttr("X-Multi").AsString(); got != "one" {
		t.Fatalf("first-value header = %q, want one", got)
	}

	headersAll := out.Request["headers_all"]

	multi := headersAll.GetAttr("X-Multi").AsValueSlice()
	if len(multi) != 2 || multi[0].AsString() != "one" || multi[1].AsString() != "two" {
		t.Fatalf("headers_all preserved wrong: %#v", multi)
	}

	query := out.Request["query"]

	foo := query.GetAttr("foo").AsValueSlice()
	if len(foo) != 2 {
		t.Fatalf("query foo should preserve 2 values, got %d", len(foo))
	}
}

func TestProviderWaitTimeout(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	startReceiver(t, p, "webhook_t", "/hook")

	_, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "wait", Target: "webhook_t", Count: 1, Timeout: 150 * time.Millisecond},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProviderWaitCountSelectsLatest(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	start := startReceiver(t, p, "webhook_c", "/hook")
	url := listenURL(start)

	for i := range 3 {
		resp, err := http.Post(url, "application/json", strings.NewReader(fmt.Sprintf(`{"n":%d}`, i)))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}

		_ = resp.Body.Close()
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "wait", Target: "webhook_c", Count: 3, Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	count, _ := out.Response[jsonKey].GetAttr("count").AsBigFloat().Int64()
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	latest := out.Request[jsonKey]

	n, _ := latest.GetAttr("n").AsBigFloat().Int64()
	if n != 2 {
		t.Fatalf("latest n = %d, want 2 (last received)", n)
	}
}

func TestProviderWaitUnknownTarget(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	_, err := p.Execute(context.Background(), provider.Input{
		Webhook: &provider.WebhookExecution{Operation: "wait", Target: "missing", Count: 1, Timeout: time.Second},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestProviderNonMatchingPathIsNotFound(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	start := startReceiver(t, p, "webhook_p", "/hook")
	base := strings.TrimSuffix(listenURL(start), "/hook")

	resp, err := http.Post(base+"/other", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-matching path returned %d, want 404", resp.StatusCode)
	}
}

func TestProviderCloseStopsAllReceivers(t *testing.T) {
	t.Parallel()

	p := New()

	a := listenURL(startReceiver(t, p, "webhook_close_a", "/a"))
	b := listenURL(startReceiver(t, p, "webhook_close_b", "/b"))

	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, url := range []string{a, b} {
		if _, err := http.Post(url, "application/json", strings.NewReader("{}")); err == nil {
			t.Fatalf("expected %s to be closed", url)
		}
	}
}

func TestProviderStartIsIdempotent(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	first := startReceiver(t, p, "webhook_idem", "/hook")
	second := startReceiver(t, p, "webhook_idem", "/hook")

	if first.Response["url"].AsString() != second.Response["url"].AsString() {
		t.Fatal("idempotent start must return the same receiver URL")
	}
}

func TestProviderConcurrentReceivers(t *testing.T) {
	t.Parallel()

	p := New()
	defer func() { _ = p.Close() }()

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			id := fmt.Sprintf("webhook_conc_%d", n)

			start := startReceiver(t, p, id, "/hook")

			resp, err := http.Post(listenURL(start), "application/json", strings.NewReader(`{"ok":true}`))
			if err != nil {
				t.Errorf("post %d: %v", n, err)

				return
			}

			_ = resp.Body.Close()

			if _, err := p.Execute(context.Background(), provider.Input{
				Webhook: &provider.WebhookExecution{Operation: "wait", Target: id, Count: 1, Timeout: 2 * time.Second},
			}); err != nil {
				t.Errorf("wait %d: %v", n, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestBuildWebhookURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		params     webhookStartParams
		listenHost string
		actualPort int
		want       string
	}{
		{
			name:       "public_url overrides all",
			params:     webhookStartParams{Path: "/hook", PublicURL: "https://example.test/in", PublicHost: "ignored"},
			listenHost: "127.0.0.1",
			actualPort: 49321,
			want:       "https://example.test/in",
		},
		{
			name:       "public_host with real port",
			params:     webhookStartParams{Path: "/webhooks/orders", PublicHost: "host.docker.internal"},
			listenHost: "127.0.0.1",
			actualPort: 49321,
			want:       "http://host.docker.internal:49321/webhooks/orders",
		},
		{
			name:       "public_port overrides listener port",
			params:     webhookStartParams{Path: "/hook", PublicHost: "tales", PublicPort: 9000},
			listenHost: "0.0.0.0",
			actualPort: 49321,
			want:       "http://tales:9000/hook",
		},
		{
			name:       "default uses listener host and port",
			params:     webhookStartParams{Path: "/hook"},
			listenHost: "127.0.0.1",
			actualPort: 49321,
			want:       "http://127.0.0.1:49321/hook",
		},
		{
			name:       "custom public scheme",
			params:     webhookStartParams{Path: "/hook", PublicScheme: "https", PublicHost: "edge"},
			listenHost: "127.0.0.1",
			actualPort: 443,
			want:       "https://edge:443/hook",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := buildWebhookURL(tc.params, tc.listenHost, tc.actualPort); got != tc.want {
				t.Fatalf("buildWebhookURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReceivedRequestValueRoundTrip ensures the cty object never carries a
// NilVal (which would panic ObjectVal upstream).
func TestReceivedRequestValueRoundTrip(t *testing.T) {
	t.Parallel()

	value := receivedRequestValue(&receivedRequest{
		Method:     http.MethodPost,
		Path:       "/hook",
		RawBody:    "not json",
		JSONBody:   cty.NullVal(cty.DynamicPseudoType),
		ReceivedAt: time.Unix(1_700_000_000, 0),
	})

	if value.GetAttr(jsonKey).IsNull() != true {
		t.Fatal("non-JSON body should expose a null json alias")
	}

	if value.GetAttr("body").GetAttr("raw").AsString() != "not json" {
		t.Fatal("raw body should be preserved verbatim")
	}
}
