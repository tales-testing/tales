package mobile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

type fakeDriverAll struct {
	mu              sync.Mutex
	hierarchies     []*tree.ViewNode
	hierarchyErr    error
	taps            []struct{ x, y float64 }
	tapErr          error
	inputs          []string
	erases          []int
	screenshotPNG   []byte
	screenshotErr   error
	healthErr       error
	hierarchyDelay  time.Duration
	activeHierarchy atomic.Int32
	maxHierarchy    atomic.Int32
	hierarchyCalls  atomic.Int32
}

func (f *fakeDriverAll) Health(_ context.Context) error { return f.healthErr }

func (f *fakeDriverAll) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	f.hierarchyCalls.Add(1)
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

func (f *fakeDriverAll) Tap(_ context.Context, _ string, x, y float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.taps = append(f.taps, struct{ x, y float64 }{x, y})

	return f.tapErr
}

func (f *fakeDriverAll) InputText(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.inputs = append(f.inputs, text)

	return nil
}

func (f *fakeDriverAll) EraseText(_ context.Context, _ string, count int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.erases = append(f.erases, count)

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

type fakeLifecycle struct {
	udid       string
	terminates atomic.Int32
}

func (f *fakeLifecycle) toAppleLifecycle() *apple.Lifecycle {
	return &apple.Lifecycle{Simctl: &noopSimctl{terminates: &f.terminates}}
}

type noopSimctl struct {
	terminates *atomic.Int32
}

func (n *noopSimctl) FindDeviceByName(_ context.Context, _ string) (apple.Device, error) {
	return apple.Device{UDID: "UDID"}, nil
}

func (n *noopSimctl) Boot(_ context.Context, _ string) error { return nil }
func (*noopSimctl) WaitBooted(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (*noopSimctl) Install(_ context.Context, _, _ string) error   { return nil }
func (*noopSimctl) Uninstall(_ context.Context, _, _ string) error { return nil }
func (*noopSimctl) Launch(_ context.Context, _, _ string) error    { return nil }

func (n *noopSimctl) Terminate(_ context.Context, _, _ string) error {
	if n.terminates != nil {
		n.terminates.Add(1)
	}

	return nil
}

func (*noopSimctl) Screenshot(_ context.Context, _, _ string) error { return nil }

func newProviderWithFake(drv *fakeDriverAll, lifecycle *fakeLifecycle, target apple.Target) *Provider {
	builder := SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
		return &Session{
			Target:    target,
			UDID:      lifecycle.udid,
			Driver:    drv,
			Lifecycle: lifecycle.toAppleLifecycle(),
		}, nil
	})

	return New(WithSessionBuilder(builder), WithArtifactsBase(""))
}

func sampleProviderTarget() apple.Target {
	return apple.Target{
		Name:       "iphone",
		Platform:   "ios",
		DeviceName: "iPhone 16",
		AppPath:    "./MyApp.app",
		BundleID:   "com.example.MyApp",
		Driver:     apple.DriverConfig{Host: "127.0.0.1", Port: 9080},
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

	if len(drv.inputs) != 1 || drv.inputs[0] != "hello" {
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

	if len(drv.erases) != 1 || drv.erases[0] != 5 {
		t.Fatalf("expected erase=5, got %v", drv.erases)
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
	if err == nil || !strings.Contains(err.Error(), `text mismatch for "welcome.register"`) {
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
	if !strings.Contains(msg, `element "welcome.register" not found after`) {
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
	if !strings.Contains(msg, `text mismatch for "welcome.register"`) {
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
	builder := SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
		return &Session{
			Target:    sampleProviderTarget(),
			UDID:      lc.udid,
			Driver:    drv,
			Lifecycle: lc.toAppleLifecycle(),
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

	builder := SessionBuilderFunc(func(_ context.Context, _ apple.Target) (*Session, error) {
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

func TestAcquireSessionBuildsConcurrentlyAcrossTargets(t *testing.T) {
	t.Parallel()

	// Use a builder that blocks until released, so we can prove two Builds
	// can be in flight at the same time for two different targets.
	release := make(chan struct{})
	inFlight := make(chan string, 2)
	released := make(chan string, 2)

	builder := SessionBuilderFunc(func(_ context.Context, target apple.Target) (*Session, error) {
		inFlight <- target.Name
		<-release
		released <- target.Name

		return &Session{
			Target:    target,
			UDID:      "UDID-" + target.Name,
			Driver:    &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}},
			Lifecycle: (&fakeLifecycle{udid: "UDID-" + target.Name}).toAppleLifecycle(),
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
			sess, err := p.acquireSession(context.Background(), apple.Target{Name: target, Platform: "ios"})
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

	builder := SessionBuilderFunc(func(_ context.Context, target apple.Target) (*Session, error) {
		builds.Add(1)
		<-release

		return &Session{
			Target:    target,
			UDID:      "UDID",
			Driver:    &fakeDriverAll{hierarchies: []*tree.ViewNode{newButtonNode()}},
			Lifecycle: (&fakeLifecycle{udid: "UDID"}).toAppleLifecycle(),
		}, nil
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(""))

	target := apple.Target{Name: "iphone", Platform: "ios"}
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
