package mobile

import (
	"context"
	"maps"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/mobile/apple"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/simrecord"
)

func recordExpr(t *testing.T, src string) model.Expression {
	t.Helper()

	e, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse %q: %s", src, diags.Error())
	}

	return model.Expression{Expr: e, File: "test.hcl", Line: 1}
}

type recordFakeSpawn struct {
	args []string
	env  map[string]string
}

type recordFakeSpawner struct {
	calls   []recordFakeSpawn
	process *recordFakeProcess
}

func (f *recordFakeSpawner) Spawn(_ context.Context, _ string, args []string, env map[string]string) (simrecord.Process, error) {
	envCopy := make(map[string]string, len(env))
	maps.Copy(envCopy, env)

	f.calls = append(f.calls, recordFakeSpawn{args: append([]string(nil), args...), env: envCopy})

	if f.process == nil {
		f.process = &recordFakeProcess{}
	}

	return f.process, nil
}

type recordFakeProcess struct {
	stopCount atomic.Int32
}

func (p *recordFakeProcess) Stop(_ context.Context) error {
	p.stopCount.Add(1)

	return nil
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
	spawner := &recordFakeSpawner{}
	p := New(WithRecorderSpawner(spawner))

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
		Target: apple.Target{Name: "iphone"},
		UDID:   "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "preview", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(spawner.calls) != 1 {
		t.Fatalf("expected one spawner call, got %d", len(spawner.calls))
	}

	if spawner.calls[0].args[0] != "simctl" || spawner.calls[0].args[2] != "UDID-1" {
		t.Fatalf("unexpected args: %v", spawner.calls[0].args)
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

	if spawner.process.stopCount.Load() != 1 {
		t.Fatalf("expected one Stop call, got %d", spawner.process.stopCount.Load())
	}
}

func TestMobileMaybeStartRecordingNoPendingIsNoop(t *testing.T) {
	t.Parallel()

	spawner := &recordFakeSpawner{}
	p := New(WithRecorderSpawner(spawner))

	session := &Session{
		Target: apple.Target{Name: "iphone"},
		UDID:   "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "no-record", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(spawner.calls) != 0 {
		t.Fatalf("expected no spawner calls, got %d", len(spawner.calls))
	}
}

func TestMobileMaybeStartRecordingTargetMismatchIsNoop(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	spawner := &recordFakeSpawner{}
	p := New(WithRecorderSpawner(spawner))

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
		Target: apple.Target{Name: "iphone"},
		UDID:   "UDID-1",
	}

	if err := p.maybeStartRecording(context.Background(), "preview", session); err != nil {
		t.Fatalf("maybeStartRecording: %v", err)
	}

	if len(spawner.calls) != 0 {
		t.Fatalf("expected no spawner calls when target name does not match, got %d", len(spawner.calls))
	}
}

func TestMobileMaybeStartRecordingConflictingUDID(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	spawner := &recordFakeSpawner{}
	p := New(WithRecorderSpawner(spawner))

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
		Target: apple.Target{Name: "iphone"},
		UDID:   "UDID-SHARED",
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
