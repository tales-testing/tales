package mobile

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
	"github.com/tales-testing/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

type fakeTap struct {
	id    string
	label string
	text  string
	x, y  float64
}

// fakeErase records an /eraseText call. The locator is what issue #63
// was about, so the fake keeps it rather than the count alone.
type fakeErase struct {
	locator    driver.Locator
	characters int
}

type fakeInput struct {
	id    string
	label string
	text  string
	paste bool
}

type fakeSwipe struct {
	startX, startY float64
	endX, endY     float64
	duration       float64
}

type fakeLongPress struct {
	id       string
	label    string
	x, y     float64
	duration float64
}

type fakeScrollTo struct {
	id    string
	label string
}

type fakeDriverAll struct {
	driver.NoopDriver

	mu              sync.Mutex
	hierarchies     []*tree.ViewNode
	hierarchyErr    error
	taps            []fakeTap
	tapErr          error
	swipes          []fakeSwipe
	longPresses     []fakeLongPress
	doubleTaps      []fakeTap
	pressedKeys     []string
	pressedButtons  []string
	orientations    []string
	inputs          []fakeInput
	erases          []fakeErase
	dismissals      []string
	scrollTos       []fakeScrollTo
	// scrollToFailures makes the fake answer the drivers' own "element
	// not found" for that many dispatches before succeeding, which is
	// what a screen still being built looks like from Tales' side.
	scrollToFailures int
	launches        []string
	activates       []string
	terminatesDrv   []string
	screenshotPNG   []byte
	screenshotErr   error
	healthErr       error
	hierarchyDelay  time.Duration
	activeHierarchy atomic.Int32
	maxHierarchy    atomic.Int32
	hierarchyCalls  atomic.Int32

	// requireBindForHierarchy models the XCUITest constraint that drives
	// issue #41: a /hierarchy snapshot only succeeds while the driver owns a
	// live, registered app process. Launch (XCUIApplication.launch()) binds
	// it, Terminate unbinds it; without a driver-routed Launch, Hierarchy
	// behaves like the real driver and times out.
	requireBindForHierarchy bool
	boundLive               atomic.Bool
}

func (f *fakeDriverAll) Health(_ context.Context) error { return f.healthErr }

func (f *fakeDriverAll) Launch(_ context.Context, bundleID string) error {
	f.mu.Lock()
	f.launches = append(f.launches, bundleID)
	f.mu.Unlock()

	f.boundLive.Store(true)

	return nil
}

// Activate binds the session exactly like Launch does: that is what makes
// it a valid substitute once the host owns the cold start.
func (f *fakeDriverAll) Activate(_ context.Context, bundleID string) error {
	f.mu.Lock()
	f.activates = append(f.activates, bundleID)
	f.mu.Unlock()

	f.boundLive.Store(true)

	return nil
}

func (f *fakeDriverAll) Terminate(_ context.Context, bundleID string) error {
	f.mu.Lock()
	f.terminatesDrv = append(f.terminatesDrv, bundleID)
	f.mu.Unlock()

	f.boundLive.Store(false)

	return nil
}

func (f *fakeDriverAll) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	f.hierarchyCalls.Add(1)

	if f.requireBindForHierarchy && !f.boundLive.Load() {
		return nil, context.DeadlineExceeded
	}

	if f.hierarchyDelay > 0 {
		active := f.activeHierarchy.Add(1)
		for {
			maxSeen := f.maxHierarchy.Load()
			if active <= maxSeen || f.maxHierarchy.CompareAndSwap(maxSeen, active) {
				break
			}
		}
		time.Sleep(f.hierarchyDelay)
		f.activeHierarchy.Add(-1)
	}

	if f.hierarchyErr != nil {
		return nil, f.hierarchyErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.hierarchies) == 0 {
		return nil, errors.New("no hierarchy")
	}

	if len(f.hierarchies) == 1 {
		return f.hierarchies[0], nil
	}

	node := f.hierarchies[0]
	f.hierarchies = f.hierarchies[1:]

	return node, nil
}

func (f *fakeDriverAll) Tap(_ context.Context, _ string, locator driver.Locator, x, y float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.taps = append(f.taps, fakeTap{id: locator.ID, label: locator.Label, text: locator.Text, x: x, y: y})

	return f.tapErr
}

func (f *fakeDriverAll) Swipe(_ context.Context, _ string, startX, startY, endX, endY, duration float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.swipes = append(f.swipes, fakeSwipe{
		startX: startX, startY: startY, endX: endX, endY: endY, duration: duration,
	})

	return nil
}

func (f *fakeDriverAll) LongPress(_ context.Context, _ string, locator driver.Locator, x, y, duration float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.longPresses = append(f.longPresses, fakeLongPress{id: locator.ID, label: locator.Label, x: x, y: y, duration: duration})

	return nil
}

func (f *fakeDriverAll) DoubleTap(_ context.Context, _ string, locator driver.Locator, x, y float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.doubleTaps = append(f.doubleTaps, fakeTap{id: locator.ID, label: locator.Label, text: locator.Text, x: x, y: y})

	return nil
}

func (f *fakeDriverAll) PressKey(_ context.Context, _, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pressedKeys = append(f.pressedKeys, key)

	return nil
}

func (f *fakeDriverAll) PressButton(_ context.Context, _, button string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pressedButtons = append(f.pressedButtons, button)

	return nil
}

func (f *fakeDriverAll) SetOrientation(_ context.Context, orientation string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.orientations = append(f.orientations, orientation)

	return nil
}

func (f *fakeDriverAll) InputText(_ context.Context, _ string, locator driver.Locator, text string, paste bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.inputs = append(f.inputs, fakeInput{id: locator.ID, label: locator.Label, text: text, paste: paste})

	return nil
}

func (f *fakeDriverAll) EraseText(_ context.Context, _ string, locator driver.Locator, count int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.erases = append(f.erases, fakeErase{locator: locator, characters: count})

	return nil
}

func (f *fakeDriverAll) DismissKeyboard(_ context.Context, bundleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.dismissals = append(f.dismissals, bundleID)

	return nil
}

func (f *fakeDriverAll) ScrollTo(_ context.Context, _ string, locator driver.Locator) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.scrollTos = append(f.scrollTos, fakeScrollTo{id: locator.ID, label: locator.Label})

	// Both real drivers resolve the locator themselves and answer 404
	// when it matches nothing at the end of their own attempts, which is
	// what issue #64 was about.
	if f.scrollToFailures > 0 {
		f.scrollToFailures--

		return errors.New("element not found")
	}

	return nil
}

func (f *fakeDriverAll) Screenshot(_ context.Context) ([]byte, error) {
	if f.screenshotErr != nil {
		return nil, f.screenshotErr
	}

	if f.screenshotPNG == nil {
		return []byte("PNG"), nil
	}

	return f.screenshotPNG, nil
}

// fakeLifecycle implements Lifecycle directly, so provider tests exercise
// the step path without any platform tooling. It records the calls the
// tests assert on and no-ops the rest.
type fakeLifecycle struct {
	udid       string
	terminates atomic.Int32
	privacy    []string
}

func (*fakeLifecycle) InstallApp(_ context.Context, _ string, _ Target) error { return nil }

func (*fakeLifecycle) ClearAppState(_ context.Context, _ string, _ Target) error { return nil }

func (f *fakeLifecycle) SetPermission(_ context.Context, _ string, _ Target, action, service string) error {
	f.privacy = append(f.privacy, action+" "+service)

	return nil
}

func (f *fakeLifecycle) TerminateApp(_ context.Context, _ string, _ Target) error {
	f.terminates.Add(1)

	return nil
}

func (f *fakeLifecycle) TerminateDriverRunner(_ context.Context, _ string) error {
	f.terminates.Add(1)

	return nil
}

func (*fakeLifecycle) ScreenshotFallback(_ context.Context, _, _ string) error { return nil }

func newProviderWithFake(drv *fakeDriverAll, lifecycle *fakeLifecycle, target Target) *Provider {
	return newProviderWithFakeAndDiagnostics(drv, lifecycle, target, Diagnostics{})
}

// iosDiagnostics builds the diagnostics an Apple backend would attach.
// Empty paths are skipped, mirroring the backend, so a test can pass ""
// to assert on a partially-populated set.
func iosDiagnostics(driverLog, xcresultDir, buildLog string) Diagnostics {
	var out []Artifact

	if driverLog != "" {
		out = append(out, Artifact{Type: ArtifactTypeDriverLog, Path: driverLog})
	}

	if xcresultDir != "" {
		out = append(out, Artifact{Type: ArtifactTypeXCResultDir, Path: xcresultDir})
	}

	if buildLog != "" {
		out = append(out, Artifact{Type: ArtifactTypeDriverBuildLog, Path: buildLog})
	}

	return Diagnostics{Artifacts: out}
}

