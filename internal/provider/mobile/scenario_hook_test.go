package mobile

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
)

func recordExpr(t *testing.T, src string) model.Expression {
	t.Helper()

	e, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse %q: %s", src, diags.Error())
	}

	return model.Expression{Expr: e, File: "test.hcl", Line: 1}
}

// recordFakeStart is one Start call captured by the fake recorder.
type recordFakeStart struct {
	deviceID string
	opts     RecordOptions
}

// recordFakeFactory is a RecorderFactory that records which device it was
// asked for and hands back a single shared recorder, so a test can assert
// on both the routing and the resulting options.
type recordFakeFactory struct {
	starts   []recordFakeStart
	recorder *recordFakeRecorder
}

func (f *recordFakeFactory) factory() RecorderFactory {
	return func(deviceID string) Recorder {
		if f.recorder == nil {
			f.recorder = &recordFakeRecorder{}
		}

		f.recorder.parent = f
		f.recorder.deviceID = deviceID

		return f.recorder
	}
}

type recordFakeRecorder struct {
	parent    *recordFakeFactory
	deviceID  string
	output    string
	stopCount atomic.Int32
}

func (r *recordFakeRecorder) Start(_ context.Context, opts RecordOptions) error {
	r.output = opts.Output
	r.parent.starts = append(r.parent.starts, recordFakeStart{deviceID: r.deviceID, opts: opts})

	return nil
}

func (r *recordFakeRecorder) Stop(_ context.Context) (string, error) {
	r.stopCount.Add(1)

	return r.output, nil
}

func TestMobileBeginScenarioNoRecordIsNoop(t *testing.T) {
	t.Parallel()

	p := New()
	if err := p.BeginScenario(context.Background(), &model.Scenario{Name: "no-record"}, provider.ScenarioContext{Workdir: t.TempDir()}); err != nil {
		t.Fatalf("BeginScenario: %v", err)
	}
}

func TestMobileBeginScenarioStoresPendingRecord(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	p := New()

	scenario := &model.Scenario{
		Name: "preview",
		Record: &model.ScenarioRecord{
			Output: recordExpr(t, `"preview.mp4"`),
			Codec:  recordExpr(t, `"h264"`),
			Force:  recordExpr(t, `true`),
		},
	}

	if err := p.BeginScenario(context.Background(), scenario, provider.ScenarioContext{Workdir: workdir, ProjectDir: workdir}); err != nil {
		t.Fatalf("BeginScenario: %v", err)
	}

	ctrl := p.recordCtrl()
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	spec, ok := ctrl.pending["preview"]
	if !ok {
		t.Fatalf("expected pending record for scenario, got %v", ctrl.pending)
	}

	wantOutput := filepath.Join(workdir, "preview.mp4")
	if spec.options.Output != wantOutput {
		t.Fatalf("output = %q want %q", spec.options.Output, wantOutput)
	}

	if spec.options.Codec != "h264" || !spec.options.Force {
		t.Fatalf("unexpected resolved options: %+v", spec.options)
	}
}

