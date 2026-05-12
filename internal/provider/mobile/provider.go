// Package mobile is the Tales mobile step provider. It speaks to a UI driver
// (V1: TalesAppleDriver over HTTP/JSON) through a transport-agnostic
// interface, manages a per-target session, and exposes the kind of high-level
// operations a .tales mobile step needs: launch, actions, expectations,
// capture, terminate.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

// expectPollInterval is the wait between two hierarchy fetches during visible
// / not_visible polling.
const expectPollInterval = 250 * time.Millisecond

// expectDefaultTimeout is used when a visibility block omits `timeout`.
const expectDefaultTimeout = 10 * time.Second

// defaultClearTextErase is the number of characters erased by clear_text when
// the element's value length is unknown.
const defaultClearTextErase = 64

// supportedPlatform is the only mobile platform accepted by V1.
const supportedPlatform = "ios"

// Provider is the mobile step provider.
type Provider struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	targetLocks map[string]*sync.Mutex
	builder     SessionBuilder

	hierarchyMu sync.RWMutex
	hierarchies map[string]*tree.ViewNode

	artifactsBase string
}

// Option configures the Provider.
type Option func(*Provider)

// WithSessionBuilder overrides the default SessionBuilder; mostly used in tests.
func WithSessionBuilder(b SessionBuilder) Option {
	return func(p *Provider) {
		p.builder = b
	}
}

// WithArtifactsBase overrides the artifacts base directory.
func WithArtifactsBase(dir string) Option {
	return func(p *Provider) {
		p.artifactsBase = dir
	}
}

// New returns a Provider with a default real-Apple session builder.
func New(opts ...Option) *Provider {
	p := &Provider{
		sessions:      map[string]*Session{},
		targetLocks:   map[string]*sync.Mutex{},
		hierarchies:   map[string]*tree.ViewNode{},
		artifactsBase: defaultArtifactsBase,
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.builder == nil {
		p.builder = defaultSessionBuilder()
	}

	return p
}

// Type identifies the provider in the registry.
func (p *Provider) Type() string {
	return "mobile"
}

// Close shuts down every cached session. It is safe to call multiple times.
func (p *Provider) Close() error {
	p.mu.Lock()

	sessions := p.sessions
	p.sessions = map[string]*Session{}

	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var firstErr error

	for _, sess := range sessions {
		if err := sess.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// LastHierarchy returns the most recent hierarchy captured for the given
// scenario/step pair, or nil when none is recorded yet.
func (p *Provider) LastHierarchy(scenario, step string) *tree.ViewNode {
	p.hierarchyMu.RLock()
	defer p.hierarchyMu.RUnlock()

	return p.hierarchies[hierarchyKey(scenario, step)]
}

func hierarchyKey(scenario, step string) string {
	return scenario + "\x00" + step
}

func (p *Provider) recordHierarchy(scenario, step string, node *tree.ViewNode) {
	if node == nil {
		return
	}

	p.hierarchyMu.Lock()
	p.hierarchies[hierarchyKey(scenario, step)] = node
	p.hierarchyMu.Unlock()
}

// Execute runs one mobile step using a cached or freshly-built session.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.Mobile == nil {
		return nil, errors.New("mobile: missing pre-evaluated step data")
	}

	if input.Mobile.Platform != supportedPlatform {
		return nil, fmt.Errorf("mobile platform %q is not supported yet", input.Mobile.Platform)
	}

	target, err := apple.ResolveTarget(input.Config, input.Mobile.TargetName)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}

	session, err := p.acquireSession(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("acquire session: %w", err)
	}

	start := time.Now()
	output := &provider.Output{
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
	}

	if err := p.executeMobile(ctx, input, session, output); err != nil {
		p.writeFailureArtifacts(ctx, input, session, output)
		output.Duration = time.Since(start)

		return output, err
	}

	output.Duration = time.Since(start)

	return output, nil
}