func newProviderWithFakeAndDiagnostics(drv *fakeDriverAll, lifecycle *fakeLifecycle, target Target, diag Diagnostics) *Provider {
	builder := SessionBuilderFunc(func(_ context.Context, _ Target) (*Session, error) {
		return &Session{
			Target:      target,
			DeviceID:    lifecycle.udid,
			Driver:      drv,
			Lifecycle:   lifecycle,
			Diagnostics: diag,
		}, nil
	})

	return New(WithSessionBuilder(builder), WithArtifactsBase(""))
}

func sampleProviderTarget() Target {
	return Target{
		Name:       "iphone",
		Platform:   PlatformIOS,
		DeviceName: "iPhone 16",
		AppPath:    "./MyApp.app",
		AppID:      "com.example.MyApp",
		Driver:     DriverConfig{Host: "127.0.0.1", Port: 9080},
	}
}

func sampleConfigCty() map[string]cty.Value {
	return map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
				}),
			}),
		}),
	}
}

func newStep(name string) *model.Step {
	return &model.Step{Provider: "mobile", Name: name}
}

func newButtonNode() *tree.ViewNode {
	return &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				ID:      "welcome.register",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 10, Y: 20, Width: 100, Height: 40},
			},
		},
	}
}

func TestExecuteTapFindsCenterAndSends(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("tap"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if out == nil {
		t.Fatal("expected output")
	}

	if len(drv.taps) != 1 {
		t.Fatalf("expected 1 tap, got %d", len(drv.taps))
	}

	if drv.taps[0].x != 60 || drv.taps[0].y != 40 {
		t.Fatalf("unexpected tap coordinates %+v", drv.taps[0])
	}

	if drv.taps[0].id != "welcome.register" {
		t.Fatalf("expected tap to carry node id, got %q", drv.taps[0].id)
	}
}

// newDuplicateSiblingsHierarchy mirrors the iOS PhotosPicker layout where
// every grid cell carries the same accessibility identifier and the cells
// are siblings inside the collection view (no parent-child collapse).
func newDuplicateSiblingsHierarchy() *tree.ViewNode {
	return &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				ID:      "PXGGridLayout-Info",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 0, Y: 0, Width: 100, Height: 100},
			},
			{
				ID:      "PXGGridLayout-Info",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 100, Y: 0, Width: 100, Height: 100},
			},
			{
				ID:      "PXGGridLayout-Info",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 200, Y: 0, Width: 100, Height: 100},
			},
		},
	}
}

func TestExecuteTapFirstResolvesDuplicateSiblings(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newDuplicateSiblingsHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("tap-first"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "PXGGridLayout-Info", First: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if out == nil {
		t.Fatal("expected output")
	}

	if len(drv.taps) != 1 {
		t.Fatalf("expected 1 tap, got %d", len(drv.taps))
	}

	// The first sibling sits at (0,0)-(100,100); its center is (50,50).
	if drv.taps[0].x != 50 || drv.taps[0].y != 50 {
		t.Fatalf("expected tap on the first sibling at (50,50), got %+v", drv.taps[0])
	}
}

func TestExecuteTapDuplicateSiblingsWithoutFirstFails(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newDuplicateSiblingsHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("tap-strict"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "PXGGridLayout-Info"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected ErrDuplicate to surface when first is false")
	}

	if !strings.Contains(err.Error(), "multiple elements share the same id") {
		t.Fatalf("expected ErrDuplicate message, got %v", err)
	}

	if len(drv.taps) != 0 {
		t.Fatalf("expected no taps on ambiguous resolution, got %d", len(drv.taps))
	}
}

func TestExecuteWaitVisibleFirstResolvesDuplicateSiblings(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newDuplicateSiblingsHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("wait-first"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:    model.MobileActionWaitVisible,
					ID:      "PXGGridLayout-Info",
					First:   true,
					Timeout: 50 * time.Millisecond,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected wait_visible first=true to succeed on duplicate siblings, got %v", err)
	}
}

// newSystemPickerHierarchy mirrors an iOS PHPickerViewController shape:
// a "Done" button with empty accessibilityIdentifier and a non-empty
// accessibilityLabel. The strict id-based path cannot reach it; label
// resolution is the only way.
func newSystemPickerHierarchy() *tree.ViewNode {
	return &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				ID:      "",
				Label:   "Done",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 200, Y: 40, Width: 80, Height: 30},
			},
		},
	}
}

func TestExecuteTapByLabelResolvesEmptyID(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newSystemPickerHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("tap-label"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, Label: "Done"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if out == nil {
		t.Fatal("expected output")
	}

	if len(drv.taps) != 1 {
		t.Fatalf("expected 1 tap, got %d", len(drv.taps))
	}

	tap := drv.taps[0]

	if tap.label != "Done" {
		t.Fatalf("expected driver to receive label=\"Done\", got %q", tap.label)
	}

	if tap.id != "" {
		t.Fatalf("expected driver to receive empty id when matching by label, got %q", tap.id)
	}

	// Center of (200,40)-(280,70).
	if tap.x != 240 || tap.y != 55 {
		t.Fatalf("expected tap on Done button center (240,55), got %+v", tap)
	}
}

func TestExecuteExpectVisibleByLabel(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newSystemPickerHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("visible-label"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{Label: "Done", Timeout: 50 * time.Millisecond},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected visible expectation to succeed via label snapshot, got %v", err)
	}
}

// TestExecuteWaitVisibleActionByLabel is the actions-block counterpart of
// TestExecuteExpectVisibleByLabel. The expect form passes the decoded
// MobileVisibilityExec straight through, so it always carried Label; the
// action form rebuilds the struct in handleAction and used to drop it,
// leaving the locator empty and timing out on a element that was right
// there. e2e/ios/pass/picker_label.tales exercises exactly this path.
func TestExecuteWaitVisibleActionByLabel(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newSystemPickerHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("wait-visible-label"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionWaitVisible, Label: "Done", Timeout: 50 * time.Millisecond},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected wait_visible action to resolve by label, got %v", err)
	}
}

// TestExecuteWaitNotVisibleActionByLabel guards the mirror case: the
// element must be looked up by label so its absence is a genuine
// observation rather than the side effect of an empty locator, which
// would make the assertion pass for the wrong reason.
func TestExecuteWaitNotVisibleActionByLabel(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newSystemPickerHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("wait-not-visible-label"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionWaitNotVisible, Label: "Done", Timeout: 50 * time.Millisecond},
			},
		},
	})
	if err == nil {
		t.Fatal("expected wait_not_visible to fail while the labelled element is still visible")
	}

	if !strings.Contains(err.Error(), `label="Done"`) {
		t.Fatalf("expected the error to cite the label locator, got %v", err)
	}
}

func TestExecuteTapByMissingLabelSurfacesLocatorInError(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newSystemPickerHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "picker",
		Step:     newStep("tap-missing-label"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, Label: "Missing", Timeout: 30 * time.Millisecond},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error when label does not resolve")
	}

	if !strings.Contains(err.Error(), `label="Missing"`) {
		t.Fatalf("expected error to surface the label, got: %v", err)
	}
}

func TestExecuteDoubleTapSendsToDriver(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("dt"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionDoubleTap, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.doubleTaps) != 1 || drv.doubleTaps[0].id != "welcome.register" {
		t.Fatalf("expected one double tap on welcome.register, got %+v", drv.doubleTaps)
	}

	if drv.doubleTaps[0].x != 60 || drv.doubleTaps[0].y != 40 {
		t.Fatalf("unexpected double tap coordinates %+v", drv.doubleTaps[0])
	}
}

func TestExecuteLongPressUsesDuration(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("lp"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionLongPress, ID: "welcome.register", Duration: 2 * time.Second},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.longPresses) != 1 {
		t.Fatalf("expected one long press, got %d", len(drv.longPresses))
	}

	got := drv.longPresses[0]
	if got.id != "welcome.register" || got.duration != 2.0 {
		t.Fatalf("unexpected long press %+v", got)
	}
}

func TestExecuteLongPressDefaultsDuration(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("lp"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionLongPress, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.longPresses) != 1 || drv.longPresses[0].duration != 1.0 {
		t.Fatalf("expected default 1s long press, got %+v", drv.longPresses)
	}
}

