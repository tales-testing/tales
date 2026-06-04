package webhook

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// defaultMaxBodySize bounds how many bytes the receiver reads from one incoming
// webhook request body. 10 MiB is generous for JSON payloads while protecting
// the in-memory buffer from an unbounded sender.
const defaultMaxBodySize int64 = 10 << 20

// localShutdownTimeout bounds how long a receiver waits for in-flight requests
// to drain on stop / close so teardown never hangs.
const localShutdownTimeout = 2 * time.Second

// receivedRequest is one inbound HTTP request captured by a receiver.
type receivedRequest struct {
	Method     string
	Path       string
	Query      url.Values
	Headers    http.Header
	RawBody    string
	JSONBody   cty.Value
	RemoteAddr string
	ReceivedAt time.Time
}

// receiver is a single live HTTP endpoint that records every inbound request
// matching its path. It is safe for concurrent use: the HTTP handler goroutine
// appends under mu while wait reads under the same lock.
type receiver struct {
	id      string
	path    string
	ln      net.Listener
	srv     *http.Server
	maxBody int64
	now     func() time.Time

	// Descriptor fields resolved at start; immutable afterwards.
	url       string
	listenURL string
	address   string
	port      int

	mu       sync.Mutex
	requests []*receivedRequest
	signal   chan struct{}
	closed   bool
}

// handle records every request whose path matches and answers 200. Non-matching
// paths get 404 so the receiver stays scoped to its declared route.
func (rc *receiver) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != rc.path {
		http.NotFound(w, r)

		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, rc.maxBody))

	captured := &receivedRequest{
		Method:     r.Method,
		Path:       r.URL.Path,
		Query:      r.URL.Query(),
		Headers:    r.Header.Clone(),
		RawBody:    string(body),
		JSONBody:   decodeBodyJSON(r.Header.Get("Content-Type"), body),
		RemoteAddr: r.RemoteAddr,
		ReceivedAt: rc.now().UTC(),
	}

	rc.mu.Lock()
	rc.requests = append(rc.requests, captured)
	rc.mu.Unlock()

	// Wake any waiter without blocking the handler: the buffered slot acts as a
	// "something changed" flag; the waiter re-reads the count under the lock.
	select {
	case rc.signal <- struct{}{}:
	default:
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// waitFor blocks until at least count requests have been recorded, the timeout
// elapses, or ctx is cancelled. It returns a snapshot of the recorded requests.
func (rc *receiver) waitFor(ctx context.Context, count int, timeout time.Duration) ([]*receivedRequest, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		rc.mu.Lock()
		got := len(rc.requests)

		if got >= count {
			snapshot := append([]*receivedRequest(nil), rc.requests...)
			rc.mu.Unlock()

			return snapshot, nil
		}

		rc.mu.Unlock()

		select {
		case <-rc.signal:
		case <-deadline.C:
			rc.mu.Lock()
			got = len(rc.requests)
			rc.mu.Unlock()

			return nil, &timeoutError{id: rc.id, timeout: timeout, count: count, got: got}
		case <-ctx.Done():
			return nil, fmt.Errorf("webhook receiver %q wait cancelled: %w", rc.id, ctx.Err())
		}
	}
}

// shutdown stops the HTTP server and releases the listener. It is idempotent.
func (rc *receiver) shutdown() {
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()

		return
	}

	rc.closed = true
	rc.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), localShutdownTimeout)
	defer cancel()

	_ = rc.srv.Shutdown(ctx)
}

// timeoutError reports that a wait did not observe enough requests in time. It
// never embeds request bodies or secrets.
type timeoutError struct {
	id      string
	timeout time.Duration
	count   int
	got     int
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("webhook receiver %q timed out after %s waiting for %d request(s), got %d", e.id, e.timeout, e.count, e.got)
}

// decodeBodyJSON parses the body as JSON when the content type advertises JSON
// or the bytes look like JSON. A parse failure yields a typed null so the
// `json` namespace round-trips as null rather than panicking ObjectVal.
func decodeBodyJSON(contentType string, body []byte) cty.Value {
	if len(body) == 0 {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	if !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "+json") && !jsonLike(body) {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	inputType, err := ctyjson.ImpliedType(body)
	if err != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	decoded, err := ctyjson.Unmarshal(body, inputType)
	if err != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	return decoded
}

func jsonLike(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))

	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// receivedRequestValue builds the cty object exposed as the `request` namespace
