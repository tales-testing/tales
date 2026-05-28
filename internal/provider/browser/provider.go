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
	"fmt"
	"sync"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/zclconf/go-cty/cty"
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

// Execute satisfies provider.Provider. It resolves the requested browser
// target, acquires (or reuses) a Chrome subprocess for it, derives a
// per-scenario browsing context off that allocator, runs the prepared
// actions in order, then runs every declared expectation. The recorded
// post-step snapshot backs the runtime's capture helpers.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.Browser == nil {
		return nil, errors.New("browser: missing pre-evaluated step data")
	}

	if p.builder == nil {
		return nil, errors.New("browser: no session builder configured")
	}

	start := time.Now()

	debugf("execute", "scenario=%q step=%q phase=%q attempt=%d actions=%d", input.Scenario, stepName(input), input.Phase, input.Attempt, len(input.Browser.Actions))

	target, err := ResolveTarget(input.Config, input.Browser.TargetName)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}

	debugf("execute", "resolved target=%q headless=%v viewport=%dx%d timeout=%s", target.Name, target.Driver.Headless, target.Driver.Viewport.Width, target.Driver.Viewport.Height, target.Driver.Timeout)

	sess, err := p.acquireSession(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}

	debugf("execute", "session acquired target=%q", target.Name)

	sc, err := p.acquireScenarioCtx(ctx, sess, input.Scenario)
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}

	debugf("execute", "scenario ctx acquired scenario=%q", input.Scenario)

	stepLock := p.stepLockFor(target.Name)
	stepLock.Lock()

	defer stepLock.Unlock()

	debugf("execute", "step lock acquired target=%q", target.Name)

	stepDir := stepArtifactDir(p.artifactsBase, fileForStep(input), input.Scenario, stepName(input), input.Phase, input.Attempt)
	defaultURL := readConfigBaseURL(input.Config)

	results, runErr := p.runActions(ctx, sc.Driver, sc, stepDir, defaultURL, target, input.Browser.Actions)

	out := &provider.Output{
		Request:       map[string]cty.Value{},
		Response:      map[string]cty.Value{},
		ActionResults: results,
	}

	if runErr != nil {
		out.Duration = time.Since(start)
		out.Response = browserResponseValues(target, sc.Driver, ctx, nil)
		out.Response["artifacts"] = artifactsListValue(p.captureStepLevel(ctx, sc.Driver, stepDir, true))

		return out, runErr
	}

	if input.Browser.Expect.HasAny() {
		if err := p.handleExpect(ctx, sc.Driver, input.Browser.Expect); err != nil {
			out.Duration = time.Since(start)
			out.Response = browserResponseValues(target, sc.Driver, ctx, nil)
			out.Response["artifacts"] = artifactsListValue(p.captureStepLevel(ctx, sc.Driver, stepDir, true))

			return out, err
		}
	}

	snap := p.recordSnapshot(ctx, sc.Driver, input.Scenario, stepName(input))

	out.Response = browserResponseValues(target, sc.Driver, ctx, snap)

	var stepArtifacts []Artifact
	if p.captureMode == provider.CaptureSteps {
		stepArtifacts = p.captureStepLevel(ctx, sc.Driver, stepDir, false)
	}

	if len(stepArtifacts) > 0 {
		out.Response["artifacts"] = artifactsListValue(stepArtifacts)
	} else {
		out.Response["artifacts"] = emptyArtifactsList()
	}

	out.Duration = time.Since(start)

	return out, nil
}

func (p *Provider) acquireSession(ctx context.Context, target Target) (*Session, error) {
	lock := p.targetLockFor(target.Name)
	lock.Lock()

	defer lock.Unlock()

	p.mu.Lock()

	if sess, ok := p.sessions[target.Name]; ok {
		p.mu.Unlock()

		return sess, nil
	}

	p.mu.Unlock()

	sess, err := p.builder.Build(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("build session: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.sessions[target.Name]; ok {
		sess.closeAll()

		return existing, nil
	}

	p.sessions[target.Name] = sess

	return sess, nil
}

func (p *Provider) acquireScenarioCtx(ctx context.Context, sess *Session, scenario string) (*ScenarioBrowserCtx, error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.scenarios == nil {
		sess.scenarios = map[string]*ScenarioBrowserCtx{}
	}

	if existing, ok := sess.scenarios[scenario]; ok {
		return existing, nil
	}

	sc, err := p.builder.NewScenarioContext(ctx, sess, scenario)
	if err != nil {
		return nil, fmt.Errorf("new scenario context: %w", err)
	}

	sess.scenarios[scenario] = sc

	return sc, nil
}

func (p *Provider) targetLockFor(name string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.targetLocks[name]; ok {
		return existing
	}

	created := &sync.Mutex{}
	p.targetLocks[name] = created

	return created
}

func (p *Provider) stepLockFor(name string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.stepLocks[name]; ok {
		return existing
	}

	created := &sync.Mutex{}
	p.stepLocks[name] = created

	return created
}

// recordSnapshot captures URL / title / outerHTML once after a successful
// step so the runtime's capture helpers see a consistent state.
func (p *Provider) recordSnapshot(ctx context.Context, drv driver.Driver, scenario, step string) *Snapshot {
	url, _ := drv.URL(ctx)
	title, _ := drv.Title(ctx)
	dom, _ := drv.OuterHTML(ctx, "html")

	snap := &Snapshot{URL: url, Title: title, DOM: dom}

	p.snapshotsMu.Lock()
	p.snapshots[snapshotKey(scenario, step)] = snap
	p.snapshotsMu.Unlock()

	return snap
}

func fileForStep(input provider.Input) string {
	if input.Step != nil {
		return input.Step.File
	}

	return ""
}

func stepName(input provider.Input) string {
	if input.Step == nil {
		return ""
	}

	return input.Step.Name
}

func readConfigBaseURL(config map[string]cty.Value) string {
	v, ok := config["base_url"]
	if !ok || v.IsNull() || v.Type() != cty.String {
		return ""
	}

	return v.AsString()
}

func browserResponseValues(target Target, drv driver.Driver, ctx context.Context, snap *Snapshot) map[string]cty.Value {
	out := map[string]cty.Value{
		"target": cty.StringVal(target.Name),
	}

	if snap != nil {
		out["url"] = cty.StringVal(snap.URL)
		out["title"] = cty.StringVal(snap.Title)

		return out
	}

	url, _ := drv.URL(ctx)
	title, _ := drv.Title(ctx)
	out["url"] = cty.StringVal(url)
	out["title"] = cty.StringVal(title)

	return out
}

// artifactType / artifactPath are the cty object attribute names used inside
// artifactsListValue. Centralized to satisfy the goconst rule, kept private
// because the visual-report layer consumes them through report.Artifact, not
// these strings directly.
const (
	artifactType = "type"
	artifactPath = "path"
)

func emptyArtifactsList() cty.Value {
	return cty.ListValEmpty(cty.Object(map[string]cty.Type{artifactType: cty.String, artifactPath: cty.String}))
}

func artifactsListValue(in []Artifact) cty.Value {
	if len(in) == 0 {
		return emptyArtifactsList()
	}

	values := make([]cty.Value, 0, len(in))

	for _, a := range in {
		values = append(values, cty.ObjectVal(map[string]cty.Value{
			artifactType: cty.StringVal(a.Type),
			artifactPath: cty.StringVal(a.Path),
		}))
	}

	return cty.ListVal(values)
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
