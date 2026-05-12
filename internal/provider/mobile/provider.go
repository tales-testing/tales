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

	"github.com/hyperxlab/tales/internal/assertion"
	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

// defaultPollInterval is the wait between two hierarchy fetches during mobile
// action and expectation polling.
const defaultPollInterval = 250 * time.Millisecond

// expectDefaultTimeout is used when a visibility block omits `timeout`.
const expectDefaultTimeout = 10 * time.Second

// actionDefaultTimeout is used when tap/input_text/clear_text omit `timeout`.
const actionDefaultTimeout = 10 * time.Second

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
	stepLocks   map[string]*sync.Mutex
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
		stepLocks:     map[string]*sync.Mutex{},
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

	stepLock := p.stepLock(target.Name)
	stepLock.Lock()
	defer stepLock.Unlock()

	start := time.Now()
	output := &provider.Output{
		Request:  mobileRequestCty(input.Mobile),
		Response: map[string]cty.Value{},
	}

	session, err := p.acquireSession(ctx, target)
	if err != nil {
		if a, ok := driverLogArtifactFromError(err); ok {
			output.Response["artifacts"] = cty.ListVal([]cty.Value{cty.ObjectVal(map[string]cty.Value{
				"type": cty.StringVal(a.Type),
				"path": cty.StringVal(a.Path),
			})})
		}

		output.Duration = time.Since(start)

		return output, fmt.Errorf("acquire session: %w", err)
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

func (p *Provider) stepLock(name string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	lock, ok := p.stepLocks[name]
	if !ok {
		lock = &sync.Mutex{}
		p.stepLocks[name] = lock
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

	if len(exec.Expect.Visible) > 0 || len(exec.Expect.NotVisible) > 0 ||
		len(exec.Expect.Text) > 0 || len(exec.Expect.Value) > 0 ||
		len(exec.Expect.Enabled) > 0 || len(exec.Expect.Disabled) > 0 {
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
	if action.Kind == model.MobileActionWaitVisible {
		return p.waitForVisibility(ctx, session, provider.MobileVisibilityExec{ID: action.ID, Timeout: action.Timeout, Interval: action.Interval}, true)
	}

	if action.Kind == model.MobileActionWaitNotVisible {
		return p.waitForVisibility(ctx, session, provider.MobileVisibilityExec{ID: action.ID, Timeout: action.Timeout, Interval: action.Interval}, false)
	}

	node, err := p.waitForActionElement(ctx, session, action)
	if err != nil {
		return err
	}

	x, y := tree.Center(node)

	switch action.Kind {
	case model.MobileActionTap:
		if err := session.Driver.Tap(ctx, session.Target.BundleID, x, y); err != nil {
			return fmt.Errorf("tap: %w", err)
		}
	case model.MobileActionInputText:
		if err := session.Driver.Tap(ctx, session.Target.BundleID, x, y); err != nil {
			return fmt.Errorf("focus element: %w", err)
		}

		if err := session.Driver.InputText(ctx, session.Target.BundleID, action.Value); err != nil {
			return fmt.Errorf("input text: %w", err)
		}
	case model.MobileActionClearText:
		if err := session.Driver.Tap(ctx, session.Target.BundleID, x, y); err != nil {
			return fmt.Errorf("focus element: %w", err)
		}

		count := len([]rune(tree.Value(node)))
		if count == 0 {
			count = defaultClearTextErase
		}

		if err := session.Driver.EraseText(ctx, session.Target.BundleID, count); err != nil {
			return fmt.Errorf("erase text: %w", err)
		}
	case model.MobileActionWaitVisible, model.MobileActionWaitNotVisible:
		return nil
	default:
		return fmt.Errorf("unsupported action kind %q", action.Kind)
	}

	return nil
}

func (p *Provider) waitForActionElement(ctx context.Context, session *Session, action provider.MobileActionExec) (*tree.ViewNode, error) {
	opts := pollOptions(action.Timeout, action.Interval, actionDefaultTimeout)

	var found *tree.ViewNode

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElementByID(pollCtx, session, action.ID)
		if err != nil {
			return pollResult{}, err
		}

		if ok && tree.IsVisible(node) {
			found = node

			return pollResult{Done: true}, nil
		}

		return pollResult{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("element %q was not visible after %s: %w", action.ID, opts.Timeout, err)
	}

	return found, nil
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

	for _, v := range expect.Text {
		if err := p.waitForText(ctx, session, v); err != nil {
			return err
		}
	}

	for _, v := range expect.Value {
		if err := p.waitForValue(ctx, session, v); err != nil {
			return err
		}
	}

	for _, v := range expect.Enabled {
		if err := p.waitForEnabled(ctx, session, v, true); err != nil {
			return err
		}
	}

	for _, v := range expect.Disabled {
		if err := p.waitForEnabled(ctx, session, v, false); err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) waitForVisibility(ctx context.Context, session *Session, v provider.MobileVisibilityExec, want bool) error {
	opts := pollOptions(v.Timeout, v.Interval, expectDefaultTimeout)

	var found bool

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElementByID(pollCtx, session, v.ID)
		if err != nil {
			return pollResult{}, err
		}

		if ok {
			found = true
		}

		visible := ok && tree.IsVisible(node)
		if want {
			return pollResult{Done: visible}, nil
		}

		return pollResult{Done: !visible}, nil
	})
	if err == nil {
		return nil
	}

	if want {
		if !found {
			return fmt.Errorf("element %q not found after %s: %w", v.ID, opts.Timeout, err)
		}

		return fmt.Errorf("element %q was not visible after %s: %w", v.ID, opts.Timeout, err)
	}

	return fmt.Errorf("element %q was still visible after %s: %w", v.ID, opts.Timeout, err)
}

func (p *Provider) waitForText(ctx context.Context, session *Session, v provider.MobileValueExpectationExec) error {
	return p.waitForNodeValue(ctx, session, v, "text", tree.Text)
}

func (p *Provider) waitForValue(ctx context.Context, session *Session, v provider.MobileValueExpectationExec) error {
	return p.waitForNodeValue(ctx, session, v, "value", tree.Value)
}

func (p *Provider) waitForNodeValue(ctx context.Context, session *Session, v provider.MobileValueExpectationExec, kind string, extract func(*tree.ViewNode) string) error {
	opts := pollOptions(v.Timeout, v.Interval, expectDefaultTimeout)

	var (
		got   string
		found bool
	)

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElementByID(pollCtx, session, v.ID)
		if err != nil {
			return pollResult{}, err
		}

		if !ok {
			return pollResult{}, nil
		}

		found = true
		got = extract(node)

		res := pollResult{Done: true}
		if mismatch := assertion.Equal(kind+"."+v.ID, v.Expected, cty.StringVal(got)); mismatch != nil {
			res = pollResult{Mismatch: mismatch}
		}

		return res, nil
	})
	if err == nil {
		return nil
	}

	if !found {
		return fmt.Errorf("element %q not found after %s: %w", v.ID, opts.Timeout, err)
	}

	want := diagnostic.ScalarString(v.Expected)

	return fmt.Errorf("%s mismatch for %q after %s: want=%q got=%q: %w", kind, v.ID, opts.Timeout, want, got, err)
}

