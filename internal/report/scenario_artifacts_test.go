package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func sampleScenarioWithArtifacts() *SuiteResult {
	return &SuiteResult{
		Seed: 7,
		Scenarios: []*ScenarioResult{{
			File:   "e2e/preview.tales",
			Name:   "app_store_preview",
			Status: StatusPass,
			Steps:  []*StepResult{},
			Artifacts: []Artifact{
				{Type: "recording", Path: "/abs/build/artifacts/preview.mp4"},
			},
		}},
	}
}

func TestConsolePrintsScenarioArtifacts(t *testing.T) {
	t.Parallel()

	result := sampleScenarioWithArtifacts()

	buf := &bytes.Buffer{}
	if err := PrintConsoleWithOptions(buf, result, ConsoleOptions{Color: false}); err != nil {
		t.Fatalf("PrintConsole: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "recording: /abs/build/artifacts/preview.mp4") {
		t.Fatalf("expected recording line in console output, got:\n%s", out)
	}
}

func TestJSONLAttachesScenarioArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/events.jsonl"

	result := sampleScenarioWithArtifacts()
	if err := WriteJSONL(path, result); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}

	for _, raw := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if event["type"] != "scenario" {
			continue
		}

		artifacts, ok := event["artifacts"].([]any)
		if !ok {
			t.Fatalf("expected artifacts array on scenario event, got %T", event["artifacts"])
		}

		if len(artifacts) != 1 {
			t.Fatalf("expected one artifact, got %d", len(artifacts))
		}

		first, ok := artifacts[0].(map[string]any)
		if !ok || first["type"] != "recording" || first["path"] != "/abs/build/artifacts/preview.mp4" {
			t.Fatalf("unexpected artifact payload: %+v", artifacts[0])
		}

		return
	}

	t.Fatal("scenario event not found in jsonl output")
}

func TestJUnitSurfacesScenarioArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/report.xml"

	if err := WriteJUnit(path, sampleScenarioWithArtifacts()); err != nil {
		t.Fatalf("WriteJUnit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, "<system-out>") {
		t.Fatalf("expected <system-out> element in junit output, got:\n%s", got)
	}

	if !strings.Contains(got, "preview.mp4") {
		t.Fatalf("expected preview.mp4 in junit system-out, got:\n%s", got)
	}
}