func (p *Provider) acquireSession(ctx context.Context, target apple.Target) (*Session, error) {
	if sess, ok := p.lookupSession(target.Name); ok {
		return sess, nil
	}

	// Serialize concurrent Build calls per target without blocking other
	// targets: p.builder.Build can take tens of seconds (booting simulators,
	// starting xcodebuild) and we don't want target B to wait on target A.
	lock := p.targetLock(target.Name)
	lock.Lock()
	defer lock.Unlock()

	if sess, ok := p.lookupSession(target.Name); ok {
		return sess, nil
	}

	sess, err := p.builder.Build(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("build session for %q: %w", target.Name, err)
	}

	p.mu.Lock()
	p.sessions[target.Name] = sess
	p.mu.Unlock()

	return sess, nil
}

func (p *Provider) lookupSession(name string) (*Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sess, ok := p.sessions[name]

	return sess, ok
}

func (p *Provider) targetLock(name string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	lock, ok := p.targetLocks[name]
	if !ok {
		lock = &sync.Mutex{}
		p.targetLocks[name] = lock
	}

	return lock
}

func (p *Provider) executeMobile(ctx context.Context, input provider.Input, session *Session, output *provider.Output) error {
	exec := input.Mobile

	if exec.Launch != nil {
		if err := p.handleLaunch(ctx, session, exec.Launch); err != nil {
			return fmt.Errorf("launch: %w", err)
		}
	}

	for i, action := range exec.Actions {
		if err := p.handleAction(ctx, session, action); err != nil {
			return fmt.Errorf("action %d (%s id=%q): %w", i, action.Kind, action.ID, err)
		}
	}

	if len(exec.Expect.Visible) > 0 || len(exec.Expect.NotVisible) > 0 {
		if err := p.handleExpect(ctx, session, exec.Expect); err != nil {
			return fmt.Errorf("expect: %w", err)
		}
	}

	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID)
	if err == nil {
		p.recordHierarchy(input.Scenario, input.Step.Name, hierarchy)
	}

	if exec.Terminate != nil {
		if err := session.Lifecycle.TerminateApp(ctx, session.UDID, session.Target); err != nil {
			return fmt.Errorf("terminate: %w", err)
		}
	}

	output.Response["target"] = cty.StringVal(session.Target.Name)
	output.Response["bundle_id"] = cty.StringVal(session.Target.BundleID)

	return nil
}

func (p *Provider) handleLaunch(ctx context.Context, session *Session, launch *provider.MobileLaunchExec) error {
	if launch.ClearState {
		if err := session.Lifecycle.ClearAppState(ctx, session.UDID, session.Target); err != nil {
			return fmt.Errorf("clear state: %w", err)
		}
	} else if err := session.Lifecycle.InstallApp(ctx, session.UDID, session.Target); err != nil {
		return fmt.Errorf("install app: %w", err)
	}

	if err := session.Lifecycle.LaunchApp(ctx, session.UDID, session.Target); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	return nil
}

func (p *Provider) handleAction(ctx context.Context, session *Session, action provider.MobileActionExec) error {
	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID)
	if err != nil {
		return fmt.Errorf("fetch hierarchy: %w", err)
	}

	node, ok, err := tree.FindByID(hierarchy, action.ID)
	if err != nil {
		return fmt.Errorf("find element: %w", err)
	}

	if !ok {
		return fmt.Errorf("element not found: id %q", action.ID)
	}

	x, y := tree.Center(node)

	switch action.Kind {
	case model.MobileActionTap:
		if err := session.Driver.Tap(ctx, x, y); err != nil {
			return fmt.Errorf("tap: %w", err)
		}
	case model.MobileActionInputText:
		if err := session.Driver.Tap(ctx, x, y); err != nil {
			return fmt.Errorf("focus element: %w", err)
		}

		if err := session.Driver.InputText(ctx, action.Value); err != nil {
			return fmt.Errorf("input text: %w", err)
		}
	case model.MobileActionClearText:
		if err := session.Driver.Tap(ctx, x, y); err != nil {
			return fmt.Errorf("focus element: %w", err)
		}

		count := len([]rune(tree.Value(node)))
		if count == 0 {
			count = defaultClearTextErase
		}

		if err := session.Driver.EraseText(ctx, count); err != nil {
			return fmt.Errorf("erase text: %w", err)
		}
	default:
		return fmt.Errorf("unsupported action kind %q", action.Kind)
	}

	return nil
}