func (p *Provider) waitForEnabled(ctx context.Context, session *Session, v provider.MobileStateExpectationExec, want bool) error {
	opts := pollOptions(v.Timeout, v.Interval, expectDefaultTimeout)

	var (
		found    bool
		lastSeen bool
	)

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElementByID(pollCtx, session, v.ID)
		if err != nil {
			return pollResult{}, err
		}

		if !ok {
			return pollResult{}, nil
		}

		found = true
		lastSeen = node.Enabled

		if node.Enabled == want {
			return pollResult{Done: true}, nil
		}

		return pollResult{Mismatch: fmt.Errorf("element %q enabled=%t, want=%t", v.ID, node.Enabled, want)}, nil
	})
	if err == nil {
		return nil
	}

	if !found {
		return fmt.Errorf("element %q not found after %s: %w", v.ID, opts.Timeout, err)
	}

	state := "enabled"
	if !want {
		state = "disabled"
	}

	return fmt.Errorf("element %q was not %s after %s (last seen enabled=%t): %w", v.ID, state, opts.Timeout, lastSeen, err)
}

// PollOptions configures a single poll() invocation.
type PollOptions struct {
	Timeout  time.Duration
	Interval time.Duration
}

func pollOptions(timeout, interval, defaultTimeout time.Duration) PollOptions {
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	if interval <= 0 {
		interval = defaultPollInterval
	}

	return PollOptions{Timeout: timeout, Interval: interval}
}

// pollResult lets a poll callback distinguish "found but not matching yet" (Mismatch)
// from "definitely done" (Done) without conflating either with a transient fatal error.
type pollResult struct {
	Done     bool
	Mismatch error
}

