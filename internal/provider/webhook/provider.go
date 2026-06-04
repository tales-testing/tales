// Package webhook implements the Tales `webhook` provider: a generic, in-memory
// HTTP receiver used to assert on inbound webhooks during e2e tests. A step
// performs exactly one operation — start (boot a receiver), wait (block for
// inbound requests then let the runtime assert), or stop (shut it down). One
// Provider instance is shared across all scenarios; it owns the receiver
// registry and implements io.Closer so leftover receivers are torn down at
// suite end.
package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// Shared attribute names used across the report / response cty maps.
const (
	jsonKey      = "json"
	keyOperation = "operation"
	keyAddress   = "address"
	keyPath      = "path"
)

// readHeaderTimeout bounds how long a receiver waits for request headers,
// mirroring the mockserver's hardening.
const readHeaderTimeout = 5 * time.Second

// Provider is the webhook receiver provider. The receivers map is shared across
// parallel scenarios and guarded by mu. Lock order is always Provider.mu before
// receiver.mu; wait never holds Provider.mu while blocked.
type Provider struct {
	mu        sync.Mutex
	receivers map[string]*receiver
	now       func() time.Time
}

// New creates a webhook provider with a real clock. now is set once and never
// reassigned, so concurrent reads from request handlers are safe.
func New() *Provider {
	return &Provider{
		receivers: map[string]*receiver{},
		now:       time.Now,
	}
}

// Type returns the provider label.
func (p *Provider) Type() string {
	return "webhook"
}

// Execute dispatches on the resolved operation.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.Webhook == nil {
		return nil, fmt.Errorf("webhook step is missing an operation")
	}

	start := time.Now()

	switch input.Webhook.Operation {
	case "start":
		return p.start(ctx, input.Webhook, start)
	case "wait":
		return p.wait(ctx, input.Webhook, start)
	case "stop":
		return p.stop(input.Webhook, start)
	default:
		return nil, fmt.Errorf("unknown webhook operation %q", input.Webhook.Operation)
	}
}

// lookupReceiver returns the receiver registered under id, if any.
func (p *Provider) lookupReceiver(id string) (*receiver, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rc, ok := p.receivers[id]

	return rc, ok
}

// removeReceiver atomically looks up and deletes the receiver registered under
// id, returning it so the caller can shut it down outside the lock.
func (p *Provider) removeReceiver(id string) (*receiver, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rc, ok := p.receivers[id]
	if ok {
		delete(p.receivers, id)
	}

	return rc, ok
}

// Close stops every remaining receiver. Called by the runner at suite end.
func (p *Provider) Close() error {
	p.mu.Lock()
	receivers := p.receivers
	p.receivers = map[string]*receiver{}
	p.mu.Unlock()

	for _, rc := range receivers {
		rc.shutdown()
	}

	return nil
}

// start binds a listener, registers a receiver, and serves it. It is idempotent
// on the receiver ID: a retry of the same start returns the existing receiver
// instead of binding a second listener.
func (p *Provider) start(ctx context.Context, exec *provider.WebhookExecution, started time.Time) (*provider.Output, error) {
	if existing, ok := p.lookupReceiver(exec.ID); ok {
		return startOutput(existing, started), nil
	}

	address := exec.Address
	if address == "" {
		address = "127.0.0.1:0"
	}

	var listenConfig net.ListenConfig

	ln, err := listenConfig.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to start webhook receiver on %s: %w", address, err)
	}

	maxBody := exec.MaxBodySize
	if maxBody <= 0 {
		maxBody = defaultMaxBodySize
	}

	rc := &receiver{
		id:      exec.ID,
		path:    exec.Path,
		ln:      ln,
		maxBody: maxBody,
		now:     p.now,
		signal:  make(chan struct{}, 1),
	}
	rc.url = buildWebhookURL(webhookStartParams{
		Path:         exec.Path,
		PublicURL:    exec.PublicURL,
		PublicScheme: exec.PublicScheme,
		PublicHost:   exec.PublicHost,
		PublicPort:   exec.PublicPort,
	}, listenHostFor(address, ln.Addr()), tcpPort(ln))
	rc.listenURL = fmt.Sprintf("http://%s%s", net.JoinHostPort(listenHostFor(address, ln.Addr()), fmt.Sprint(tcpPort(ln))), exec.Path)
	rc.address = ln.Addr().String()
	rc.port = tcpPort(ln)

	rc.srv = &http.Server{Handler: http.HandlerFunc(rc.handle), ReadHeaderTimeout: readHeaderTimeout}

	go func() { _ = rc.srv.Serve(ln) }()

	p.mu.Lock()
	if existing, ok := p.receivers[exec.ID]; ok {
		p.mu.Unlock()
		rc.shutdown()

		return startOutput(existing, started), nil
	}

	p.receivers[exec.ID] = rc
	p.mu.Unlock()

	return startOutput(rc, started), nil
}