func (p *Provider) handleExpect(ctx context.Context, session *Session, expect provider.MobileExpectExec) error {
	for _, v := range expect.Visible {
		if err := p.waitForVisibility(ctx, session, v, true); err != nil {
			return err
		}
	}

	for _, v := range expect.NotVisible {
		if err := p.waitForVisibility(ctx, session, v, false); err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) waitForVisibility(ctx context.Context, session *Session, v provider.MobileVisibilityExec, want bool) error {
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = expectDefaultTimeout
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		match, err := pollVisibility(deadlineCtx, session, v.ID, want)
		if err == nil && match {
			return nil
		}

		select {
		case <-deadlineCtx.Done():
			return formatVisibilityError(v, want, err)
		case <-time.After(expectPollInterval):
		}
	}
}

func pollVisibility(ctx context.Context, session *Session, id string, want bool) (bool, error) {
	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID)
	if err != nil {
		return false, fmt.Errorf("fetch hierarchy: %w", err)
	}

	node, ok, err := tree.FindByID(hierarchy, id)
	if err != nil {
		return false, fmt.Errorf("find element: %w", err)
	}

	visible := ok && tree.IsVisible(node)
	if want {
		return visible, nil
	}

	return !visible, nil
}

func formatVisibilityError(v provider.MobileVisibilityExec, want bool, lastErr error) error {
	kind := "visible"
	if !want {
		kind = "not_visible"
	}

	timeout := v.Timeout
	if timeout <= 0 {
		timeout = expectDefaultTimeout
	}

	if lastErr != nil {
		return fmt.Errorf("expect %s id %q timed out after %s: %w", kind, v.ID, timeout, lastErr)
	}

	return fmt.Errorf("expect %s id %q timed out after %s", kind, v.ID, timeout)
}

func (p *Provider) writeFailureArtifacts(ctx context.Context, input provider.Input, session *Session, output *provider.Output) {
	dir := artifactDir(p.artifactsBase, input.Scenario, stepName(input))
	artifacts := make([]Artifact, 0, 2)

	if hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID); err == nil {
		p.recordHierarchy(input.Scenario, stepName(input), hierarchy)

		if a, werr := writeHierarchy(dir, hierarchy); werr == nil {
			artifacts = append(artifacts, a)
		}
	} else if last := p.LastHierarchy(input.Scenario, stepName(input)); last != nil {
		if a, werr := writeHierarchy(dir, last); werr == nil {
			artifacts = append(artifacts, a)
		}
	}

	if png, err := session.Driver.Screenshot(ctx); err == nil {
		if a, werr := writeScreenshot(dir, png); werr == nil {
			artifacts = append(artifacts, a)
		}
	}

	if len(artifacts) == 0 {
		return
	}

	values := make([]cty.Value, 0, len(artifacts))
	for _, a := range artifacts {
		values = append(values, cty.ObjectVal(map[string]cty.Value{
			"type": cty.StringVal(a.Type),
			"path": cty.StringVal(a.Path),
		}))
	}

	output.Response["artifacts"] = cty.ListVal(values)
}

func stepName(input provider.Input) string {
	if input.Step == nil {
		return unnamedSegment
	}

	return input.Step.Name
}

// defaultSessionBuilder returns a builder that errors out clearly. Callers
// who want real Apple lifecycle must use NewApple() instead of New().
func defaultSessionBuilder() SessionBuilder {
	return SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
		return nil, errors.New("mobile: no session builder configured; call mobile.NewApple() or pass mobile.WithSessionBuilder()")
	})
}