func TestExecuteSwipeComputesVectorFromBounds(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("sw"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				// welcome.register bounds {10,20,100,40} → center (60,40),
				// default distance 0.6 → vertical travel 24. Swipe up:
				// finger goes from (60,52) to (60,28).
				{Kind: model.MobileActionSwipe, ID: "welcome.register", Direction: "up"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.swipes) != 1 {
		t.Fatalf("expected one swipe, got %d", len(drv.swipes))
	}

	got := drv.swipes[0]
	if got.startX != 60 || got.startY != 52 || got.endX != 60 || got.endY != 28 {
		t.Fatalf("unexpected swipe vector %+v", got)
	}
}

func TestExecuteScrollInvertsDirection(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("sc"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				// scroll down reveals lower content → finger swipes up:
				// from (60,52) to (60,28), same as swipe up.
				{Kind: model.MobileActionScroll, ID: "welcome.register", Direction: "down"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.swipes) != 1 {
		t.Fatalf("expected one swipe from scroll, got %d", len(drv.swipes))
	}

	got := drv.swipes[0]
	if got.startY != 52 || got.endY != 28 {
		t.Fatalf("scroll down should swipe finger up, got %+v", got)
	}
}

func TestExecuteSwipeRejectsBadDirection(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("sw"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionSwipe, ID: "welcome.register", Direction: "sideways"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected step to fail on invalid swipe direction")
	}

	if len(drv.swipes) != 0 {
		t.Fatalf("expected no swipe dispatched on bad direction, got %d", len(drv.swipes))
	}
}

func TestExecuteAppliesPermissionsOnLaunch(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("launch"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Launch:     &provider.MobileLaunchExec{ClearState: true},
			Permissions: []provider.MobilePermissionExec{
				{Service: "camera", Action: "grant"},
				{Service: "photos", Action: "revoke"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(lc.privacy) != 2 || lc.privacy[0] != "grant camera" || lc.privacy[1] != "revoke photos" {
		t.Fatalf("unexpected privacy calls: %v", lc.privacy)
	}
}

func TestExecuteDeviceActionsDispatchWithoutElement(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("device"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionPressKey, Value: "return"},
				{Kind: model.MobileActionPressButton, Value: "home"},
				{Kind: model.MobileActionSetOrientation, Value: "landscape_left"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.pressedKeys) != 1 || drv.pressedKeys[0] != "return" {
		t.Fatalf("unexpected pressed keys: %v", drv.pressedKeys)
	}

	if len(drv.pressedButtons) != 1 || drv.pressedButtons[0] != "home" {
		t.Fatalf("unexpected pressed buttons: %v", drv.pressedButtons)
	}

	if len(drv.orientations) != 1 || drv.orientations[0] != "landscape_left" {
		t.Fatalf("unexpected orientations: %v", drv.orientations)
	}

	// Device actions never resolve an element, so no tap was issued.
	if len(drv.taps) != 0 {
		t.Fatalf("device actions should not tap an element, got %d taps", len(drv.taps))
	}
}

func TestLooksLikeDriverDeath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("element not found"), false},
		{errors.New("call POST /tap: dial tcp 127.0.0.1:9080: connect: connection refused"), true},
		{errors.New("call POST /pressKey: EOF"), true},
		{errors.New("call POST /tap: write: broken pipe"), true},
		{errors.New("call POST /tap: connection reset by peer"), true},
	}

	for _, tc := range cases {
		if got := looksLikeDriverDeath(tc.err); got != tc.want {
			t.Errorf("looksLikeDriverDeath(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestWrapDriverDeathErrorAppendsDiagnostics(t *testing.T) {
	t.Parallel()

	session := &Session{
		Diagnostics: iosDiagnostics("/tmp/driver.log", "/tmp/derived/Logs/Test", "/tmp/build.log"),
	}

	wrapped := wrapDriverDeathError(errors.New("call POST /tap: connection refused"), session)
	if wrapped == nil {
		t.Fatal("expected wrapped error")
	}

	msg := wrapped.Error()
	for _, want := range []string{
		"driver process appears to have terminated",
		"/tmp/driver.log",
		"/tmp/derived/Logs/Test",
		"*.xcresult",
		"/tmp/build.log",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("wrapped error missing %q, got: %s", want, msg)
		}
	}
}

func TestWrapDriverDeathErrorNoOpWithoutDiagnostics(t *testing.T) {
	t.Parallel()

	session := &Session{} // No diagnostics (external driver).

	original := errors.New("call POST /tap: connection refused")

	wrapped := wrapDriverDeathError(original, session)
	if wrapped.Error() != original.Error() {
		t.Fatalf("expected no wrapping for empty Diagnostics, got: %s", wrapped.Error())
	}
}

func TestWrapDriverDeathErrorNoOpForNonTransportError(t *testing.T) {
	t.Parallel()

	session := &Session{
		Diagnostics: iosDiagnostics("/tmp/driver.log", "", ""),
	}

	original := errors.New("element id=\"foo\" not found after 30s")

	wrapped := wrapDriverDeathError(original, session)
	if wrapped.Error() != original.Error() {
		t.Fatalf("expected no wrapping for non-transport error, got: %s", wrapped.Error())
	}
}

func TestDriverDeathArtifactsSkipsEmptyFields(t *testing.T) {
	t.Parallel()

	out := driverDeathArtifacts(iosDiagnostics("/tmp/driver.log", "", ""))

	if len(out) != 1 || out[0].Type != ArtifactTypeDriverLog {
		t.Fatalf("expected only the driver_log artifact, got %+v", out)
	}
}

func TestExecuteSurfacesDriverDeathDiagnostics(t *testing.T) {
	t.Parallel()

	// fakeDriverAll lets us simulate the post-XCTest-tear-down state:
	// the listener died, every subsequent dial returns "connection
	// refused". tapErr is the same error pattern http.Client surfaces
	// when net/http calls Dial under those conditions.
	drv := &fakeDriverAll{
		hierarchies: []*tree.ViewNode{newButtonNode()},
		tapErr:      errors.New("call POST /tap: dial tcp 127.0.0.1:9080: connect: connection refused"),
	}
	lc := &fakeLifecycle{udid: "UDID"}
	driverLog := "/tmp/build/artifacts/mobile/driver/iphone/driver.log"
	xcresultDir := "/Users/me/Library/Caches/tales/apple-driver/abc/derived-data/Logs/Test"
	diag := iosDiagnostics(driverLog, xcresultDir, "/Users/me/Library/Caches/tales/apple-driver/abc/logs/build.log")
	p := newProviderWithFakeAndDiagnostics(drv, lc, sampleProviderTarget(), diag)

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "form",
		Step:     newStep("tap-died"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "welcome.register"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error when driver dies mid-action")
	}

	msg := err.Error()
	for _, want := range []string{
		"connection refused",
		"driver process appears to have terminated",
		driverLog,
		xcresultDir,
		"*.xcresult",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("driver-death error missing %q, got:\n%s", want, msg)
		}
	}

	// The same paths surface as report artifacts so the visual / JSONL
	// renderer can show clickable links next to the failed step.
	artifacts := out.Response["artifacts"]
	if !artifacts.IsKnown() || artifacts.IsNull() {
		t.Fatal("expected artifacts list on the output")
	}

	gotTypes := map[string]bool{}
	for it := artifacts.ElementIterator(); it.Next(); {
		_, v := it.Element()
		gotTypes[v.GetAttr("type").AsString()] = true
	}

	for _, want := range []string{ArtifactTypeDriverLog, ArtifactTypeXCResultDir, ArtifactTypeDriverBuildLog} {
		if !gotTypes[want] {
			t.Errorf("expected artifact of type %q in output, got %v", want, gotTypes)
		}
	}
}

func TestExecuteScrollToDispatchesLocatorToDriver(t *testing.T) {
	t.Parallel()

	// Both targets have to be in the tree: since issue #64 scroll_to
	// waits for its element to exist before dispatching.
	screen := &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{ID: "form.identifier_value", Visible: true, Enabled: true, Bounds: tree.Rect{X: 10, Y: 20, Width: 100, Height: 40}},
			{Label: "Done", Visible: true, Enabled: true, Bounds: tree.Rect{X: 10, Y: 80, Width: 100, Height: 40}},
		},
	}

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{screen}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "form",
		Step:     newStep("scroll"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionScrollTo, ID: "form.identifier_value"},
				{Kind: model.MobileActionScrollTo, Label: "Done"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.scrollTos) != 2 {
		t.Fatalf("expected 2 scroll_to dispatches, got %d", len(drv.scrollTos))
	}

	if drv.scrollTos[0].id != "form.identifier_value" || drv.scrollTos[0].label != "" {
		t.Fatalf("first scroll_to: expected id-only locator, got %+v", drv.scrollTos[0])
	}

	if drv.scrollTos[1].label != "Done" || drv.scrollTos[1].id != "" {
		t.Fatalf("second scroll_to: expected label-only locator, got %+v", drv.scrollTos[1])
	}

	// scroll_to is a device action: no element resolution, no tap.
	if len(drv.taps) != 0 {
		t.Fatalf("scroll_to should not tap, got %d taps", len(drv.taps))
	}
}

func TestExecuteDismissKeyboardDispatchesToDriver(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "form",
		Step:     newStep("dismiss"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionDismissKeyboard},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.dismissals) != 1 || drv.dismissals[0] != sampleProviderTarget().AppID {
		t.Fatalf("expected one dismiss_keyboard call carrying the target bundle id, got %v", drv.dismissals)
	}

	// dismiss_keyboard never resolves an element, so no tap / hierarchy
	// fetch is issued.
	if len(drv.taps) != 0 {
		t.Fatalf("dismiss_keyboard should not tap, got %d taps", len(drv.taps))
	}
}

