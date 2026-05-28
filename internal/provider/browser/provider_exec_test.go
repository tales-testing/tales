package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/zclconf/go-cty/cty"
)

// buildFakeProvider builds a Provider whose SessionBuilder returns a fake
// driver fed by the supplied configurator.
func buildFakeProvider(t *testing.T, mode provider.CaptureMode, configure func(*driver.FakeDriver)) (*Provider, *driver.FakeDriver) {
	t.Helper()

	fake := driver.NewFakeDriver()
	if configure != nil {
		configure(fake)
	}

	builder := SessionBuilderFunc{
		BuildFn: func(_ context.Context, target Target) (*Session, error) {
			return &Session{TargetName: target.Name, Target: target}, nil
		},
		ScenarioFn: func(_ context.Context, _ *Session, _ string) (*ScenarioBrowserCtx, error) {
			return &ScenarioBrowserCtx{Driver: fake}, nil
		},
	}

	p := New(WithSessionBuilder(builder), WithCaptureMode(mode), WithArtifactsBase(t.TempDir()))

	return p, fake
}

func sampleConfig() map[string]cty.Value {
	return map[string]cty.Value{
		"browser": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"chrome": cty.ObjectVal(map[string]cty.Value{
					"browser":  cty.StringVal("chrome"),
					"headless": cty.True,
				}),
			}),
		}),
	}
}

func TestProviderExecuteOrderedActions(t *testing.T) {
	t.Parallel()

	p, fake := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.Visibility["#title"] = true
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions: []provider.BrowserActionExec{
			{Kind: model.BrowserActionGoto, URL: "https://example.com/"},
			{Kind: model.BrowserActionClick, Selector: "#cta"},
			{Kind: model.BrowserActionFill, Selector: "#email", Value: "test@example.com"},
			{Kind: model.BrowserActionWaitVisible, Selector: "#title"},
		},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "open", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}

	if got := fake.MethodsCalled(); len(got) < 4 {
		t.Fatalf("expected at least 4 calls, got %v", got)
	}

	want := []string{"Goto", "Click", "Fill", "WaitVisible"}

	for i, w := range want {
		if fake.Calls[i].Method != w {
			t.Errorf("call[%d] = %q, want %q", i, fake.Calls[i].Method, w)
		}
	}

	if len(out.ActionResults) != 4 {
		t.Fatalf("expected 4 ActionResults, got %d", len(out.ActionResults))
	}

	for i, r := range out.ActionResults {
		if r.Status != actionStatusPass {
			t.Errorf("ActionResults[%d].Status = %q, want pass", i, r.Status)
		}
	}
}

func TestProviderExecuteSecureFillMasked(t *testing.T) {
	t.Parallel()

	p, _ := buildFakeProvider(t, provider.CaptureNone, nil)
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions: []provider.BrowserActionExec{
			{Kind: model.BrowserActionFill, Selector: "#password", Value: "supersecret", Secure: true},
		},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "open", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}

	if len(out.ActionResults) != 1 {
		t.Fatalf("expected 1 action result, got %d", len(out.ActionResults))
	}

	if out.ActionResults[0].Value != "***" {
		t.Fatalf("Value = %q, want ***", out.ActionResults[0].Value)
	}

	if !strings.Contains(out.ActionResults[0].Label, "***") {
		t.Errorf("Label should mention masked: %q", out.ActionResults[0].Label)
	}
}

func TestProviderExecuteFailMidwayMarksRestSkipped(t *testing.T) {
	t.Parallel()

	p, _ := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.FailOnSelector = map[string]error{"#missing": errors.New("not found")}
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions: []provider.BrowserActionExec{
			{Kind: model.BrowserActionClick, Selector: "#a"},
			{Kind: model.BrowserActionClick, Selector: "#missing"},
			{Kind: model.BrowserActionClick, Selector: "#b"},
		},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "open", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err == nil {
		t.Fatal("expected error when middle action fails")
	}

	if len(out.ActionResults) != 3 {
		t.Fatalf("expected 3 results, got %d", len(out.ActionResults))
	}

	statuses := []string{out.ActionResults[0].Status, out.ActionResults[1].Status, out.ActionResults[2].Status}
	want := []string{actionStatusPass, actionStatusFail, actionStatusSkip}

	for i, w := range want {
		if statuses[i] != w {
			t.Errorf("status[%d] = %q, want %q", i, statuses[i], w)
		}
	}
}