// during a wait step's expect / capture evaluation.
func receivedRequestValue(r *receivedRequest) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"method":      cty.StringVal(r.Method),
		keyPath:       cty.StringVal(r.Path),
		"query":       multiValueObject(r.Query),
		"headers":     firstValueHeaders(r.Headers),
		"headers_all": multiValueHeaders(r.Headers),
		"body": cty.ObjectVal(map[string]cty.Value{
			"raw":   cty.StringVal(r.RawBody),
			jsonKey: r.JSONBody,
		}),
		jsonKey:       r.JSONBody,
		"remote_addr": cty.StringVal(r.RemoteAddr),
		"received_at": cty.StringVal(r.ReceivedAt.Format(time.RFC3339)),
	})
}

// firstValueHeaders renders headers as a canonical-name -> first-value map, the
// convenience shape documented for request.headers["X-..."].
func firstValueHeaders(headers http.Header) cty.Value {
	if len(headers) == 0 {
		return cty.EmptyObjectVal
	}

	out := make(map[string]cty.Value, len(headers))

	for name, values := range headers {
		if len(values) == 0 {
			continue
		}

		out[textproto.CanonicalMIMEHeaderKey(name)] = cty.StringVal(values[0])
	}

	return cty.ObjectVal(out)
}

// multiValueHeaders renders headers as a canonical-name -> list(string) map so
// multi-valued headers are preserved under request.headers_all.
func multiValueHeaders(headers http.Header) cty.Value {
	if len(headers) == 0 {
		return cty.EmptyObjectVal
	}

	out := make(map[string]cty.Value, len(headers))

	for name, values := range headers {
		out[textproto.CanonicalMIMEHeaderKey(name)] = stringList(values)
	}

	return cty.ObjectVal(out)
}

// multiValueObject renders url.Values as a name -> list(string) object.
func multiValueObject(values url.Values) cty.Value {
	if len(values) == 0 {
		return cty.EmptyObjectVal
	}

	out := make(map[string]cty.Value, len(values))
	for name, vs := range values {
		out[name] = stringList(vs)
	}

	return cty.ObjectVal(out)
}

func stringList(values []string) cty.Value {
	if len(values) == 0 {
		return cty.ListValEmpty(cty.String)
	}

	items := make([]cty.Value, 0, len(values))
	for _, v := range values {
		items = append(items, cty.StringVal(v))
	}

	return cty.ListVal(items)
}

// buildWebhookURL resolves the externally reachable callback URL from the
// resolved listener and the public_* knobs. public_url wins outright; otherwise
// the URL is composed from public_scheme/host/port falling back to the listener.
func buildWebhookURL(exec webhookStartParams, listenHost string, actualPort int) string {
	if exec.PublicURL != "" {
		return exec.PublicURL
	}

	scheme := exec.PublicScheme
	if scheme == "" {
		scheme = "http"
	}

	host := exec.PublicHost
	if host == "" {
		host = listenHost
	}

	port := exec.PublicPort
	if port == 0 {
		port = actualPort
	}

	return fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(host, strconv.Itoa(port)), exec.Path)
}

// listenHostFor derives a dialable host for the local listen URL from the bind
// address. Wildcard binds (empty / 0.0.0.0 / ::) collapse to loopback so the
// emitted listen_url is actually reachable.
func listenHostFor(address string, addr net.Addr) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = ""
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		if tcp, ok := addr.(*net.TCPAddr); ok && tcp.IP != nil && !tcp.IP.IsUnspecified() {
			return tcp.IP.String()
		}

		return "127.0.0.1"
	}

	return host
}

// webhookStartParams is the subset of WebhookExecution consumed by URL building.
type webhookStartParams struct {
	Path         string
	PublicURL    string
	PublicScheme string
	PublicHost   string
	PublicPort   int
}