func TestExecuteInputTextOnSecureFieldUsesPasteAndSkipsFocusTap(t *testing.T) {
	t.Parallel()

	secure := &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				ID:      "auth.signup.password",
				Type:    "secure_text_field",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 10, Y: 20, Width: 100, Height: 40},
			},
		},
	}

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{secure}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("type"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionInputText, ID: "auth.signup.password", Value: "p@ssw0rd!"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.taps) != 0 {
		t.Fatalf("paste mode should not emit a focus tap, got %d taps", len(drv.taps))
	}

	if len(drv.inputs) != 1 {
		t.Fatalf("expected 1 input call, got %d", len(drv.inputs))
	}

	if !drv.inputs[0].paste {
		t.Fatalf("expected paste=true on secure_text_field, got %+v", drv.inputs[0])
	}

	if drv.inputs[0].id != "auth.signup.password" || drv.inputs[0].text != "p@ssw0rd!" {
		t.Fatalf("unexpected input payload: %+v", drv.inputs[0])
	}
}

func TestExecuteInputTextTapsThenTypes(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("type"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionInputText, ID: "welcome.register", Value: "hello"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.taps) != 1 {
		t.Fatalf("expected one focusing tap, got %d", len(drv.taps))
	}

	if len(drv.inputs) != 1 || drv.inputs[0].text != "hello" || drv.inputs[0].paste {
		t.Fatalf("unexpected inputs: %v", drv.inputs)
	}
}

func TestExecuteClearTextUsesValueLength(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Value = "abcde"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("clear"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionClearText, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.erases) != 1 || drv.erases[0].characters != 5 {
		t.Fatalf("expected erase=5, got %v", drv.erases)
	}
}

func TestExecuteClearTextSkipsEmptySecureField(t *testing.T) {
	t.Parallel()

	root := &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				ID:      "auth.signup.password_confirm",
				Type:    "secure_text_field",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 10, Y: 20, Width: 100, Height: 40},
			},
		},
	}

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{root}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("clear"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionClearText, ID: "auth.signup.password_confirm"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.taps) != 0 {
		t.Fatalf("clear_text on empty SecureField should not focus-tap (would leak deletes via strong-password group), got %d taps", len(drv.taps))
	}

	if len(drv.erases) != 0 {
		t.Fatalf("clear_text on empty SecureField should not erase, got %d erase calls", len(drv.erases))
	}
}

func TestExecuteExpectVisibleSucceeds(t *testing.T) {
	t.Parallel()

	node := newButtonNode()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("ev"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: time.Second},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestExecuteActionWaitsUntilElementIsVisible(t *testing.T) {
	t.Parallel()

	missing := &tree.ViewNode{ID: "root", Visible: true}
	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{missing, newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("tap"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "welcome.register", Timeout: time.Second},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := drv.hierarchyCalls.Load(); got < 2 {
		t.Fatalf("expected polling to fetch hierarchy at least twice, got %d", got)
	}
}

func TestExecuteActionTimesOutWhenElementNeverAppears(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{{ID: "root", Visible: true}}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("tap"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "welcome.register", Timeout: 30 * time.Millisecond},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "was not visible after") {
		t.Fatalf("expected action timeout, got %v", err)
	}
}

func TestExecuteExpectVisibleTimesOut(t *testing.T) {
	t.Parallel()

	hidden := newButtonNode()
	hidden.Children[0].Visible = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{hidden}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("ev"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: 30 * time.Millisecond},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "was not visible after") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestExecuteExpectVisiblePollsUntilVisible(t *testing.T) {
	t.Parallel()

	hidden := newButtonNode()
	hidden.Children[0].Visible = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{hidden, newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("ev"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: time.Second},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected visible after polling, got %v", err)
	}
}

func TestExecuteExpectNotVisibleWhenMissing(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{{ID: "root", Visible: true}}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("nv"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				NotVisible: []provider.MobileVisibilityExec{
					{ID: "login.error", Timeout: time.Second},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected not_visible to pass when missing, got %v", err)
	}
}

func TestExecuteExpectNotVisibleWhenHidden(t *testing.T) {
	t.Parallel()

	hidden := newButtonNode()
	hidden.Children[0].Visible = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{hidden}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("nv"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				NotVisible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: time.Second},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected not_visible to pass when hidden, got %v", err)
	}
}

func TestExecuteExpectNotVisibleTimesOutWhileVisible(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("nv"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				NotVisible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: 30 * time.Millisecond},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "was still visible after") {
		t.Fatalf("expected not_visible timeout, got %v", err)
	}
}

func TestExecuteWaitVisibleActionPollsUntilVisible(t *testing.T) {
	t.Parallel()

	hidden := newButtonNode()
	hidden.Children[0].Visible = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{hidden, newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("wait"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionWaitVisible, ID: "welcome.register", Timeout: time.Second},
			},
		},
	})
	if err != nil {
		t.Fatalf("wait_visible should pass after polling: %v", err)
	}
}

func TestExecuteWaitNotVisibleActionTimesOut(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("wait"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionWaitNotVisible, ID: "welcome.register", Timeout: 30 * time.Millisecond},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "was still visible after") {
		t.Fatalf("expected wait_not_visible timeout, got %v", err)
	}
}