func TestMobileBeginScenarioRejectsPathEscape(t *testing.T) {
	t.Parallel()

	p := New()

	scenario := &model.Scenario{
		Name: "preview",
		Record: &model.ScenarioRecord{
			Output: recordExpr(t, `"../escape.mp4"`),
		},
	}

	err := p.BeginScenario(context.Background(), scenario, provider.ScenarioContext{Workdir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for output escaping the workdir")
	}

	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMobileBeginScenarioRequiresOutput(t *testing.T) {
	t.Parallel()

	p := New()

	scenario := &model.Scenario{
		Name: "preview",
		Record: &model.ScenarioRecord{
			Codec: recordExpr(t, `"h264"`),
		},
	}

	err := p.BeginScenario(context.Background(), scenario, provider.ScenarioContext{Workdir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when output is missing")
	}
}

func TestMobileEndScenarioNoActiveIsNoop(t *testing.T) {
	t.Parallel()

	p := New()

	artifacts, err := p.EndScenario(context.Background(), &model.Scenario{Name: "no-record"}, provider.ScenarioContext{}, nil)
	if err != nil {
		t.Fatalf("EndScenario: %v", err)
	}

	if len(artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %+v", artifacts)
	}
}

func TestMobileMaybeStartRecordingSpawnsAndStops(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	factory := &recordFakeFactory{}
	p := New(WithRecorderFactory(factory.factory()))

	scenario := &model.Scenario{
		Name: "preview",
		Record: &model.ScenarioRecord{
			Output: recordExpr(t, `"preview.mp4"`),
		},
	}

	if err := p.BeginScenario(context.Background(), scenario, provider.ScenarioContext{Workdir: workdir, ProjectDir: workdir}); err != nil {
		t.Fatalf("BeginScenario: %v", err)
	}

	session := &Session{
		Target:   Target{Name: "iphone"},
		DeviceID: "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "preview", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(factory.starts) != 1 {
		t.Fatalf("expected one recorder start, got %d", len(factory.starts))
	}

	// The recorder is built for the session's device and receives the
	// resolved output path; how a platform turns that into a command is
	// the backend's business, asserted in its own package.
	if got := factory.starts[0].deviceID; got != "UDID-1" {
		t.Fatalf("recorder built for device %q, want UDID-1", got)
	}

	if got := factory.starts[0].opts.Output; !strings.HasSuffix(got, "preview.mp4") {
		t.Fatalf("unexpected recorder output path: %q", got)
	}

	artifacts, err := p.EndScenario(context.Background(), scenario, provider.ScenarioContext{}, nil)
	if err != nil {
		t.Fatalf("EndScenario: %v", err)
	}

	if len(artifacts) != 1 || artifacts[0].Type != artifactTypeRecording {
		t.Fatalf("expected one recording artifact, got %+v", artifacts)
	}

	if !strings.HasSuffix(artifacts[0].Path, "preview.mp4") {
		t.Fatalf("unexpected artifact path: %q", artifacts[0].Path)
	}

	if factory.recorder.stopCount.Load() != 1 {
		t.Fatalf("expected one Stop call, got %d", factory.recorder.stopCount.Load())
	}
}

func TestMobileMaybeStartRecordingNoPendingIsNoop(t *testing.T) {
	t.Parallel()

	factory := &recordFakeFactory{}
	p := New(WithRecorderFactory(factory.factory()))

	session := &Session{
		Target:   Target{Name: "iphone"},
		DeviceID: "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "no-record", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(factory.starts) != 0 {
		t.Fatalf("expected no recorder starts, got %d", len(factory.starts))
	}
}

func TestMobileMaybeStartRecordingTargetMismatchIsNoop(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	factory := &recordFakeFactory{}
	p := New(WithRecorderFactory(factory.factory()))

	scenario := &model.Scenario{
		Name: "preview",
		Record: &model.ScenarioRecord{
			Output: recordExpr(t, `"preview.mp4"`),
			Target: recordExpr(t, `"ipad"`),
		},
	}

	if err := p.BeginScenario(context.Background(), scenario, provider.ScenarioContext{Workdir: workdir, ProjectDir: workdir}); err != nil {
		t.Fatalf("BeginScenario: %v", err)
	}

	session := &Session{
		Target:   Target{Name: "iphone"},
		DeviceID: "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "preview", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(factory.starts) != 0 {
		t.Fatalf("expected no recorder start when target name does not match, got %d", len(factory.starts))
	}
}

func TestMobileMaybeStartRecordingConflictingUDID(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	factory := &recordFakeFactory{}
	p := New(WithRecorderFactory(factory.factory()))

	makeScenario := func(name, output string) *model.Scenario {
		return &model.Scenario{
			Name: name,
			Record: &model.ScenarioRecord{
				Output: recordExpr(t, `"`+output+`"`),
			},
		}
	}

	scenarioA := makeScenario("scenarioA", "a.mp4")
	scenarioB := makeScenario("scenarioB", "b.mp4")

	if err := p.BeginScenario(context.Background(), scenarioA, provider.ScenarioContext{Workdir: workdir, ProjectDir: workdir}); err != nil {
		t.Fatalf("BeginScenario A: %v", err)
	}

	if err := p.BeginScenario(context.Background(), scenarioB, provider.ScenarioContext{Workdir: workdir, ProjectDir: workdir}); err != nil {
		t.Fatalf("BeginScenario B: %v", err)
	}

	session := &Session{
		Target:   Target{Name: "iphone"},
		DeviceID: "UDID-SHARED",
	}

	if err := p.maybeStartRecording(context.Background(), "scenarioA", session); err != nil {
		t.Fatalf("maybeStartRecording A: %v", err)
	}

	err := p.maybeStartRecording(context.Background(), "scenarioB", session)
	if err == nil {
		t.Fatal("expected conflict error when two scenarios target the same UDID")
	}

	if !strings.Contains(err.Error(), "already recording") {
		t.Fatalf("unexpected error: %v", err)
	}
}
