package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	fileprovider "github.com/tales-testing/tales/internal/provider/file"
	"github.com/tales-testing/tales/internal/report"
)

func runFileScenario(t *testing.T, base string, step *model.Step) *report.SuiteResult {
	t.Helper()

	runner := NewRunner(provider.NewRegistry(fileprovider.New()))

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "inspect",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1, ArtifactsBase: base, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	return result
}

// seedWorkspaceFile writes a file into the scenario workspace that
// buildScenarioWorkspace will compute for ("inspect", "test.tales"), so the
// file step can read it via a relative path.
func seedWorkspaceFile(t *testing.T, base, rel string, data []byte) {
	t.Helper()

	workdir, _, _, err := buildScenarioWorkspace(Options{ArtifactsBase: base, ProjectDir: t.TempDir()}, &model.Scenario{Name: "inspect", File: "test.tales"})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}

	target := filepath.Join(workdir, rel)

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
}

func TestFileStepAssertsExistingFile(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	seedWorkspaceFile(t, base, "downloads/v.json", []byte(`{"valid":true,"merkle":{"root":"r0"}}`))

	step := &model.Step{
		Provider: "file",
		Name:     "check",
		File:     "test.tales",
		FileOp: &model.FileCall{
			Path: expr(`"downloads/v.json"`),
			Expect: &model.FileExpect{
				Exists: expr(`true`),
				JSON:   expr(`{ valid = true }`),
				Hashes: map[string]model.Expression{},
			},
		},
		Capture: map[string]model.Expression{
			"root": expr(`file.json.merkle.root`),
		},
	}

	result := runFileScenario(t, base, step)

	stepResult := result.Scenarios[0].Steps[0]
	if stepResult.Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", stepResult.Failure)
	}
}

func TestFileStepExistsFalsePasses(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	step := &model.Step{
		Provider: "file",
		Name:     "check",
		File:     "test.tales",
		FileOp: &model.FileCall{
			Path:   expr(`"downloads/missing.bin"`),
			Expect: &model.FileExpect{Exists: expr(`false`), Hashes: map[string]model.Expression{}},
		},
		Capture: map[string]model.Expression{},
	}

	result := runFileScenario(t, base, step)

	if result.Scenarios[0].Steps[0].Status != report.StatusPass {
		t.Fatalf("expect exists=false on a missing file should pass: %#v", result.Scenarios[0].Steps[0].Failure)
	}
}

func TestFileStepRejectsTraversal(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	step := &model.Step{
		Provider: "file",
		Name:     "check",
		File:     "test.tales",
		FileOp: &model.FileCall{
			Path:   expr(`"../../etc/passwd"`),
			Expect: &model.FileExpect{Exists: expr(`true`), Hashes: map[string]model.Expression{}},
		},
		Capture: map[string]model.Expression{},
	}

	result := runFileScenario(t, base, step)

	failure := result.Scenarios[0].Steps[0].Failure
	if failure == nil {
		t.Fatal("expected traversal path to fail the step")
	}

	if failure.Path != "path" {
		t.Fatalf("expected failure on path, got %q", failure.Path)
	}
}

func TestFileStepFailsOnHashMismatch(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	seedWorkspaceFile(t, base, "a.bin", []byte("content"))

	step := &model.Step{
		Provider: "file",
		Name:     "check",
		File:     "test.tales",
		FileOp: &model.FileCall{
			Path: expr(`"a.bin"`),
			Expect: &model.FileExpect{
				Hashes: map[string]model.Expression{"sha256": expr(`"deadbeef"`)},
			},
		},
		Capture: map[string]model.Expression{},
	}

	result := runFileScenario(t, base, step)

	if result.Scenarios[0].Steps[0].Status != report.StatusFail {
		t.Fatal("expected a wrong sha256 to fail the step")
	}
}