func TestExecuteTextExpectationSupportsContainsMatcher(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Text = "Welcome back"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Text: []provider.MobileValueExpectationExec{
					{
						ID: "welcome.register",
						Expected: cty.ObjectVal(map[string]cty.Value{
							"__tales_matcher": cty.StringVal("contains"),
							"value":           cty.StringVal("Welcome"),
						}),
						Timeout: time.Second,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("text matcher should pass: %v", err)
	}
}

func TestExecuteTextExpectationFailsCleanly(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Text = "Bienvenue"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Text: []provider.MobileValueExpectationExec{
					{ID: "welcome.register", Expected: cty.StringVal("Welcome"), Timeout: 30 * time.Millisecond},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `text mismatch for id="welcome.register"`) {
		t.Fatalf("expected text mismatch, got %v", err)
	}
}

func TestExecuteTextExpectationReportsElementNotFound(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{{ID: "root", Visible: true}}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Text: []provider.MobileValueExpectationExec{
					{ID: "welcome.register", Expected: cty.StringVal("Welcome"), Timeout: 30 * time.Millisecond},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	msg := err.Error()
	if !strings.Contains(msg, `element id="welcome.register" not found after`) {
		t.Fatalf("expected not-found message, got %v", msg)
	}

	if strings.Contains(msg, `got=""`) {
		t.Fatalf("not-found error should not surface a misleading got=\"\" mismatch: %v", msg)
	}
}

func TestExecuteTextExpectationPreservesMatcherMessageOnTimeout(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Text = "Bienvenue"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Text: []provider.MobileValueExpectationExec{
					{
						ID: "welcome.register",
						Expected: cty.ObjectVal(map[string]cty.Value{
							"__tales_matcher": cty.StringVal("contains"),
							"value":           cty.StringVal("Welcome"),
						}),
						Timeout: 30 * time.Millisecond,
					},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected mismatch error")
	}

	msg := err.Error()
	if !strings.Contains(msg, `text mismatch for id="welcome.register"`) {
		t.Fatalf("expected mismatch summary, got %v", msg)
	}

	if !strings.Contains(msg, "Welcome") {
		t.Fatalf("expected matcher-specific detail (want=...): %v", msg)
	}
}

func TestExecuteValueExpectationPasses(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Value = "user@example.com"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("value"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Value: []provider.MobileValueExpectationExec{
					{ID: "welcome.register", Expected: cty.StringVal("user@example.com"), Timeout: time.Second},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("value expectation should pass: %v", err)
	}
}

func TestExecuteEnabledDisabledExpectations(t *testing.T) {
	t.Parallel()

	enabled := newButtonNode()
	disabled := newButtonNode()
	disabled.Children[0].Enabled = false

	for name, tc := range map[string]struct {
		hierarchy *tree.ViewNode
		expect    provider.MobileExpectExec
	}{
		"enabled": {
			hierarchy: enabled,
			expect: provider.MobileExpectExec{Enabled: []provider.MobileStateExpectationExec{
				{ID: "welcome.register", Timeout: time.Second},
			}},
		},
		"disabled": {
			hierarchy: disabled,
			expect: provider.MobileExpectExec{Disabled: []provider.MobileStateExpectationExec{
				{ID: "welcome.register", Timeout: time.Second},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{tc.hierarchy}}
			lc := &fakeLifecycle{udid: "UDID"}
			p := newProviderWithFake(drv, lc, sampleProviderTarget())

			_, err := p.Execute(context.Background(), provider.Input{
				Scenario: "demo",
				Step:     newStep(name),
				Config:   sampleConfigCty(),
				Mobile: &provider.MobileExecution{
					Platform:   "ios",
					TargetName: "iphone",
					Expect:     tc.expect,
				},
			})
			if err != nil {
				t.Fatalf("expectation should pass: %v", err)
			}
		})
	}
}

func TestExecuteRejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("x"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "android",
			TargetName: "iphone",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "android") {
		t.Fatalf("expected unsupported platform error, got %v", err)
	}
}

func TestExecuteRecordsLastHierarchy(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("hier"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	hierarchy := p.LastHierarchy("demo", "hier")
	if hierarchy == nil || hierarchy.ID != "root" {
		t.Fatalf("expected last hierarchy recorded, got %+v", hierarchy)
	}
}

func TestExecuteWritesArtifactsOnFailure(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies: []*tree.ViewNode{
			{ID: "root", Visible: true},
			{ID: "root", Visible: true},
		},
	}
	lc := &fakeLifecycle{udid: "UDID"}

	base := t.TempDir()
	builder := SessionBuilderFunc(func(_ context.Context, _ Target) (*Session, error) {
		return &Session{
			Target:    sampleProviderTarget(),
			DeviceID:  lc.udid,
			Driver:    drv,
			Lifecycle: lc,
		}, nil
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(base))

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("fail"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, ID: "does.not.exist", Timeout: 30 * time.Millisecond},
			},
		},
	})
	if err == nil {
		t.Fatal("expected failure")
	}

	if out == nil {
		t.Fatal("expected output even on failure")
	}

	artifacts, ok := out.Response["artifacts"]
	if !ok || artifacts.LengthInt() == 0 {
		t.Fatalf("expected artifacts in response, got %+v", out.Response)
	}
}

func TestExecuteIncludesDriverLogArtifactOnStartupFailure(t *testing.T) {
	t.Parallel()

	builder := SessionBuilderFunc(func(_ context.Context, _ Target) (*Session, error) {
		return nil, &xcodebuild.StartError{Err: errors.New("driver did not become healthy"), LogPath: "build/artifacts/mobile/driver/iphone/driver.log"}
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(""))

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("launch"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
		},
	})
	if err == nil {
		t.Fatal("expected startup failure")
	}

	if out == nil {
		t.Fatal("expected output with artifacts")
	}

	artifacts := out.Response["artifacts"]
	if artifacts.LengthInt() != 1 {
		t.Fatalf("expected one driver log artifact, got %s", artifacts.GoString())
	}
}

func TestExecuteSerializesStepsForSameTarget(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies:    []*tree.ViewNode{newButtonNode()},
		hierarchyDelay: 80 * time.Millisecond,
	}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	run := func(done chan<- error) {
		_, err := p.Execute(context.Background(), provider.Input{
			Scenario: "demo",
			Step:     newStep("ev"),
			Config:   sampleConfigCty(),
			Mobile: &provider.MobileExecution{
				Platform:   "ios",
				TargetName: "iphone",
				Expect: provider.MobileExpectExec{
					Visible: []provider.MobileVisibilityExec{
						{ID: "welcome.register", Timeout: time.Second},
					},
				},
			},
		})
		done <- err
	}

	done := make(chan error, 2)
	go run(done)
	go run(done)

	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for mobile executions")
		}
	}

	if got := drv.maxHierarchy.Load(); got != 1 {
		t.Fatalf("expected same-target steps to be serialized, max concurrent hierarchy calls=%d", got)
	}
}

func TestCloseClearsSessions(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, _ = p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("ev"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{ID: "welcome.register", Timeout: time.Second},
				},
			},
		},
	})

	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if len(p.sessions) != 0 {
		t.Fatalf("expected sessions cleared, got %d", len(p.sessions))
	}

	if got := lc.terminates.Load(); got == 0 {
		t.Fatalf("expected terminate to fire during Close, got %d", got)
	}
}

func TestTypeReturnsMobile(t *testing.T) {
	t.Parallel()

	p := New()
	if p.Type() != "mobile" {
		t.Fatalf("expected mobile, got %q", p.Type())
	}
}

// TestSharedTargetSecondScenarioRelaunchesViaDriver pins issue #41: two
// scenarios reuse the same cached session, and the second one must still be
// able to read the hierarchy. It only can because launch and the terminate{}
// step route through the driver (XCUIApplication.launch()/terminate()), which
// keeps XCTest bound to the live app process. The fake encodes that binding:
// Hierarchy times out unless a driver-routed Launch happened since the last
// Terminate — exactly what a simctl-based launch would fail to do.
func TestSharedTargetSecondScenarioRelaunchesViaDriver(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies:             []*tree.ViewNode{newButtonNode()},
		requireBindForHierarchy: true,
	}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	run := func(scenario string) error {
		_, err := p.Execute(context.Background(), provider.Input{
			Scenario: scenario,
			Step:     newStep("run"),
			Config:   sampleConfigCty(),
			Mobile: &provider.MobileExecution{
				Platform:   "ios",
				TargetName: "iphone",
				Launch:     &provider.MobileLaunchExec{ClearState: true},
				Actions: []provider.MobileActionExec{
					{Kind: model.MobileActionWaitVisible, ID: "welcome.register", Timeout: time.Second},
				},
				Terminate: &provider.MobileTerminateExec{},
			},
		})

		return err
	}

	if err := run("scenario A"); err != nil {
		t.Fatalf("scenario A: %v", err)
	}

	if err := run("scenario B"); err != nil {
		t.Fatalf("scenario B (the #41 regression): %v", err)
	}

	if len(drv.launches) != 2 {
		t.Fatalf("expected 2 driver launches (one per scenario), got %d (%v)", len(drv.launches), drv.launches)
	}

	if len(drv.terminatesDrv) != 2 {
		t.Fatalf("expected 2 driver terminates (one per scenario), got %d (%v)", len(drv.terminatesDrv), drv.terminatesDrv)
	}
}

func TestAcquireSessionBuildsConcurrentlyAcrossTargets(t *testing.T) {
	t.Parallel()

	// Use a builder that blocks until released, so we can prove two Builds
	// can be in flight at the same time for two different targets.
	release := make(chan struct{})
	inFlight := make(chan string, 2)
	released := make(chan string, 2)

	builder := SessionBuilderFunc(func(_ context.Context, target Target) (*Session, error) {
		inFlight <- target.Name
		<-release
		released <- target.Name

		return &Session{
			Target:    target,
			DeviceID:  "UDID-" + target.Name,
			Driver:    &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}},
			Lifecycle: (&fakeLifecycle{udid: "UDID-" + target.Name}),
		}, nil
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(""))

	type result struct {
		sess *Session
		err  error
	}

	out := make(chan result, 2)

	for _, name := range []string{"iphone-a", "iphone-b"} {
		go func(target string) {
			sess, err := p.acquireSession(context.Background(), Target{Name: target, Platform: "ios"})
			out <- result{sess: sess, err: err}
		}(name)
	}

	// Both Build calls should arrive before either is released — that's the
	// property that fails under a global lock around Build.
	got := map[string]bool{}
	timeout := time.After(2 * time.Second)

	for len(got) < 2 {
		select {
		case name := <-inFlight:
			got[name] = true
		case <-timeout:
			t.Fatalf("only %d Build calls started concurrently: %v", len(got), got)
		}
	}

	close(release)

	for range 2 {
		select {
		case r := <-out:
			if r.err != nil {
				t.Fatalf("acquireSession returned error: %v", r.err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for acquireSession to return")
		}
	}

	if len(released) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(released))
	}
}