// wait looks up the receiver and blocks until enough requests arrive. Assertions
// happen in the runtime; the provider only returns the recorded requests.
func (p *Provider) wait(ctx context.Context, exec *provider.WebhookExecution, started time.Time) (*provider.Output, error) {
	rc, ok := p.lookupReceiver(exec.Target)
	if !ok {
		return nil, fmt.Errorf("webhook receiver %q not found", exec.Target)
	}

	requests, err := rc.waitFor(ctx, exec.Count, exec.Timeout)
	if err != nil {
		return nil, err
	}

	return waitOutput(requests, started), nil
}

// stop shuts the receiver down and removes it. It is idempotent: stopping an
// already-removed receiver succeeds with stopped=false so teardown never fails.
func (p *Provider) stop(exec *provider.WebhookExecution, started time.Time) (*provider.Output, error) {
	rc, ok := p.removeReceiver(exec.Target)
	if ok {
		rc.shutdown()
	}

	return stopOutput(exec.Target, ok, started), nil
}

func tcpPort(ln net.Listener) int {
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		return tcp.Port
	}

	return 0
}

// startOutput builds the provider output for a start operation. Response carries
// the receiver descriptor exposed to capture via the `webhook` namespace.
func startOutput(rc *receiver, started time.Time) *provider.Output {
	return &provider.Output{
		Request: map[string]cty.Value{
			keyOperation: cty.StringVal("start"),
			keyAddress:   cty.StringVal(rc.address),
			keyPath:      cty.StringVal(rc.path),
		},
		Response: map[string]cty.Value{
			"id":         cty.StringVal(rc.id),
			"url":        cty.StringVal(rc.url),
			"listen_url": cty.StringVal(rc.listenURL),
			keyAddress:   cty.StringVal(rc.address),
			"port":       cty.NumberIntVal(int64(rc.port)),
			keyPath:      cty.StringVal(rc.path),
		},
		Duration: time.Since(started),
	}
}

// waitOutput builds the provider output for a wait operation. Request is the
// selected (latest) received request; Response.json carries the full summary.
func waitOutput(requests []*receivedRequest, started time.Time) *provider.Output {
	values := make([]cty.Value, 0, len(requests))
	for _, r := range requests {
		values = append(values, receivedRequestValue(r))
	}

	var latest cty.Value
	if len(values) > 0 {
		latest = values[len(values)-1]
	} else {
		latest = cty.NullVal(cty.DynamicPseudoType)
	}

	requestsVal := cty.EmptyTupleVal
	if len(values) > 0 {
		requestsVal = cty.TupleVal(values)
	}

	summary := cty.ObjectVal(map[string]cty.Value{
		"received": cty.BoolVal(len(requests) > 0),
		"count":    cty.NumberIntVal(int64(len(requests))),
		"requests": requestsVal,
		"request":  latest,
	})

	request := map[string]cty.Value{}
	if latest.Type() != cty.DynamicPseudoType {
		request = latest.AsValueMap()
	}

	return &provider.Output{
		Request:  request,
		Response: map[string]cty.Value{jsonKey: summary},
		Duration: time.Since(started),
	}
}

func stopOutput(target string, stopped bool, started time.Time) *provider.Output {
	return &provider.Output{
		Request: map[string]cty.Value{
			keyOperation: cty.StringVal("stop"),
			"target":     cty.StringVal(target),
		},
		Response: map[string]cty.Value{
			"stopped": cty.BoolVal(stopped),
		},
		Duration: time.Since(started),
	}
}
