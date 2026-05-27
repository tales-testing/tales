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

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/mobile/apple"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/tales-testing/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

const (
	artifactTypeKey = "type"
	artifactPathKey = "path"
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

// Gesture defaults applied when a swipe / scroll / long_press action omits
// the corresponding attribute.
const (
	// defaultGestureDistance is the swipe/scroll travel as a fraction of
	// the target element's relevant dimension.
	defaultGestureDistance = 0.6
	// defaultSwipeDuration is the swipe / scroll gesture duration.
	defaultSwipeDuration = 300 * time.Millisecond
	// defaultLongPressDuration is how long long_press holds the finger.
	defaultLongPressDuration = 1 * time.Second
)

// Swipe / scroll travel directions.
const (
	directionUp    = "up"
	directionDown  = "down"
	directionLeft  = "left"
	directionRight = "right"
)

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
	captureMode   CaptureMode
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

// WithCaptureMode overrides the screenshot/hierarchy capture mode. The
// default is CaptureFailures, which matches the pre-visual-report behavior.
func WithCaptureMode(mode CaptureMode) Option {
	return func(p *Provider) {
		p.captureMode = mode
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
		captureMode:   CaptureFailures,
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
				artifactTypeKey: cty.StringVal(a.Type),
				artifactPathKey: cty.StringVal(a.Path),
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
		if err := p.handleLaunch(ctx, session, exec.Launch, exec.Permissions); err != nil {
			return fmt.Errorf("launch: %w", err)
		}
	} else if len(exec.Permissions) > 0 {
		if err := applyPermissions(ctx, session, exec.Permissions); err != nil {
			return fmt.Errorf("permissions: %w", err)
		}
	}

	stepDir := artifactDir(p.artifactsBase, inputFile(input), input.Scenario, stepName(input), inputPhase(input), inputAttempt(input))

	results, actionErr := p.runActionLoop(ctx, session, stepDir, exec.Actions)
	if actionErr == nil && p.captureMode == CaptureSteps {
		if endResult, ok := p.captureStepEnd(ctx, session, stepDir, len(results)); ok {
			results = append(results, endResult)
		}
	}

	output.ActionResults = results
	output.Response["target"] = cty.StringVal(session.Target.Name)
	output.Response["bundle_id"] = cty.StringVal(session.Target.BundleID)

	if actionErr != nil {
		return actionErr
	}

	return p.finalizeStep(ctx, session, exec, input)
}

// runActionLoop executes every queued action sequentially, building one
// ActionResult per attempt. On failure it captures a best-effort screenshot
// (when the mode allows) and records every remaining action as skipped so the
// visual timeline shows what was queued but never ran.
func (p *Provider) runActionLoop(ctx context.Context, session *Session, stepDir string, actions []provider.MobileActionExec) ([]provider.ActionResult, error) {
	results := make([]provider.ActionResult, 0, len(actions))

	for i, action := range actions {
		started := time.Now()
		result := initialActionResult(i, action, started)

		err := p.handleAction(ctx, session, action)
		result.Duration = time.Since(started)

		if err != nil {
			result.Status = actionStatusFail
			result.Err = err

			p.captureForAction(ctx, session, stepDir, &result, true)
			results = append(results, result)

			results = appendSkippedActions(results, actions[i+1:], i+1)

			return results, fmt.Errorf("action %d (%s id=%q): %w", i, action.Kind, action.ID, err)
		}

		result.Status = actionStatusPass

		p.captureForAction(ctx, session, stepDir, &result, false)
		results = append(results, result)
	}

	return results, nil
}

// finalizeStep runs the post-action work: expectations, the end-of-step
// hierarchy snapshot used by capture functions, and the optional terminate
// directive. Extracted from executeMobile to keep its cyclomatic complexity
// within the project lint budget.
func (p *Provider) finalizeStep(ctx context.Context, session *Session, exec *provider.MobileExecution, input provider.Input) error {
	if exec.Expect.HasAny() {
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

	return nil
}

// initialActionResult builds the partial result that wraps one queued mobile
// action. The Value field is masked at this single boundary: every consumer
// downstream (visual report, JSONL action events, console summary) reads from
// this struct without re-masking.
func initialActionResult(index int, action provider.MobileActionExec, started time.Time) provider.ActionResult {
	result := provider.ActionResult{
		Index:      index,
		Kind:       string(action.Kind),
		SelectorID: action.ID,
		Secure:     action.Secure,
		StartedAt:  started,
	}

	if action.Value != "" {
		if action.Secure {
			result.Value = "***"
		} else {
			result.Value = action.Value
		}
	}

	result.Label = actionLabel(action.Kind, action.ID, result.Value, action.Secure)

	return result
}

// appendSkippedActions records a "skipped" entry for every action that did
// not run because an earlier action failed. The timeline can render them
// grayed out so the user can see what was queued but never executed.
func appendSkippedActions(results []provider.ActionResult, remaining []provider.MobileActionExec, startIndex int) []provider.ActionResult {
	for offset, action := range remaining {
		idx := startIndex + offset
		skipped := initialActionResult(idx, action, time.Time{})
		skipped.Status = actionStatusSkip
		results = append(results, skipped)
	}

	return results
}

// captureForAction writes the per-action screenshot and hierarchy when the
// capture mode requires it. forFailure is true when called from the action
// failure path (best-effort capture even in CaptureSteps mode); on success
// the capture only happens in CaptureActions mode.
//
// Capture errors are intentionally swallowed: they must not mask the
// action's own status. The action result simply omits the
// screenshot/hierarchy paths in that case.
func (p *Provider) captureForAction(ctx context.Context, session *Session, stepDir string, result *provider.ActionResult, forFailure bool) {
	if p.captureMode == CaptureNone {
		return
	}

	if p.captureMode == CaptureFailures {
		return
	}

	if !forFailure && p.captureMode != CaptureActions {
		return
	}

	dir := actionArtifactDir(stepDir, result.Index, result.Kind, result.SelectorID)

	if png, err := session.Driver.Screenshot(ctx); err == nil {
		if a, werr := writeScreenshot(dir, png); werr == nil {
			result.Screenshot = a.Path
		}
	} else if a, werr := writeScreenshotFallback(ctx, dir, session); werr == nil {
		result.Screenshot = a.Path
	}

	if hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID); err == nil {
		if a, werr := writeHierarchy(dir, hierarchy); werr == nil {
			result.Hierarchy = a.Path
		}
	}
}

// captureStepEnd produces a synthetic "step_end" ActionResult that carries
// the end-of-step screenshot and hierarchy. Used only by CaptureSteps mode.
func (p *Provider) captureStepEnd(ctx context.Context, session *Session, stepDir string, index int) (provider.ActionResult, bool) {
	dir := stepLevelArtifactDir(stepDir)
	result := provider.ActionResult{
		Index:     index,
		Kind:      "step_end",
		Label:     "Step end",
		Status:    actionStatusPass,
		StartedAt: time.Now(),
	}

	captured := false

	if png, err := session.Driver.Screenshot(ctx); err == nil {
		if a, werr := writeScreenshot(dir, png); werr == nil {
			result.Screenshot = a.Path

			captured = true
		}
	} else if a, werr := writeScreenshotFallback(ctx, dir, session); werr == nil {
		result.Screenshot = a.Path
		captured = true
	}

	if hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.BundleID); err == nil {
		if a, werr := writeHierarchy(dir, hierarchy); werr == nil {
			result.Hierarchy = a.Path

			captured = true
		}
	}

	if !captured {
		return provider.ActionResult{}, false
	}

	return result, true
}