func TestAcquireSessionSerializesSameTarget(t *testing.T) {
	t.Parallel()

	// Two concurrent acquires for the same target must result in exactly one
	// Build call: the second waits on the per-target lock, then sees the
	// cached session on the post-lock double-check.
	release := make(chan struct{})

	var builds atomic.Int32

	builder := SessionBuilderFunc(func(_ context.Context, target Target) (*Session, error) {
		builds.Add(1)
		<-release

		return &Session{
			Target:    target,
			DeviceID:  "UDID",
			Driver:    &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}},
			Lifecycle: (&fakeLifecycle{udid: "UDID"}),
		}, nil
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(""))

	target := Target{Name: "iphone", Platform: "ios"}
	done := make(chan struct{}, 2)

	for range 2 {
		go func() {
			_, _ = p.acquireSession(context.Background(), target)
			done <- struct{}{}
		}()
	}

	// Give both goroutines time to reach the per-target lock.
	time.Sleep(50 * time.Millisecond)
	close(release)

	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for acquireSession to return")
		}
	}

	if got := builds.Load(); got != 1 {
		t.Fatalf("expected exactly 1 Build call, got %d", got)
	}
}

// newTextOnlyHierarchy mirrors a screen that ships no identifiers at
// all — system dialogs and most third-party UI — where visible text is
// the only handle a scenario has.
func newTextOnlyHierarchy() *tree.ViewNode {
	return &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{Text: "Cancel", Visible: true, Enabled: true, Bounds: tree.Rect{X: 0, Y: 40, Width: 80, Height: 30}},
			{Text: "Allow", Visible: true, Enabled: true, Bounds: tree.Rect{X: 100, Y: 40, Width: 80, Height: 30}},
		},
	}
}

func TestExecuteTapByText(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newTextOnlyHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "permissions",
		Step:     newStep("tap-text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, Text: "Allow"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(drv.taps) != 1 {
		t.Fatalf("expected 1 tap, got %d", len(drv.taps))
	}

	tap := drv.taps[0]

	// The authored text rides along so the driver can re-resolve
	// against the live UI rather than trusting stale coordinates.
	if tap.text != "Allow" {
		t.Fatalf("expected the driver to receive text=\"Allow\", got %q", tap.text)
	}

	// Center of (100,40)-(180,70).
	if tap.x != 140 || tap.y != 55 {
		t.Fatalf("expected the tap on the Allow button center (140,55), got %+v", tap)
	}
}

func TestExecuteExpectVisibleByText(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newTextOnlyHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "permissions",
		Step:     newStep("visible-text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Expect: provider.MobileExpectExec{
				Visible: []provider.MobileVisibilityExec{
					{Text: "Cancel", Timeout: 50 * time.Millisecond},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected the visibility expectation to resolve by text, got %v", err)
	}
}

func TestExecuteMissingTextSurfacesTheLocatorInTheError(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newTextOnlyHierarchy()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "permissions",
		Step:     newStep("tap-missing-text"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionTap, Text: "Absent", Timeout: 30 * time.Millisecond},
			},
		},
	})
	if err == nil {
		t.Fatal("expected an error for a text locator that matches nothing")
	}

	if !strings.Contains(err.Error(), `text="Absent"`) {
		t.Fatalf("error should cite the text locator, got %v", err)
	}
}

// hostLaunchLifecycle adds the optional HostAppLauncher capability to the
// plain fake, so these tests exercise the path the iOS backend takes:
// cold start from the host, then Activate to re-bind the driver.
type hostLaunchLifecycle struct {
	fakeLifecycle

	launches  []string
	launchErr error
}

func (h *hostLaunchLifecycle) LaunchApp(_ context.Context, _ string, target Target) error {
	h.launches = append(h.launches, target.AppID)

	return h.launchErr
}

func newProviderWithHostLauncher(drv *fakeDriverAll, lifecycle Lifecycle, target Target) *Provider {
	builder := SessionBuilderFunc(func(_ context.Context, _ Target) (*Session, error) {
		return &Session{
			Target:    target,
			DeviceID:  "UDID",
			Driver:    drv,
			Lifecycle: lifecycle,
		}, nil
	})

	return New(WithSessionBuilder(builder), WithArtifactsBase(""))
}

func launchInput(scenario string) provider.Input {
	return provider.Input{
		Scenario: scenario,
		Step:     newStep("launch"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Launch:     &provider.MobileLaunchExec{ClearState: true},
		},
	}
}

func TestExecuteLaunchesFromTheHostWhenTheBackendCan(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &hostLaunchLifecycle{}
	p := newProviderWithHostLauncher(drv, lc, sampleProviderTarget())

	if _, err := p.Execute(context.Background(), launchInput("demo")); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(lc.launches) != 1 || lc.launches[0] != "com.example.MyApp" {
		t.Fatalf("expected one host launch, got %v", lc.launches)
	}

	if len(drv.activates) != 1 || drv.activates[0] != "com.example.MyApp" {
		t.Fatalf("expected one driver activate, got %v", drv.activates)
	}

	// The whole point of the split: the driver never runs the launch, so a
	// simulator refusing to open the app cannot become an XCTest failure
	// that tears the runner down.
	if len(drv.launches) != 0 {
		t.Fatalf("driver launch must not be used when the host can launch, got %v", drv.launches)
	}
}

// The #41 regression, on the host-launch path: a second scenario reusing
// the session must still get a working hierarchy, which is what Activate
// re-binds.
func TestExecuteHostLaunchRebindsTheDriverAcrossScenarios(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies:             []*tree.ViewNode{newButtonNode()},
		requireBindForHierarchy: true,
	}
	lc := &hostLaunchLifecycle{}
	p := newProviderWithHostLauncher(drv, lc, sampleProviderTarget())

	run := func(scenario string) error {
		_, err := p.Execute(context.Background(), provider.Input{
			Scenario: scenario,
			Step:     newStep("run"),
			Config:   sampleConfigCty(),
			Mobile: &provider.MobileExecution{
				Platform:   "ios",
				TargetName: "iphone",
				Launch:     &provider.MobileLaunchExec{ClearState: true},
				Actions: []provider.MobileActionExec{
					{Kind: model.MobileActionWaitVisible, ID: "welcome.register", Timeout: time.Second},
				},
				Terminate: &provider.MobileTerminateExec{},
			},
		})

		return err
	}

	if err := run("scenario A"); err != nil {
		t.Fatalf("scenario A: %v", err)
	}

	if err := run("scenario B"); err != nil {
		t.Fatalf("scenario B (the #41 regression on the host-launch path): %v", err)
	}
}

func TestExecuteFailsTheStepWhenTheHostLaunchFails(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}}
	lc := &hostLaunchLifecycle{launchErr: errors.New(`Application "com.example.MyApp" is unknown to FrontBoard`)}
	p := newProviderWithHostLauncher(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), launchInput("demo"))
	if err == nil {
		t.Fatal("expected the step to fail")
	}

	// The simulator's own words must reach the report: this is the message
	// that says the app was not installed, not a locator problem.
	if !strings.Contains(err.Error(), "unknown to FrontBoard") {
		t.Fatalf("error should carry the simulator message, got: %v", err)
	}

	if len(drv.activates) != 0 {
		t.Fatalf("a failed launch must not be followed by an activate, got %v", drv.activates)
	}
}

// logDumpingLifecycle adds the optional DeviceLogDumper capability, which
// only Android provides in production.
type logDumpingLifecycle struct {
	fakeLifecycle

	devices []string
	err     error
}

func (l *logDumpingLifecycle) CaptureDeviceLog(_ context.Context, deviceID, path string) error {
	l.devices = append(l.devices, deviceID)

	if l.err != nil {
		return l.err
	}

	return os.WriteFile(path, []byte("ANR in com.google.android.apps.nexuslauncher\n"), 0o600)
}