// poll invokes fn repeatedly until it reports Done, the context expires, or fn
// returns a fatal error from outside the matcher pipeline.
//
// Transient fatal errors (e.g. driver / hierarchy fetch hiccups) are recorded
// as lastErr and the loop keeps polling; on timeout, lastMismatch wins over
// lastErr so matcher-specific messages survive into the final error.
//
// The poll interval reuses a single time.Ticker so frequent polling does not
// allocate a new timer per iteration.
func poll(ctx context.Context, opts PollOptions, fn func(context.Context) (pollResult, error)) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	var (
		lastErr      error
		lastMismatch error
	)

	for {
		res, err := fn(deadlineCtx)

		switch {
		case err != nil:
			lastErr = err
		case res.Done:
			return nil
		case res.Mismatch != nil:
			lastMismatch = res.Mismatch
		}

		select {
		case <-deadlineCtx.Done():
			if lastMismatch != nil {
				return lastMismatch
			}

			if lastErr != nil {
				return lastErr
			}

			return fmt.Errorf("poll timed out: %w", deadlineCtx.Err())
		case <-ticker.C:
		}
	}
}

func findElementByID(ctx context.Context, session *Session, id string) (*tree.ViewNode, bool, error) {
	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch hierarchy: %w", err)
	}

	node, ok, err := tree.FindByID(hierarchy, id)
	if err != nil {
		return nil, false, fmt.Errorf("find element: %w", err)
	}

	return node, ok, nil
}

func (p *Provider) writeFailureArtifacts(ctx context.Context, input provider.Input, session *Session, output *provider.Output) {
	dir := artifactDir(p.artifactsBase, inputFile(input), input.Scenario, stepName(input), inputPhase(input), inputAttempt(input))
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
	} else if a, werr := writeScreenshotFallback(ctx, dir, session); werr == nil {
		artifacts = append(artifacts, a)
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

// mobileRequestCty produces a cty map describing the mobile step request so
// downstream steps can reference `result.<step>.request.*` (platform, target,
// launch.clear_state, terminate, actions). Secure action values are masked.
func mobileRequestCty(exec *provider.MobileExecution) map[string]cty.Value {
	if exec == nil {
		return map[string]cty.Value{}
	}

	out := map[string]cty.Value{
		"platform": cty.StringVal(exec.Platform),
		"target":   cty.StringVal(exec.TargetName),
	}

	if exec.Launch != nil {
		out["launch"] = cty.ObjectVal(map[string]cty.Value{
			"clear_state": cty.BoolVal(exec.Launch.ClearState),
		})
	}

	if exec.Terminate != nil {
		out["terminate"] = cty.BoolVal(true)
	}

	if len(exec.Actions) > 0 {
		actions := make([]cty.Value, 0, len(exec.Actions))

		for _, action := range exec.Actions {
			actions = append(actions, cty.ObjectVal(mobileActionCty(action)))
		}

		out["actions"] = cty.TupleVal(actions)
	}

	return out
}

func mobileActionCty(action provider.MobileActionExec) map[string]cty.Value {
	entry := map[string]cty.Value{
		"kind": cty.StringVal(string(action.Kind)),
		"id":   cty.StringVal(action.ID),
	}
	if action.Timeout > 0 {
		entry["timeout"] = cty.StringVal(action.Timeout.String())
	}

	if action.Interval > 0 {
		entry["interval"] = cty.StringVal(action.Interval.String())
	}

	if action.Value == "" {
		return entry
	}

	if action.Secure {
		entry["value"] = cty.StringVal("***")
	} else {
		entry["value"] = cty.StringVal(action.Value)
	}

	return entry
}

func stepName(input provider.Input) string {
	if input.Step == nil {
		return unnamedSegment
	}

	return input.Step.Name
}

func inputFile(input provider.Input) string {
	if input.Step == nil {
		return ""
	}

	return input.Step.File
}

func inputPhase(input provider.Input) string {
	if input.Phase == "" {
		return "step"
	}

	return input.Phase
}

func inputAttempt(input provider.Input) int {
	if input.Attempt <= 0 {
		return 1
	}

	return input.Attempt
}

func driverLogArtifactFromError(err error) (Artifact, bool) {
	var startErr *xcodebuild.StartError
	if !errors.As(err, &startErr) || startErr.LogPath == "" {
		return Artifact{}, false
	}

	return Artifact{Type: "driver_log", Path: startErr.LogPath}, true
}

// defaultSessionBuilder returns a builder that errors out clearly. Callers
// who want real Apple lifecycle must use NewApple() instead of New().
func defaultSessionBuilder() SessionBuilder {
	return SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
		return nil, errors.New("mobile: no session builder configured; call mobile.NewApple() or pass mobile.WithSessionBuilder()")
	})
}
