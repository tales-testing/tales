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
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
	"github.com/tales-testing/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

const (
	artifactTypeKey = "type"
	artifactPathKey = "path"
)

// Artifact type strings, surfaced verbatim in the visual / JSONL reports
// and the cty `request.artifacts[*].type` namespace. Exported because
// platform backends build the Diagnostics list themselves and must label
// their files with the same vocabulary the reporters render.
const (
	// ArtifactTypeDriverLog is the driver process' stdout+stderr capture.
	ArtifactTypeDriverLog = "driver_log"
	// ArtifactTypeXCResultDir is the directory holding Xcode's .xcresult
	// bundle for the driver test session (iOS).
	ArtifactTypeXCResultDir = "xcresult_dir"
	// ArtifactTypeDriverBuildLog is the log of the driver's own build,
	// relevant when the driver died because the build was broken.
	ArtifactTypeDriverBuildLog = "driver_build_log"
	// ArtifactTypeLogcat is a device log dump, captured next to the
	// failure screenshot and hierarchy on platforms that can produce one
	// (Android). It answers what the system was doing, which the other
	// two cannot: an ANR, a crash and a slow start all look alike on a
	// screenshot.
	ArtifactTypeLogcat = "logcat"
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

// Provider is the mobile step provider.
type Provider struct {
	mu          sync.Mutex
	sessions    map[string]*Session
	targetLocks map[string]*sync.Mutex
	stepLocks   map[string]*sync.Mutex
	// backends maps a platform name onto the builder that knows how to
	// stand a session up for it. A step's `platform` selects the entry.
	backends map[string]SessionBuilder

	hierarchyMu sync.RWMutex
	hierarchies map[string]*tree.ViewNode

	artifactsBase string
	captureMode   CaptureMode

	// recorderFactory builds the Recorder used by the scenario-level
	// record hook. Registered by the platform backend; nil means the
	// platform cannot record and a record block fails with that message.
	recorderFactory RecorderFactory
	recordOnce      sync.Once
	recording       *recordController
}

// Option configures the Provider.
type Option func(*Provider)

// WithBackend registers the session builder handling one platform. The
// CLI registers every compiled-in backend; tests register a fake for the
// single platform they exercise.
func WithBackend(platform string, b SessionBuilder) Option {
	return func(p *Provider) {
		p.backends[platform] = b
	}
}

// WithSessionBuilder registers b for iOS. Kept as a thin shorthand over
// WithBackend for the many call sites that only ever drive one platform.
func WithSessionBuilder(b SessionBuilder) Option {
	return WithBackend(PlatformIOS, b)
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

// WithRecorderFactory sets the factory backing the scenario-level record
// block. Platform backends register their own; tests inject a fake so
// unit coverage does not require a device.
func WithRecorderFactory(factory RecorderFactory) Option {
	return func(p *Provider) {
		p.recorderFactory = factory
	}
}

// New returns a Provider with a default real-Apple session builder.
func New(opts ...Option) *Provider {
	p := &Provider{
		sessions:      map[string]*Session{},
		targetLocks:   map[string]*sync.Mutex{},
		stepLocks:     map[string]*sync.Mutex{},
		backends:      map[string]SessionBuilder{},
		hierarchies:   map[string]*tree.ViewNode{},
		artifactsBase: artifacts.DefaultBase,
		captureMode:   CaptureFailures,
	}

	for _, opt := range opts {
		opt(p)
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

	target, err := ResolveTarget(input.Config, input.Mobile.TargetName)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}

	// The step's platform and the target's must agree: the step selects
	// the backend, the target configures the device, and a mismatch would
	// silently drive the wrong one.
	if input.Mobile.Platform != target.Platform {
		return nil, fmt.Errorf("step declares platform %q but target %q is configured for %q",
			input.Mobile.Platform, target.Name, target.Platform)
	}

	stepLock := p.stepLock(sessionKey(target))
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

	// Best-effort: start the scenario-level recording on the first mobile
	// step. A failure here is logged via the artifact channel but does not
	// fail the step itself: the user's mobile assertions still take
	// precedence over a recording mishap.
	if recErr := p.maybeStartRecording(ctx, input.Scenario, session); recErr != nil {
		fmt.Fprintf(os.Stderr, "mobile: %v\n", recErr)
	}

	if err := p.executeMobile(ctx, input, session, output); err != nil {
		p.writeFailureArtifacts(ctx, input, session, output)
		output.Duration = time.Since(start)

		// When the failure looks like the XCUITest process died
		// mid-scenario (connect: connection refused, EOF on a POST,
		// broken pipe, ...), append the on-disk paths Tales just
		// allocated for this session so users land on driver.log and
		// the .xcresult bundle directly instead of just seeing a
		// transport-level error from net/http. The matching artifacts
		// also get attached to the step report so the visual / JSONL
		// surfaces can render clickable links to the same files.
		if looksLikeDriverDeath(err) {
			if extras := driverDeathArtifacts(session.Diagnostics); len(extras) > 0 {
				appendArtifactsToOutput(output, extras)
			}

			return output, wrapDriverDeathError(err, session)
		}

		return output, err
	}

	output.Duration = time.Since(start)

	return output, nil
}

// sessionKey namespaces a target by platform. Two targets may share a
// name across platforms (an "app" target for iOS and one for Android),
// and they are different devices with different sessions.
func sessionKey(target Target) string {
	return target.Platform + "\x00" + target.Name
}

// backendFor returns the builder registered for the target's platform.
func (p *Provider) backendFor(target Target) (SessionBuilder, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if b, ok := p.backends[target.Platform]; ok {
		return b, nil
	}

	registered := make([]string, 0, len(p.backends))
	for platform := range p.backends {
		registered = append(registered, platform)
	}

	sort.Strings(registered)

	if len(registered) == 0 {
		return nil, fmt.Errorf("mobile platform %q is not supported: no platform backend is registered", target.Platform)
	}

	return nil, fmt.Errorf("mobile platform %q is not supported (available: %s)",
		target.Platform, strings.Join(registered, ", "))
}

func (p *Provider) acquireSession(ctx context.Context, target Target) (*Session, error) {
	key := sessionKey(target)

	if sess, ok := p.lookupSession(key); ok {
		return sess, nil
	}

	builder, err := p.backendFor(target)
	if err != nil {
		return nil, err
	}

	// Serialize concurrent Build calls per target without blocking other
	// targets: Build can take tens of seconds (booting devices, starting
	// the driver) and we don't want target B to wait on target A.
	lock := p.targetLock(key)
	lock.Lock()
	defer lock.Unlock()

	if sess, ok := p.lookupSession(key); ok {
		return sess, nil
	}

	sess, err := builder.Build(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("build session for %q: %w", target.Name, err)
	}

	p.mu.Lock()
	p.sessions[key] = sess
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
	output.Response["app_id"] = cty.StringVal(session.Target.AppID)
	// bundle_id is the previous spelling, kept so captures written
	// against it keep resolving. It is removed on the same schedule as
	// the config attribute of the same name.
	output.Response["bundle_id"] = cty.StringVal(session.Target.AppID)

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

	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.AppID)
	if err == nil {
		p.recordHierarchy(input.Scenario, input.Step.Name, hierarchy)
	}

	if exec.Terminate != nil {
		// Terminate through the driver (XCUIApplication.terminate()) so
		// XCTest deregisters the app process. Tearing it down out-of-band
		// via simctl would leave XCTest bound to a now-dead process, and the
		// next scenario reusing this session would hang on /hierarchy.
		if err := session.Driver.Terminate(ctx, session.Target.AppID); err != nil {
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

	if hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.AppID); err == nil {
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

	if hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.AppID); err == nil {
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
		if err := session.Lifecycle.ClearAppState(ctx, session.DeviceID, session.Target); err != nil {
			return fmt.Errorf("clear state: %w", err)
		}
	} else if err := session.Lifecycle.InstallApp(ctx, session.DeviceID, session.Target); err != nil {
		return fmt.Errorf("install app: %w", err)
	}

	// Privacy permissions are applied while the app is installed but
	// before it launches, so the app sees the granted state on first run.
	if err := applyPermissions(ctx, session, permissions); err != nil {
		return err
	}

	// Cold-start from the host when the platform can (see HostAppLauncher),
	// then have the driver activate the app. Activate is what re-establishes
	// XCTest's automation session with the freshly launched process; without
	// it, a scenario reusing a cached session would snapshot a stale process
	// and time out on /hierarchy (issue #41). Driving the launch itself from
	// inside the driver solved that too, but made every launch failure a
	// runner-killing XCTest failure.
	if host, ok := session.Lifecycle.(HostAppLauncher); ok {
		if err := host.LaunchApp(ctx, session.DeviceID, session.Target); err != nil {
			return fmt.Errorf("launch app: %w", err)
		}

		if err := session.Driver.Activate(ctx, session.Target.AppID); err != nil {
			return fmt.Errorf("activate app: %w", err)
		}

		return nil
	}

	if err := session.Driver.Launch(ctx, session.Target.AppID); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	return nil
}

// applyPermissions grants or revokes each declared privacy permission
// through simctl.
func applyPermissions(ctx context.Context, session *Session, permissions []provider.MobilePermissionExec) error {
	for _, permission := range permissions {
		if err := session.Lifecycle.SetPermission(ctx, session.DeviceID, session.Target, permission.Action, permission.Service); err != nil {
			return fmt.Errorf("%s %s: %w", permission.Action, permission.Service, err)
		}
	}

	return nil
}

// visibilityFromAction narrows a wait_visible / wait_not_visible action
// into the visibility expectation the shared wait helper consumes.
//
// It exists so the locator is carried across in one place: an earlier
// inline literal only copied ID, silently dropping Label. That made
// `wait_visible { label = "Done" }` poll for an empty id and time out,
// and — worse — made `wait_not_visible { label = ... }` pass for the
// wrong reason, since an unresolvable locator reads as "not visible".
// Any locator field added later must be threaded here too.
func visibilityFromAction(action provider.MobileActionExec) provider.MobileVisibilityExec {
	return provider.MobileVisibilityExec{
		ID:       action.ID,
		Label:    action.Label,
		Text:     action.Text,
		Timeout:  action.Timeout,
		Interval: action.Interval,
	}
}

func (p *Provider) handleAction(ctx context.Context, session *Session, action provider.MobileActionExec) error {
	if action.Kind == model.MobileActionWaitVisible {
		return p.waitForVisibility(ctx, session, visibilityFromAction(action), true, action.First)
	}

	if action.Kind == model.MobileActionWaitNotVisible {
		return p.waitForVisibility(ctx, session, visibilityFromAction(action), false, action.First)
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
		return executeTap(ctx, session, actionLocator(action), node)
	case model.MobileActionDoubleTap:
		return executeDoubleTap(ctx, session, actionLocator(action), node)
	case model.MobileActionLongPress:
		return executeLongPress(ctx, session, action, node)
	case model.MobileActionInputText:
		return executeInputText(ctx, session, action, node)
	case model.MobileActionClearText:
		return executeClearText(ctx, session, actionLocator(action), node)
	case model.MobileActionSwipe:
		return executeSwipe(ctx, session, action, node, false)
	case model.MobileActionScroll:
		return executeSwipe(ctx, session, action, node, true)
	case model.MobileActionWaitVisible, model.MobileActionWaitNotVisible,
		model.MobileActionPressKey, model.MobileActionPressButton, model.MobileActionSetOrientation,
		model.MobileActionDismissKeyboard, model.MobileActionScrollTo:
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
		if err := session.Driver.PressKey(ctx, session.Target.AppID, action.Value); err != nil {
			return true, fmt.Errorf("press key: %w", err)
		}

		return true, nil
	}

	if action.Kind == model.MobileActionPressButton {
		if err := session.Driver.PressButton(ctx, session.Target.AppID, action.Value); err != nil {
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

	if action.Kind == model.MobileActionDismissKeyboard {
		if err := session.Driver.DismissKeyboard(ctx, session.Target.AppID); err != nil {
			return true, fmt.Errorf("dismiss keyboard: %w", err)
		}

		return true, nil
	}

	if action.Kind == model.MobileActionScrollTo {
		if err := session.Driver.ScrollTo(ctx, session.Target.AppID, actionLocator(action)); err != nil {
			return true, fmt.Errorf("scroll to: %w", err)
		}

		return true, nil
	}

	return false, nil
}

func executeTap(ctx context.Context, session *Session, locator driver.Locator, node *tree.ViewNode) error {
	x, y := tree.Center(node)
	if err := session.Driver.Tap(ctx, session.Target.AppID, resolvedLocator(locator, node), x, y); err != nil {
		return fmt.Errorf("tap: %w", err)
	}

	return nil
}

func executeDoubleTap(ctx context.Context, session *Session, locator driver.Locator, node *tree.ViewNode) error {
	x, y := tree.Center(node)
	if err := session.Driver.DoubleTap(ctx, session.Target.AppID, resolvedLocator(locator, node), x, y); err != nil {
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

	if err := session.Driver.LongPress(ctx, session.Target.AppID, resolvedLocator(actionLocator(action), node), x, y, duration.Seconds()); err != nil {
		return fmt.Errorf("long press: %w", err)
	}

	return nil
}

func executeInputText(ctx context.Context, session *Session, action provider.MobileActionExec, node *tree.ViewNode) error {
	if usePasteInput(node) {
		if err := session.Driver.InputText(ctx, session.Target.AppID, resolvedLocator(actionLocator(action), node), action.Value, true); err != nil {
			return fmt.Errorf("input text: %w", err)
		}

		return nil
	}

	x, y := tree.Center(node)
	if err := session.Driver.Tap(ctx, session.Target.AppID, resolvedLocator(actionLocator(action), node), x, y); err != nil {
		return fmt.Errorf("focus element: %w", err)
	}

	if err := session.Driver.InputText(ctx, session.Target.AppID, resolvedLocator(actionLocator(action), node), action.Value, false); err != nil {
		return fmt.Errorf("input text: %w", err)
	}

	return nil
}

func executeClearText(ctx context.Context, session *Session, locator driver.Locator, node *tree.ViewNode) error {
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
	if err := session.Driver.Tap(ctx, session.Target.AppID, resolvedLocator(locator, node), x, y); err != nil {
		return fmt.Errorf("focus element: %w", err)
	}

	if count == 0 {
		count = defaultClearTextErase
	}

	if err := session.Driver.EraseText(ctx, session.Target.AppID, count); err != nil {
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

	if err := session.Driver.Swipe(ctx, session.Target.AppID, startX, startY, endX, endY, duration.Seconds()); err != nil {
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

	locator := elementLocator{ID: action.ID, Label: action.Label, Text: action.Text, First: action.First}

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElement(pollCtx, session, locator)
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
		return nil, fmt.Errorf("element %s was not visible after %s: %w", locator, opts.Timeout, err)
	}

	return found, nil
}

func (p *Provider) handleExpect(ctx context.Context, session *Session, expect provider.MobileExpectExec) error {
	for _, v := range expect.Visible {
		if err := p.waitForVisibility(ctx, session, v, true, false); err != nil {
			return err
		}
	}

	for _, v := range expect.NotVisible {
		if err := p.waitForVisibility(ctx, session, v, false, false); err != nil {
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

func (p *Provider) waitForVisibility(ctx context.Context, session *Session, v provider.MobileVisibilityExec, want, first bool) error {
	opts := pollOptions(v.Timeout, v.Interval, expectDefaultTimeout)

	var found bool

	locator := elementLocator{ID: v.ID, Label: v.Label, Text: v.Text, First: first}

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElement(pollCtx, session, locator)
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
			return fmt.Errorf("element %s not found after %s: %w", locator, opts.Timeout, err)
		}

		return fmt.Errorf("element %s was not visible after %s: %w", locator, opts.Timeout, err)
	}

	return fmt.Errorf("element %s was still visible after %s: %w", locator, opts.Timeout, err)
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

	locator := elementLocator{ID: v.ID, Label: v.Label}

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElement(pollCtx, session, locator)
		if err != nil {
			return pollResult{}, err
		}

		if !ok {
			return pollResult{}, nil
		}

		found = true
		got = extract(node)
		assertionLabel := kind + "." + locator.String()

		res := pollResult{Done: true}
		if mismatch := assertion.Equal(assertionLabel, v.Expected, cty.StringVal(got)); mismatch != nil {
			res = pollResult{Mismatch: mismatch}
		}

		return res, nil
	})
	if err == nil {
		return nil
	}

	if !found {
		return fmt.Errorf("element %s not found after %s: %w", locator, opts.Timeout, err)
	}

	want := diagnostic.ScalarString(v.Expected)

	return fmt.Errorf("%s mismatch for %s after %s: want=%q got=%q: %w", kind, locator, opts.Timeout, want, got, err)
}

func (p *Provider) waitForEnabled(ctx context.Context, session *Session, v provider.MobileStateExpectationExec, want bool) error {
	opts := pollOptions(v.Timeout, v.Interval, expectDefaultTimeout)

	var (
		found    bool
		lastSeen bool
	)

	locator := elementLocator{ID: v.ID, Label: v.Label}

	err := poll(ctx, opts, func(pollCtx context.Context) (pollResult, error) {
		node, ok, err := findElement(pollCtx, session, locator)
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

		return pollResult{Mismatch: fmt.Errorf("element %s enabled=%t, want=%t", locator, node.Enabled, want)}, nil
	})
	if err == nil {
		return nil
	}

	if !found {
		return fmt.Errorf("element %s not found after %s: %w", locator, opts.Timeout, err)
	}

	state := "enabled"
	if !want {
		state = "disabled"
	}

	return fmt.Errorf("element %s was not %s after %s (last seen enabled=%t): %w", locator, state, opts.Timeout, lastSeen, err)
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

// elementLocator carries the per-call resolution mode for findElement.
// Exactly one of ID or Label is non-empty (parser-enforced). First is
// only honored when ID is set: label resolution is always firstMatch.
type elementLocator struct {
	ID    string
	Label string
	Text  string
	First bool
}

// actionLocator extracts the element locator the author wrote on an
// action.
func actionLocator(action provider.MobileActionExec) driver.Locator {
	return driver.Locator{ID: action.ID, Label: action.Label, Text: action.Text}
}

// resolvedLocator is what the driver is actually sent.
//
// The id comes from the node the provider just resolved rather than from
// the scenario, so a label- or text-located element still reaches the
// driver with whatever identifier it turned out to have. The authored
// label and text ride along so the driver can re-resolve against the
// live UI: the coordinates were computed from a snapshot, and the screen
// may have moved since.
func resolvedLocator(authored driver.Locator, node *tree.ViewNode) driver.Locator {
	locator := authored

	if node != nil {
		locator.ID = node.ID
	}

	return locator
}

// String returns a user-facing rendering of the locator for error
// messages, so a missing element cites label="Done" rather than a bare
// (and possibly empty) id.
func (l elementLocator) String() string {
	if l.Label != "" {
		return fmt.Sprintf("label=%q", l.Label)
	}

	if l.Text != "" {
		return fmt.Sprintf("text=%q", l.Text)
	}

	return fmt.Sprintf("id=%q", l.ID)
}

func findElement(ctx context.Context, session *Session, locator elementLocator) (*tree.ViewNode, bool, error) {
	hierarchy, err := session.Driver.Hierarchy(ctx, session.Target.AppID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch hierarchy: %w", err)
	}

	if locator.Label != "" {
		node, ok, err := tree.FindFirstByLabel(hierarchy, locator.Label)
		if err != nil {
			return nil, false, fmt.Errorf("find element: %w", err)
		}

		return node, ok, nil
	}

	if locator.Text != "" {
		node, ok, err := tree.FindFirstByText(hierarchy, locator.Text)
		if err != nil {
			return nil, false, fmt.Errorf("find element: %w", err)
		}

		return node, ok, nil
	}

	if locator.First {
		node, ok, err := tree.FindFirstByID(hierarchy, locator.ID)
		if err != nil {
			return nil, false, fmt.Errorf("find element: %w", err)
		}

		return node, ok, nil
	}

	node, ok, err := tree.FindByID(hierarchy, locator.ID)
	if err != nil {
		return nil, false, fmt.Errorf("find element: %w", err)
	}

	return node, ok, nil
}

// fetchHierarchyForArtifacts gets the tree to attach to a failing step,
// retrying briefly.
//
// A single attempt loses the tree in exactly the case that matters. The
// driver single-flights snapshots and answers a retryable 503 while one
// is running, and a poll that just ran out of time usually leaves its
// last attempt still in flight — so the artifact fetch arrives during
// the one window the driver refuses. That is not hypothetical: a CI run
// failed its artifact verification with no hierarchy.json while the
// device log showed the driver answering 200 in 328ms, 240ms after
// refusing the artifact fetch with a 503.
//
// Recording the tree during the poll would be better still, since it is
// the one the assertion actually judged, but the poll helpers carry
// neither the scenario nor the step name that key the record.
func fetchHierarchyForArtifacts(ctx context.Context, session *Session) (*tree.ViewNode, error) {
	var err error

	for attempt := range artifactHierarchyAttempts {
		var hierarchy *tree.ViewNode

		if hierarchy, err = session.Driver.Hierarchy(ctx, session.Target.AppID); err == nil {
			return hierarchy, nil
		}

		if attempt < artifactHierarchyAttempts-1 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("fetch hierarchy for artifacts: %w", ctx.Err())
			case <-time.After(artifactHierarchyRetryDelay):
			}
		}
	}

	return nil, fmt.Errorf("fetch hierarchy for artifacts: %w", err)
}

const (
	artifactHierarchyAttempts   = 3
	artifactHierarchyRetryDelay = 250 * time.Millisecond
)

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

	if hierarchy, err := fetchHierarchyForArtifacts(ctx, session); err == nil {
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

	// Last, and never fatal: the screenshot and hierarchy say what was on
	// screen, the device log says what the system thought it was doing.
	// An ANR, a crash and an app that is merely slow to draw are
	// indistinguishable from the first two.
	if a, ok := writeDeviceLog(ctx, dir, session); ok {
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
		return artifacts.SafePathSegment("")
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

// driverLogArtifactFromError pulls the driver startup log out of a failed
// session build, so a launch failure links straight to the log explaining
// it. Matching on the driverStartError interface rather than a concrete
// launcher error keeps this platform-agnostic.
func driverLogArtifactFromError(err error) (Artifact, bool) {
	var startErr driverStartError
	if !errors.As(err, &startErr) {
		return Artifact{}, false
	}

	path := startErr.DriverLogPath()
	if path == "" {
		return Artifact{}, false
	}

	return Artifact{Type: ArtifactTypeDriverLog, Path: path}, true
}

// looksLikeDriverDeath reports whether err's chain contains the
// transport-level patterns Go's net/http surfaces when the driver
// process is gone. We match on the well-known string fragments
// rather than typed errors because url.Error wraps everything once
// the connection is reset.
//
// Patterns:
//   - "connection refused": the next dial after the listener closed.
//   - "EOF": the listener closed mid-response, so the client got no
//     body and bubbled io.EOF up through net/http.
//   - "broken pipe": writing the POST body while the listener died.
//   - "context deadline exceeded" while reading: half-dead listener,
//     also worth flagging.
func looksLikeDriverDeath(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, ": EOF") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset")
}

// wrapDriverDeathError appends diagnostic-file hints to err when the
// failure looks like the driver process died mid-scenario (connection
// refused / EOF on POST / broken pipe). The hints quote the exact paths
// of driver.log + the .xcresult bundle dir + the build.log so users can
// open them directly in Xcode (`open <xcresult dir>/*.xcresult`) and
// read XCTest's full post-mortem (SIGABRT, XCTest API Violation, runaway
// accessibility queries, ...). No-op when the session has no
// diagnostics (external driver: Tales does not own the runner) or when
// err does not look like a transport-level driver death.
func wrapDriverDeathError(err error, session *Session) error {
	if err == nil || session == nil || !looksLikeDriverDeath(err) {
		return err
	}

	artifacts := session.Diagnostics.Artifacts
	if len(artifacts) == 0 {
		return err
	}

	hints := make([]string, 0, len(artifacts))

	for _, a := range artifacts {
		if a.Path == "" {
			continue
		}

		hints = append(hints, fmt.Sprintf("%s: %s", diagnosticLabel(a.Type), a.Path))
	}

	if len(hints) == 0 {
		return err
	}

	return fmt.Errorf("%w\ndriver process appears to have terminated mid-scenario; diagnostic files:\n  %s",
		err, strings.Join(hints, "\n  "))
}

// diagnosticLabel renders a diagnostic artifact type as the phrase shown
// in a driver-death error. Unknown types fall back to the raw type so a
// backend can add one without touching this function.
func diagnosticLabel(artifactType string) string {
	switch artifactType {
	case ArtifactTypeDriverLog:
		return "driver log"
	case ArtifactTypeXCResultDir:
		return "xcresult bundle dir (open <path>/*.xcresult in Xcode for the full XCTest crash report)"
	case ArtifactTypeDriverBuildLog:
		return "build log"
	case ArtifactTypeLogcat:
		return "device log"
	default:
		return artifactType
	}
}

// appendArtifactsToOutput merges extras into output.Response["artifacts"],
// preserving any artifacts that prior writeFailureArtifacts paths already
// recorded (screenshots, hierarchy JSON, action recordings). Idempotent
// on the (Type, Path) pair so two driver-death wraps from the same
// session do not double-list the same files.
func appendArtifactsToOutput(output *provider.Output, extras []Artifact) {
	if output == nil || len(extras) == 0 {
		return
	}

	existing := output.Response["artifacts"]
	seen := map[string]bool{}
	merged := make([]cty.Value, 0)

	if existing.IsKnown() && !existing.IsNull() && existing.Type().IsListType() {
		for it := existing.ElementIterator(); it.Next(); {
			_, v := it.Element()
			if v.Type().IsObjectType() && v.Type().HasAttribute(artifactTypeKey) && v.Type().HasAttribute(artifactPathKey) {
				typ := v.GetAttr(artifactTypeKey).AsString()
				path := v.GetAttr(artifactPathKey).AsString()
				seen[typ+"\x00"+path] = true
			}

			merged = append(merged, v)
		}
	}

	for _, a := range extras {
		key := a.Type + "\x00" + a.Path
		if seen[key] {
			continue
		}

		seen[key] = true

		merged = append(merged, cty.ObjectVal(map[string]cty.Value{
			artifactTypeKey: cty.StringVal(a.Type),
			artifactPathKey: cty.StringVal(a.Path),
		}))
	}

	if len(merged) == 0 {
		return
	}

	output.Response["artifacts"] = cty.ListVal(merged)
}

// driverDeathArtifacts surfaces the diagnostic file paths as report
// Artifacts, so a user looking at build/reports/e2e-*.html sees clickable
// links to the driver log and crash bundle next to the failed step. The
// backend already labeled them, so this is a straight hand-off.
func driverDeathArtifacts(diag Diagnostics) []Artifact {
	out := make([]Artifact, 0, len(diag.Artifacts))

	for _, a := range diag.Artifacts {
		if a.Path == "" {
			continue
		}

		out = append(out, a)
	}

	return out
}