// secureTextFieldType identifies SwiftUI SecureField in the normalized
// view tree. iOS handles these specially: typing surfaces an autofill
// QuickType bar that intercepts keystrokes, and clearing an empty one
// can leak deletes across the strong-password group.
const secureTextFieldType = "secure_text_field"

// usePasteInput reports whether an input_text action should use the
// id-targeted driver input route instead of typing into the currently
// focused element. SwiftUI SecureField(.newPassword) inputs surface an
// autofill QuickType bar that intercepts the first keystrokes; the
// id-targeted route taps the field to focus it and feeds the text
// through the private XCTest event-synthesis pipeline, which bypasses
// the input listener the banner hooks into.
func usePasteInput(node *tree.ViewNode) bool {
	if node == nil {
		return false
	}

	return node.Type == secureTextFieldType
}

var actionLabels = map[model.MobileActionKind]string{
	model.MobileActionTap:            "Tap",
	model.MobileActionDoubleTap:      "Double tap",
	model.MobileActionLongPress:      "Long press",
	model.MobileActionInputText:      "Input text",
	model.MobileActionClearText:      "Clear text",
	model.MobileActionSwipe:          "Swipe",
	model.MobileActionScroll:         "Scroll",
	model.MobileActionPressKey:       "Press key",
	model.MobileActionPressButton:    "Press button",
	model.MobileActionSetOrientation: "Set orientation",
	model.MobileActionWaitVisible:    "Wait visible",
	model.MobileActionWaitNotVisible: "Wait not visible",
}

