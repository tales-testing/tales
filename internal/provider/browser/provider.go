// Package browser is the Tales browser step provider. It drives Chrome /
// Chromium via the Chrome DevTools Protocol (chromedp) to run declarative
// .tales browser steps: navigate, click, fill, assert, capture.
//
// V1 targets Chrome/Chromium only. CSS selectors are the only locator
// surface. The provider mirrors the mobile provider's architecture:
//   - one allocator per target (Chrome subprocess) cached for the suite
//   - one fresh browsing context per (target, scenario) for cookie isolation
//   - per-target lock to serialize step starts on the same target
//   - artifacts (screenshot, dom.html) written under build/artifacts/browser/
package browser

import (
	"context"
	"errors"
	"sync"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/artifacts"
)

// Provider is the browser step provider implementation.
type Provider struct {
	mu       sync.Mutex
	sessions map[string]*Session // key: target name

	targetLocks map[string]*sync.Mutex
	stepLocks   map[string]*sync.Mutex

	snapshotsMu sync.RWMutex
	snapshots   map[string]*Snapshot

	builder       SessionBuilder
	artifactsBase string
	captureMode   provider.CaptureMode
}

// Option configures a Provider via New / NewChrome.
type Option func(*Provider)

// WithSessionBuilder swaps the default chromedp-backed builder for a custom
// implementation. Tests use this to inject a fake browser.
func WithSessionBuilder(b SessionBuilder) Option {
	return func(p *Provider) { p.builder = b }
}

// WithCaptureMode sets when screenshots / DOM dumps are captured.
func WithCaptureMode(m provider.CaptureMode) Option {
	return func(p *Provider) { p.captureMode = m }
}

// WithArtifactsBase overrides the base directory under which the provider
// writes screenshots / DOM dumps.
func WithArtifactsBase(dir string) Option {
	return func(p *Provider) { p.artifactsBase = dir }
}

// New builds a provider with the supplied options. A SessionBuilder must be
// set via WithSessionBuilder; NewChrome wraps this with the chromedp-backed
// builder for production use.
func New(opts ...Option) *Provider {
	p := &Provider{
		sessions:      map[string]*Session{},
		targetLocks:   map[string]*sync.Mutex{},
		stepLocks:     map[string]*sync.Mutex{},
		snapshots:     map[string]*Snapshot{},
		artifactsBase: artifacts.DefaultBase,
		captureMode:   provider.CaptureFailures,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Type satisfies provider.Provider.
func (p *Provider) Type() string { return "browser" }

// Execute satisfies provider.Provider. The real implementation lands in a
// later commit; until then the registry returns a clear error so users
// know the provider is registered but not yet functional.
func (p *Provider) Execute(_ context.Context, input provider.Input) (*provider.Output, error) {
	if input.Browser == nil {
		return nil, errors.New("browser: missing pre-evaluated step data")
	}

	if p.builder == nil {
		return nil, errors.New("browser: no session builder configured")
	}

	return nil, errors.New("browser: provider not yet implemented")
}

// Close cancels every cached session, terminating Chrome subprocesses.
// Implements io.Closer so the runner can clean up at suite end.
func (p *Provider) Close() error {
	p.mu.Lock()
	sessions := p.sessions
	p.sessions = map[string]*Session{}
	p.mu.Unlock()

	for _, sess := range sessions {
		sess.closeAll()
	}

	return nil
}

// LastSnapshot returns the snapshot recorded for the given (scenario, step)
// pair, or nil when no snapshot has been recorded yet.
func (p *Provider) LastSnapshot(scenario, step string) (*Snapshot, bool) {
	p.snapshotsMu.RLock()
	defer p.snapshotsMu.RUnlock()

	snap, ok := p.snapshots[snapshotKey(scenario, step)]

	return snap, ok
}

func snapshotKey(scenario, step string) string {
	return scenario + "\x00" + step
}
