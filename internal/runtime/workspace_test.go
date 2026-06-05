package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
)

func TestBuildScenarioWorkspaceCreatesDirsAndVars(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	project := t.TempDir()

	scenario := &model.Scenario{Name: "Download & verify", File: "suite.tales"}

	workdir, artifactsDir, scopeVars, err := buildScenarioWorkspace(
		Options{ArtifactsBase: base, ProjectDir: project}, scenario,
	)
	if err != nil {
		t.Fatalf("buildScenarioWorkspace error: %v", err)
	}

	if !filepath.IsAbs(workdir) {
		t.Fatalf("workdir %q is not absolute", workdir)
	}

	if info, statErr := os.Stat(artifactsDir); statErr != nil || !info.IsDir() {
		t.Fatalf("artifacts dir %q was not created: %v", artifactsDir, statErr)
	}

	// The sanitized scenario name is used as a directory segment (a run of
	// unsafe characters collapses to a single underscore).
	if !strings.Contains(workdir, "Download_verify-") {
		t.Fatalf("workdir %q does not embed the sanitized scenario name", workdir)
	}

	scenarioObj := scopeVars["scenario"]
	if got := scenarioObj.GetAttr("workdir").AsString(); got != workdir {
		t.Fatalf("scenario.workdir = %q, want %q", got, workdir)
	}

	if got := scenarioObj.GetAttr("artifacts_dir").AsString(); got != artifactsDir {
		t.Fatalf("scenario.artifacts_dir = %q, want %q", got, artifactsDir)
	}

	if got := scopeVars["project"].GetAttr("dir").AsString(); got != project {
		t.Fatalf("project.dir = %q, want %q", got, project)
	}
}

// TestBuildScenarioWorkspaceCollisionResistance verifies two scenarios with
// the same name in different files get distinct workspace directories.
func TestBuildScenarioWorkspaceCollisionResistance(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	opts := Options{ArtifactsBase: base, ProjectDir: t.TempDir()}

	a, _, _, err := buildScenarioWorkspace(opts, &model.Scenario{Name: "same", File: "a.tales"})
	if err != nil {
		t.Fatalf("a: %v", err)
	}

	b, _, _, err := buildScenarioWorkspace(opts, &model.Scenario{Name: "same", File: "b.tales"})
	if err != nil {
		t.Fatalf("b: %v", err)
	}

	if a == b {
		t.Fatalf("expected distinct workdirs, both = %q", a)
	}
}

// TestScenarioStateExposesWorkspace confirms the workspace roots survive on
// the ScenarioState (read by the save / file / exec executors).
func TestScenarioStateExposesWorkspace(t *testing.T) {
	t.Parallel()

	state := NewScenarioState([]string{"a"}, 7, "/work/sc", "/work/sc/artifacts")

	if state.Workdir() != "/work/sc" {
		t.Fatalf("Workdir() = %q", state.Workdir())
	}

	if state.ArtifactsDir() != "/work/sc/artifacts" {
		t.Fatalf("ArtifactsDir() = %q", state.ArtifactsDir())
	}

	// Sanity: empty step results are still pre-filled.
	if _, ok := state.GetResultMap()["a"]; !ok {
		t.Fatal("expected step a to be pre-filled in results")
	}
}