func runFailingStep(t *testing.T, lifecycle Lifecycle, base string) *provider.Output {
	t.Helper()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{
		{ID: "root", Visible: true},
		{ID: "root", Visible: true},
	}}

	builder := SessionBuilderFunc(func(_ context.Context, _ Target) (*Session, error) {
		return &Session{
			Target:    sampleProviderTarget(),
			DeviceID:  "UDID",
			Driver:    drv,
			Lifecycle: lifecycle,
		}, nil
	})

	out, err := New(WithSessionBuilder(builder), WithArtifactsBase(base)).
		Execute(context.Background(), provider.Input{
			Scenario: "demo",
			Step:     newStep("fail"),
			Config:   sampleConfigCty(),
			Mobile: &provider.MobileExecution{
				Platform:   "ios",
				TargetName: "iphone",
				Actions: []provider.MobileActionExec{
					{Kind: model.MobileActionTap, ID: "does.not.exist", Timeout: 30 * time.Millisecond},
				},
			},
		})
	if err == nil {
		t.Fatal("expected the step to fail")
	}

	return out
}

func artifactPathOfType(t *testing.T, out *provider.Output, want string) (string, bool) {
	t.Helper()

	artifacts, ok := out.Response["artifacts"]
	if !ok {
		return "", false
	}

	for _, a := range artifacts.AsValueSlice() {
		if a.GetAttr(artifactTypeKey).AsString() == want {
			return a.GetAttr(artifactPathKey).AsString(), true
		}
	}

	return "", false
}

func TestExecuteAttachesTheDeviceLogOnFailure(t *testing.T) {
	t.Parallel()

	lc := &logDumpingLifecycle{}
	base := t.TempDir()

	out := runFailingStep(t, lc, base)

	path, ok := artifactPathOfType(t, out, ArtifactTypeLogcat)
	if !ok {
		t.Fatalf("expected a %s artifact, got %+v", ArtifactTypeLogcat, out.Response["artifacts"])
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read device log: %v", err)
	}

	if !strings.Contains(string(body), "ANR in") {
		t.Fatalf("device log was not written through: %q", body)
	}

	if len(lc.devices) != 1 || lc.devices[0] != "UDID" {
		t.Fatalf("expected one dump for the session device, got %v", lc.devices)
	}
}

// A backend without the capability must still produce the other
// artifacts: the device log is a bonus, not a requirement.
func TestExecuteSkipsTheDeviceLogWhenTheBackendCannotDumpIt(t *testing.T) {
	t.Parallel()

	out := runFailingStep(t, &fakeLifecycle{udid: "UDID"}, t.TempDir())

	if _, ok := artifactPathOfType(t, out, ArtifactTypeLogcat); ok {
		t.Fatal("a backend without DeviceLogDumper must not report a device log")
	}

	if _, ok := artifactPathOfType(t, out, artifactKindScreenshot); !ok {
		t.Fatal("the other failure artifacts must still be attached")
	}
}

// A failing dump must not cost the step its screenshot and hierarchy:
// it is the least important of the three and runs last for that reason.
func TestExecuteToleratesADeviceLogDumpFailure(t *testing.T) {
	t.Parallel()

	lc := &logDumpingLifecycle{err: errors.New("adb: device offline")}

	out := runFailingStep(t, lc, t.TempDir())

	if _, ok := artifactPathOfType(t, out, ArtifactTypeLogcat); ok {
		t.Fatal("a failed dump must not be reported as an artifact")
	}

	if _, ok := artifactPathOfType(t, out, artifactKindScreenshot); !ok {
		t.Fatal("the screenshot must survive a failed device log dump")
	}
}

// flakyHierarchyDriver refuses the first refusals calls, the way the
// driver answers 503 while a previous snapshot is still in flight.
type flakyHierarchyDriver struct {
	driver.NoopDriver

	refusals int
	calls    int
}

func (d *flakyHierarchyDriver) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	d.calls++

	if d.calls <= d.refusals {
		return nil, errors.New("driver /hierarchy returned 503: snapshot busy; retry")
	}

	return &tree.ViewNode{ID: "root", Visible: true}, nil
}

func TestFetchHierarchyForArtifactsRetriesABusyDriver(t *testing.T) {
	t.Parallel()

	drv := &flakyHierarchyDriver{refusals: 2}
	session := &Session{Target: sampleProviderTarget(), Driver: drv}

	node, err := fetchHierarchyForArtifacts(context.Background(), session)
	if err != nil {
		t.Fatalf("expected the third attempt to succeed: %v", err)
	}

	if node == nil || node.ID != "root" {
		t.Fatalf("unexpected tree: %+v", node)
	}

	if drv.calls != 3 {
		t.Fatalf("calls = %d, want 3", drv.calls)
	}
}

// A driver that never answers must not be retried forever, and the
// caller still falls back to the last recorded tree.
func TestFetchHierarchyForArtifactsGivesUp(t *testing.T) {
	t.Parallel()

	drv := &flakyHierarchyDriver{refusals: 99}
	session := &Session{Target: sampleProviderTarget(), Driver: drv}

	_, err := fetchHierarchyForArtifacts(context.Background(), session)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !strings.Contains(err.Error(), "snapshot busy") {
		t.Fatalf("error should carry the driver's message, got: %v", err)
	}

	if drv.calls != artifactHierarchyAttempts {
		t.Fatalf("calls = %d, want %d", drv.calls, artifactHierarchyAttempts)
	}
}

// Artifact capture runs after a step already failed, often on a context
// that is about to expire: the retry must not outlive it.
func TestFetchHierarchyForArtifactsStopsOnACancelledContext(t *testing.T) {
	t.Parallel()

	drv := &flakyHierarchyDriver{refusals: 99}
	session := &Session{Target: sampleProviderTarget(), Driver: drv}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := fetchHierarchyForArtifacts(ctx, session); err == nil {
		t.Fatal("expected an error")
	}

	if drv.calls != 1 {
		t.Fatalf("a cancelled context should stop after the first attempt, got %d", drv.calls)
	}
}

// newTextButtonNode returns a hierarchy whose only leaf is reachable by
// its visible text, with no accessibility id at all. It is the shape a
// `text = "..."` locator exists for.
func newTextButtonNode() *tree.ViewNode {
	return &tree.ViewNode{
		ID:      "root",
		Visible: true,
		Enabled: true,
		Children: []*tree.ViewNode{
			{
				Text:    "Sign in",
				Value:   "user@example.com",
				Visible: true,
				Enabled: true,
				Bounds:  tree.Rect{X: 10, Y: 20, Width: 100, Height: 40},
			},
		},
	}
}

// TestExecuteExpectationsAcceptTextLocator pins the text locator through
// the enabled / disabled / text / value expectations.
//
// The parser accepts `text` on every expect block and the runtime fills
// MobileStateExpectationExec.Text, but the provider used to build its
// elementLocator from ID and Label only. A `text = "Sign in"` expect
// therefore polled an empty id, found nothing, and failed with
// `element  not found` instead of asserting anything.
func TestExecuteExpectationsAcceptTextLocator(t *testing.T) {
	t.Parallel()

	disabled := newTextButtonNode()
	disabled.Children[0].Enabled = false

	for name, tc := range map[string]struct {
		hierarchy *tree.ViewNode
		expect    provider.MobileExpectExec
	}{
		"enabled": {
			hierarchy: newTextButtonNode(),
			expect: provider.MobileExpectExec{Enabled: []provider.MobileStateExpectationExec{
				{Text: "Sign in", Timeout: 200 * time.Millisecond},
			}},
		},
		"disabled": {
			hierarchy: disabled,
			expect: provider.MobileExpectExec{Disabled: []provider.MobileStateExpectationExec{
				{Text: "Sign in", Timeout: 200 * time.Millisecond},
			}},
		},
		"text": {
			hierarchy: newTextButtonNode(),
			expect: provider.MobileExpectExec{Text: []provider.MobileValueExpectationExec{
				{Text: "Sign in", Expected: cty.StringVal("Sign in"), Timeout: 200 * time.Millisecond},
			}},
		},
		"value": {
			hierarchy: newTextButtonNode(),
			expect: provider.MobileExpectExec{Value: []provider.MobileValueExpectationExec{
				{Text: "Sign in", Expected: cty.StringVal("user@example.com"), Timeout: 200 * time.Millisecond},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{tc.hierarchy}}
			lc := &fakeLifecycle{udid: "UDID"}
			p := newProviderWithFake(drv, lc, sampleProviderTarget())

			_, err := p.Execute(context.Background(), provider.Input{
				Scenario: "demo",
				Step:     newStep(name),
				Config:   sampleConfigCty(),
				Mobile: &provider.MobileExecution{
					Platform:   "ios",
					TargetName: "iphone",
					Expect:     tc.expect,
				},
			})
			if err != nil {
				t.Fatalf("text-locator expectation should pass: %v", err)
			}
		})
	}
}

