package browser

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

func TestProviderPointerActionsReportTheirPoint(t *testing.T) {
	t.Parallel()

	p, fake := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.PointerPoint = &driver.Point{X: 12.5, Y: 40}
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions: []provider.BrowserActionExec{
			{Kind: model.BrowserActionClick, Selector: "#behind", Force: true, Interval: 50 * time.Millisecond},
			{Kind: model.BrowserActionHover, Selector: "#menu"},
			{Kind: model.BrowserActionCheck, Selector: "#tos"},
			{Kind: model.BrowserActionUncheck, Selector: "#news", Force: true},
			{Kind: model.BrowserActionFill, Selector: "#email", Value: "a@example.com"},
		},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "pointer", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}

	wantCalls := []driver.Call{
		{Method: "Click", Selector: "#behind", Force: true, Interval: 50 * time.Millisecond},
		{Method: "Hover", Selector: "#menu"},
		{Method: "Check", Selector: "#tos"},
		{Method: "Uncheck", Selector: "#news", Force: true},
	}

	for i, want := range wantCalls {
		if fake.Calls[i] != want {
			t.Errorf("call[%d] = %+v, want %+v", i, fake.Calls[i], want)
		}
	}

	for i := range 4 {
		got := out.ActionResults[i].Point
		if got == nil || *got != (provider.ActionPoint{X: 12.5, Y: 40}) {
			t.Errorf("ActionResults[%d].Point = %+v, want {12.5 40}", i, got)
		}
	}

	if out.ActionResults[4].Point != nil {
		t.Errorf("a fill has no pointer, got Point = %+v", out.ActionResults[4].Point)
	}

	if got := out.ActionResults[0].Label; got != "click #behind (force)" {
		t.Errorf("label = %q, want %q", got, "click #behind (force)")
	}
}

func TestProviderNotActionableKeepsTheTestedPoint(t *testing.T) {
	t.Parallel()

	notActionable := &driver.ActionabilityError{
		Action:   "click",
		Selector: "#covered",
		Reason:   `point (912, 684) is covered by <div class="toast">`,
		Point:    &driver.Point{X: 912, Y: 684},
		Timeout:  2 * time.Second,
	}

	p, _ := buildFakeProvider(t, provider.CaptureNone, func(f *driver.FakeDriver) {
		f.FailOnSelector = map[string]error{"#covered": notActionable}
	})
	defer p.Close()

	exec := &provider.BrowserExecution{
		TargetName: "chrome",
		Actions: []provider.BrowserActionExec{
			{Kind: model.BrowserActionClick, Selector: "#covered"},
			{Kind: model.BrowserActionWaitVisible, Selector: "#next"},
		},
	}

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "pointer", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})
	if err == nil {
		t.Fatal("expected the covered click to fail the step")
	}

	want := `action 0 (click): driver: click #covered: not actionable after 2s: point (912, 684) is covered by <div class="toast">`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}

	failed := out.ActionResults[0]
	if failed.Status != actionStatusFail {
		t.Fatalf("status = %q, want fail", failed.Status)
	}

	if failed.Point == nil || *failed.Point != (provider.ActionPoint{X: 912, Y: 684}) {
		t.Errorf("Point = %+v, want the covered point {912 684}", failed.Point)
	}

	if out.ActionResults[1].Status != actionStatusSkip {
		t.Errorf("the wait after a failed click must be skipped, got %q", out.ActionResults[1].Status)
	}
}