func actionLabel(kind model.MobileActionKind, id, maskedValue string, secure bool) string {
	verb, ok := actionLabels[kind]
	if !ok {
		verb = string(kind)
	}

	switch {
	case id == "" && maskedValue == "":
		return verb
	case maskedValue == "":
		return fmt.Sprintf("%s %s", verb, id)
	case secure:
		return fmt.Sprintf("%s %s ***", verb, id)
	default:
		return fmt.Sprintf("%s %s %q", verb, id, maskedValue)
	}
}

func (p *Provider) handleLaunch(ctx context.Context, session *Session, launch *provider.MobileLaunchExec, permissions []provider.MobilePermissionExec) error {
	if launch.ClearState {
		if err := session.Lifecycle.ClearAppState(ctx, session.UDID, session.Target); err != nil {
			return fmt.Errorf("clear state: %w", err)
		}
	} else if err := session.Lifecycle.InstallApp(ctx, session.UDID, session.Target); err != nil {
		return fmt.Errorf("install app: %w", err)
	}

	// Privacy permissions are applied while the app is installed but
	// before it launches, so the app sees the granted state on first run.
	if err := applyPermissions(ctx, session, permissions); err != nil {
		return err
	}

	if err := session.Lifecycle.LaunchApp(ctx, session.UDID, session.Target); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	return nil
}