func TestProviderExecuteExpectValueReadsInputValueNotText(t *testing.T) {
	t.Parallel()

	// Regression for the Copilot review on expect.value: the previous
	// implementation read drv.Text() (innerText) which is empty for
	// <input>. expect.value must read the form .value via drv.InputValue.
	p, fake := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.Texts["#email"] = ""
		f.InputValues["#email"] = "test@example.com"
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Expect: provider.BrowserExpectExec{
			Value: []provider.BrowserValueExpectationExec{
				{Selector: "#email", Expected: cty.StringVal("test@example.com")},
			},
		},
	}

	if _, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "verify", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	}); err != nil {
		t.Fatalf("expect.value should pass against InputValue, got: %v", err)
	}

	// Confirm we hit InputValue, not Text.
	saw := false

	for _, c := range fake.Calls {
		if c.Method == "InputValue" && c.Selector == "#email" {
			saw = true

			break
		}
	}

	if !saw {
		t.Fatalf("expected InputValue call, got methods: %v", fake.MethodsCalled())
	}
}

func TestProviderExecuteExpectDisabledBooleanSemantics(t *testing.T) {
	t.Parallel()

	// Regression for the Copilot review on matchEnabled: HTML boolean
	// attributes treat presence as true, regardless of the attribute
	// value. `<input disabled="false">` is still disabled.
	p, _ := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.Attributes["#tos"] = map[string]string{"disabled": "false"}
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Expect: provider.BrowserExpectExec{
			Disabled: []provider.BrowserStateExpectationExec{
				{Selector: "#tos", Timeout: 100 * time.Millisecond},
			},
		},
	}

	if _, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "verify", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	}); err != nil {
		t.Fatalf("disabled=\"false\" must be treated as disabled per HTML semantics, got: %v", err)
	}
}

func TestProviderExecuteExpectURLPasses(t *testing.T) {
	t.Parallel()

	p, _ := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.URLValue = "https://example.com/web/dashboard"
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Expect: provider.BrowserExpectExec{
			URL: []provider.BrowserURLExpectationExec{
				{Expected: cty.StringVal("https://example.com/web/dashboard")},
			},
		},
	}

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "verify", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err != nil {
		t.Fatalf("expected URL match to pass, got: %v", err)
	}
}

func TestProviderRecordsSnapshot(t *testing.T) {
	t.Parallel()

	p, _ := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.URLValue = "https://example.com/dash"
		f.TitleValue = "Dashboard"
		f.OuterHTMLValue = `<html><head><title>Dashboard</title></head><body><h1 data-testid="greeting">Hi</h1></body></html>`
	})
	defer p.Close()

	exec := &provider.BrowserExecution{TargetName: "chrome"}

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "open", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}

	snap, ok := p.LastSnapshot("demo", "open")
	if !ok {
		t.Fatal("snapshot should be recorded")
	}

	if snap.URL != "https://example.com/dash" || snap.Title != "Dashboard" {
		t.Errorf("snapshot mismatch: %+v", snap)
	}

	if !strings.Contains(snap.DOM, "greeting") {
		t.Errorf("snapshot DOM missing expected content: %q", snap.DOM)
	}
}

func TestProviderCaptureFailureWritesArtifacts(t *testing.T) {
	t.Parallel()

	p, _ := buildFakeProvider(t, provider.CaptureFailures, func(f *driver.FakeDriver) {
		f.ScreenshotPNG = []byte{0x89, 0x50, 0x4E, 0x47}
		f.OuterHTMLValue = "<html></html>"
		f.FailOnSelector = map[string]error{"#dead": errors.New("nope")}
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions:    []provider.BrowserActionExec{{Kind: model.BrowserActionClick, Selector: "#dead"}},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "boom", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err == nil {
		t.Fatal("expected click to fail")
	}

	if out == nil || len(out.ActionResults) == 0 {
		t.Fatal("expected action results on failure")
	}

	if out.ActionResults[0].Screenshot == "" {
		t.Errorf("expected screenshot path to be set on failure")
	}
}