// TestExecuteWaitEnabledUnlocksBeforeTap covers the case the action
// exists for: the element renders while still inert, and the tap must
// not fire until it accepts input. A tap on a disabled control is
// swallowed by both platforms with no error, so without the wait the
// scenario fails later, somewhere unrelated.
func TestExecuteWaitEnabledUnlocksBeforeTap(t *testing.T) {
	t.Parallel()

	inert := newButtonNode()
	inert.Children[0].Enabled = false

	armed := newButtonNode()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{inert, inert, armed}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("submit"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionWaitEnabled,
					ID:       "welcome.register",
					Timeout:  2 * time.Second,
					Interval: 10 * time.Millisecond,
				},
				{Kind: model.MobileActionTap, ID: "welcome.register", Timeout: time.Second},
			},
		},
	})
	if err != nil {
		t.Fatalf("wait_enabled then tap should pass: %v", err)
	}

	drv.mu.Lock()
	taps := append([]fakeTap(nil), drv.taps...)
	drv.mu.Unlock()

	if len(taps) != 1 {
		t.Fatalf("expected exactly one tap, got %d", len(taps))
	}

	if taps[0].id != "welcome.register" {
		t.Fatalf("unexpected tap target %q", taps[0].id)
	}
}

func TestExecuteWaitDisabledPasses(t *testing.T) {
	t.Parallel()

	inert := newButtonNode()
	inert.Children[0].Enabled = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode(), inert}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("lock"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionWaitDisabled,
					ID:       "welcome.register",
					Timeout:  2 * time.Second,
					Interval: 10 * time.Millisecond,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("wait_disabled should pass: %v", err)
	}
}

// TestExecuteWaitEnabledAcceptsTextLocator pins stateFromAction carrying
// the text locator across, the same threading rule visibilityFromAction
// documents.
func TestExecuteWaitEnabledAcceptsTextLocator(t *testing.T) {
	t.Parallel()

	inert := newTextButtonNode()
	inert.Children[0].Enabled = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{inert, newTextButtonNode()}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("submit"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionWaitEnabled,
					Text:     "Sign in",
					Timeout:  2 * time.Second,
					Interval: 10 * time.Millisecond,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("wait_enabled with a text locator should pass: %v", err)
	}
}

func TestExecuteWaitEnabledTimesOutWithLastSeenState(t *testing.T) {
	t.Parallel()

	inert := newButtonNode()
	inert.Children[0].Enabled = false

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{inert}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("submit"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionWaitEnabled,
					ID:       "welcome.register",
					Timeout:  100 * time.Millisecond,
					Interval: 10 * time.Millisecond,
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}

	msg := err.Error()
	if !strings.Contains(msg, `element id="welcome.register" was not enabled after`) {
		t.Fatalf("expected the not-enabled summary, got %v", msg)
	}

	if !strings.Contains(msg, "last seen enabled=false") {
		t.Fatalf("expected the last-seen state in the message, got %v", msg)
	}
}

// TestExecuteClearTextNamesItsTarget pins the contract issue #63 broke:
// the erase must name the element it is clearing.
//
// Tales used to send /eraseText with a character count and nothing else,
// so the driver could only delete from whatever held input focus. When
// the focus had not followed the preceding tap — which is a race on
// Compose, not a certainty — the deletes landed in a different field,
// and clearing an already-empty one sent a full default-length burst at
// it. The scenario then lost a value it had typed two actions earlier,
// and nothing in the failure pointed at clear_text.
func TestExecuteClearTextNamesItsTarget(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Value = "abcde"

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("clear"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionClearText, ID: "welcome.register"},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drv.mu.Lock()
	erases := append([]fakeErase(nil), drv.erases...)
	drv.mu.Unlock()

	if len(erases) != 1 {
		t.Fatalf("expected one erase, got %d", len(erases))
	}

	if erases[0].locator.ID != "welcome.register" {
		t.Fatalf("erase must carry the element locator, got %+v", erases[0].locator)
	}

	if erases[0].characters != 5 {
		t.Fatalf("expected erase=5, got %d", erases[0].characters)
	}
}

// TestExecuteClearTextCarriesLabelAndTextLocators mirrors the threading
// rule the wait helpers document: a locator field that is not carried
// across leaves the driver guessing.
func TestExecuteClearTextCarriesLabelAndTextLocators(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		action  provider.MobileActionExec
		want    driver.Locator
		asLabel bool
	}{
		"label": {
			action:  provider.MobileActionExec{Kind: model.MobileActionClearText, Label: "Sign in"},
			want:    driver.Locator{Label: "Sign in"},
			asLabel: true,
		},
		"text": {
			action: provider.MobileActionExec{Kind: model.MobileActionClearText, Text: "Sign in"},
			want:   driver.Locator{Text: "Sign in"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			node := newTextButtonNode()
			node.Children[0].Value = "abcde"

			if tc.asLabel {
				node.Children[0].Text = ""
				node.Children[0].Label = "Sign in"
			}

			drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{node}}
			lc := &fakeLifecycle{udid: "UDID"}
			p := newProviderWithFake(drv, lc, sampleProviderTarget())

			_, err := p.Execute(context.Background(), provider.Input{
				Scenario: "demo",
				Step:     newStep("clear"),
				Config:   sampleConfigCty(),
				Mobile: &provider.MobileExecution{
					Platform:   "ios",
					TargetName: "iphone",
					Actions:    []provider.MobileActionExec{tc.action},
				},
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}

			drv.mu.Lock()
			erases := append([]fakeErase(nil), drv.erases...)
			drv.mu.Unlock()

			if len(erases) != 1 {
				t.Fatalf("expected one erase, got %d", len(erases))
			}

			if erases[0].locator.Label != tc.want.Label || erases[0].locator.Text != tc.want.Text {
				t.Fatalf("erase locator: want %+v got %+v", tc.want, erases[0].locator)
			}
		})
	}
}

// TestExecuteScrollToWaitsForTheElement covers issue #64: scroll_to used
// to dispatch immediately, so an element that the screen was still
// building was a 404 rather than something to wait for. The scenario
// that reads as tolerant — scroll_to then wait_visible — was on the
// wrong side of the race, and failed on the step that looks best-effort.
func TestExecuteScrollToWaitsForTheElement(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies:      []*tree.ViewNode{newButtonNode()},
		scrollToFailures: 2,
	}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("scroll"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionScrollTo,
					ID:       "welcome.register",
					Timeout:  2 * time.Second,
					Interval: 10 * time.Millisecond,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("scroll_to should wait for a late element: %v", err)
	}

	drv.mu.Lock()
	scrollTos := append([]fakeScrollTo(nil), drv.scrollTos...)
	drv.mu.Unlock()

	if len(scrollTos) != 3 {
		t.Fatalf("expected the dispatch to be retried until it took, got %d", len(scrollTos))
	}

	if scrollTos[0].id != "welcome.register" {
		t.Fatalf("expected the element locator on every dispatch, got %v", scrollTos)
	}
}

func TestExecuteScrollToTimesOutOnAMissingElement(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{
		hierarchies:      []*tree.ViewNode{newButtonNode()},
		scrollToFailures: 1000,
	}
	lc := &fakeLifecycle{udid: "UDID"}
	p := newProviderWithFake(drv, lc, sampleProviderTarget())

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("scroll"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{
					Kind:     model.MobileActionScrollTo,
					ID:       "never.appears",
					Timeout:  100 * time.Millisecond,
					Interval: 10 * time.Millisecond,
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected a timeout error for an element that never appears")
	}

	if !strings.Contains(err.Error(), `id="never.appears": gave up after`) {
		t.Fatalf("expected the give-up summary naming the element, got %v", err)
	}

	if !strings.Contains(err.Error(), "scroll to: element not found") {
		t.Fatalf("expected the driver's own reason to survive, got %v", err)
	}

	drv.mu.Lock()
	scrollTos := len(drv.scrollTos)
	drv.mu.Unlock()

	if scrollTos < 2 {
		t.Fatalf("expected the dispatch to be retried before giving up, got %d", scrollTos)
	}
}