// applyPermissions grants or revokes each declared privacy permission
// through simctl.
func applyPermissions(ctx context.Context, session *Session, permissions []provider.MobilePermissionExec) error {
	for _, permission := range permissions {
		if err := session.Lifecycle.SetPermission(ctx, session.UDID, session.Target, permission.Action, permission.Service); err != nil {
			return fmt.Errorf("%s %s: %w", permission.Action, permission.Service, err)
		}
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

	// Device-level actions target no element, so they skip the
	// wait-for-element step entirely.
	if handled, err := handleDeviceAction(ctx, session, action); handled {
		return err
	}

	node, err := p.waitForActionElement(ctx, session, action)
	if err != nil {
		return err
	}

	switch action.Kind {
	case model.MobileActionTap:
		return executeTap(ctx, session, node)
	case model.MobileActionDoubleTap:
		return executeDoubleTap(ctx, session, node)
	case model.MobileActionLongPress:
		return executeLongPress(ctx, session, action, node)
	case model.MobileActionInputText:
		return executeInputText(ctx, session, action, node)
	case model.MobileActionClearText:
		return executeClearText(ctx, session, node)
	case model.MobileActionSwipe:
		return executeSwipe(ctx, session, action, node, false)
	case model.MobileActionScroll:
		return executeSwipe(ctx, session, action, node, true)
	case model.MobileActionWaitVisible, model.MobileActionWaitNotVisible,
		model.MobileActionPressKey, model.MobileActionPressButton, model.MobileActionSetOrientation:
		// Handled before element resolution (wait_* and the device-level
		// actions); never reached here.
		return nil
	default:
		return fmt.Errorf("unsupported action kind %q", action.Kind)
	}
}

// handleDeviceAction runs the device-level actions (press_key,
// press_button, set_orientation) that operate on the device rather than
// an element. The first return value reports whether the action kind was
// a device action; when false the caller continues with element-based
// handling.
func handleDeviceAction(ctx context.Context, session *Session, action provider.MobileActionExec) (bool, error) {
	if action.Kind == model.MobileActionPressKey {
		if err := session.Driver.PressKey(ctx, session.Target.BundleID, action.Value); err != nil {
			return true, fmt.Errorf("press key: %w", err)
		}

		return true, nil
	}

	if action.Kind == model.MobileActionPressButton {
		if err := session.Driver.PressButton(ctx, session.Target.BundleID, action.Value); err != nil {
			return true, fmt.Errorf("press button: %w", err)
		}

		return true, nil
	}

	if action.Kind == model.MobileActionSetOrientation {
		if err := session.Driver.SetOrientation(ctx, action.Value); err != nil {
			return true, fmt.Errorf("set orientation: %w", err)
		}

		return true, nil
	}

	return false, nil
}

func executeTap(ctx context.Context, session *Session, node *tree.ViewNode) error {
	x, y := tree.Center(node)
	if err := session.Driver.Tap(ctx, session.Target.BundleID, node.ID, x, y); err != nil {
		return fmt.Errorf("tap: %w", err)
	}

	return nil
}

func executeDoubleTap(ctx context.Context, session *Session, node *tree.ViewNode) error {
	x, y := tree.Center(node)
	if err := session.Driver.DoubleTap(ctx, session.Target.BundleID, node.ID, x, y); err != nil {
		return fmt.Errorf("double tap: %w", err)
	}

	return nil
}

func executeLongPress(ctx context.Context, session *Session, action provider.MobileActionExec, node *tree.ViewNode) error {
	x, y := tree.Center(node)

	duration := action.Duration
	if duration <= 0 {
		duration = defaultLongPressDuration
	}

	if err := session.Driver.LongPress(ctx, session.Target.BundleID, node.ID, x, y, duration.Seconds()); err != nil {
		return fmt.Errorf("long press: %w", err)
	}

	return nil
}

func executeInputText(ctx context.Context, session *Session, action provider.MobileActionExec, node *tree.ViewNode) error {
	if usePasteInput(node) {
		if err := session.Driver.InputText(ctx, session.Target.BundleID, node.ID, action.Value, true); err != nil {
			return fmt.Errorf("input text: %w", err)
		}

		return nil
	}

	x, y := tree.Center(node)
	if err := session.Driver.Tap(ctx, session.Target.BundleID, node.ID, x, y); err != nil {
		return fmt.Errorf("focus element: %w", err)
	}

	if err := session.Driver.InputText(ctx, session.Target.BundleID, node.ID, action.Value, false); err != nil {
		return fmt.Errorf("input text: %w", err)
	}

	return nil
}

func executeClearText(ctx context.Context, session *Session, node *tree.ViewNode) error {
	count := len([]rune(tree.Value(node)))

	// SecureField on iOS exposes its value as one "•" per typed
	// character. An empty value therefore means the field is truly
	// empty and no delete keys are required. Sending the default
	// fallback in that case leaks deletes via app.typeText, which on
	// .newPassword fields routes through the iOS strong-password
	// group and erases a sibling SecureField instead of the targeted
	// (already empty) one.
	if count == 0 && node.Type == secureTextFieldType {
		return nil
	}

	x, y := tree.Center(node)
	if err := session.Driver.Tap(ctx, session.Target.BundleID, node.ID, x, y); err != nil {
		return fmt.Errorf("focus element: %w", err)
	}

	if count == 0 {
		count = defaultClearTextErase
	}

	if err := session.Driver.EraseText(ctx, session.Target.BundleID, count); err != nil {
		return fmt.Errorf("erase text: %w", err)
	}

	return nil
}

// executeSwipe backs both the swipe and scroll actions. For swipe,
// action.Direction is the finger travel direction. For scroll
// (invert == true) action.Direction is the content direction the author
// wants to reveal, so the finger travels the opposite way.
func executeSwipe(ctx context.Context, session *Session, action provider.MobileActionExec, node *tree.ViewNode, invert bool) error {
	fingerDir := action.Direction
	if invert {
		fingerDir = oppositeDirection(action.Direction)
	}

	startX, startY, endX, endY, err := gestureVector(node, fingerDir, action.Distance)
	if err != nil {
		return fmt.Errorf("%s: %w", action.Kind, err)
	}

	duration := action.Duration
	if duration <= 0 {
		duration = defaultSwipeDuration
	}

	if err := session.Driver.Swipe(ctx, session.Target.BundleID, startX, startY, endX, endY, duration.Seconds()); err != nil {
		return fmt.Errorf("%s: %w", action.Kind, err)
	}

	return nil
}

// gestureVector computes the screen-space start/end points of a finger
// drag in fingerDir across node, traveling `distance` (a fraction of
// the element's relevant dimension; defaults applied when <= 0).
func gestureVector(node *tree.ViewNode, fingerDir string, distance float64) (startX, startY, endX, endY float64, err error) {
	if distance <= 0 {
		distance = defaultGestureDistance
	}

	centerX, centerY := tree.Center(node)

	switch fingerDir {
	case directionUp:
		travel := node.Bounds.Height * distance

		return centerX, centerY + travel/2, centerX, centerY - travel/2, nil
	case directionDown:
		travel := node.Bounds.Height * distance

		return centerX, centerY - travel/2, centerX, centerY + travel/2, nil
	case directionLeft:
		travel := node.Bounds.Width * distance

		return centerX + travel/2, centerY, centerX - travel/2, centerY, nil
	case directionRight:
		travel := node.Bounds.Width * distance

		return centerX - travel/2, centerY, centerX + travel/2, centerY, nil
	default:
		return 0, 0, 0, 0, fmt.Errorf("invalid direction %q (expected up, down, left or right)", fingerDir)
	}
}

func oppositeDirection(direction string) string {
	switch direction {
	case directionUp:
		return directionDown
	case directionDown:
		return directionUp
	case directionLeft:
		return directionRight
	case directionRight:
		return directionLeft
	default:
		return direction
	}
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
	// CaptureNone is strict: skip every screenshot/hierarchy capture, even
	// on failure. The driver_log artifact is still surfaced from
	// Execute() because it is the only diagnostic for a driver that never
	// starts.
	if p.captureMode == CaptureNone {
		return
	}

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
			artifactTypeKey: cty.StringVal(a.Type),
			artifactPathKey: cty.StringVal(a.Path),
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
