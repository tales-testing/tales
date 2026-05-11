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
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

type fakeDriverAll struct {
	mu             sync.Mutex
	hierarchies    []*tree.ViewNode
	hierarchyErr   error
	taps           []struct{ x, y float64 }
	tapErr         error
	inputs         []string
	erases         []int
	screenshotPNG  []byte
	screenshotErr  error
	healthErr      error
	hierarchyCalls atomic.Int32
}

func (f *fakeDriverAll) Health(_ context.Context) error { return f.healthErr }

func (f *fakeDriverAll) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	f.hierarchyCalls.Add(1)

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

func (f *fakeDriverAll) Tap(_ context.Context, x, y float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.taps = append(f.taps, struct{ x, y float64 }{x, y})

	return f.tapErr
}

func (f *fakeDriverAll) InputText(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.inputs = append(f.inputs, text)

	return nil
}

func (f *fakeDriverAll) EraseText(_ context.Context, count int) error {
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

func (n *noopSimctl) Boot(_ context.Context, _ string) error    { return nil }
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
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
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
				{Kind: model.MobileActionTap, ID: "does.not.exist"},
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
