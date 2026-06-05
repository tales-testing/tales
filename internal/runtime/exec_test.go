package runtime

import (
	"context"
	"os/exec"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	execprovider "github.com/tales-testing/tales/internal/provider/exec"
	"github.com/tales-testing/tales/internal/report"
)

func requireSh(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available on PATH")
	}
}

func runExecScenario(t *testing.T, allowExec bool, step *model.Step) *report.SuiteResult {
	t.Helper()

	runner := NewRunner(provider.NewRegistry(execprovider.New(execprovider.WithAllowExec(allowExec))))

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "verify",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1, ArtifactsBase: t.TempDir(), ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	return result
}

func TestExecDisabledFailsStepWithExactMessage(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Provider: "exec",
		Name:     "run",
		File:     "test.tales",
		Exec:     &model.ExecCall{Command: expr(`"sh"`), Args: expr(`["-c", "echo hi"]`)},
		Capture:  map[string]model.Expression{},
	}

	result := runExecScenario(t, false, step)

	failure := result.Scenarios[0].Steps[0].Failure
	if failure == nil {
		t.Fatal("expected exec step to fail when --allow-exec is absent")
	}

	if failure.Message != "exec provider is disabled by default. Re-run with --allow-exec." {
		t.Fatalf("unexpected disabled message: %q", failure.Message)
	}
}

func TestExecRunsWithNamespaces(t *testing.T) {
	t.Parallel()
	requireSh(t)

	step := &model.Step{
		Provider: "exec",
		Name:     "run",
		File:     "test.tales",
		Exec: &model.ExecCall{
			Command: expr(`"sh"`),
			Args:    expr(`["-c", "printf '{\"valid\":true}'"]`),
			Expect: &model.ExecExpect{
				ExitCode:   expr(`0`),
				StdoutJSON: expr(`{ valid = true }`),
			},
		},
		Capture: map[string]model.Expression{
			"code":  expr(`exec.exit_code`),
			"valid": expr(`stdout.json.valid`),
		},
	}

	result := runExecScenario(t, true, step)

	stepResult := result.Scenarios[0].Steps[0]
	if stepResult.Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", stepResult.Failure)
	}

	// Artifacts are attached to the report.
	if len(stepResult.Artifacts) == 0 {
		t.Fatal("expected exec artifacts on the step report")
	}
}

func TestExecStdoutJSONExpectedButNotJSONFails(t *testing.T) {
	t.Parallel()
	requireSh(t)

	step := &model.Step{
		Provider: "exec",
		Name:     "run",
		File:     "test.tales",
		Exec: &model.ExecCall{
			Command: expr(`"sh"`),
			Args:    expr(`["-c", "echo not-json"]`),
			Expect:  &model.ExecExpect{StdoutJSON: expr(`{ valid = true }`)},
		},
		Capture: map[string]model.Expression{},
	}

	result := runExecScenario(t, true, step)

	if result.Scenarios[0].Steps[0].Status != report.StatusFail {
		t.Fatal("expected stdout_json on non-JSON stdout to fail the step")
	}
}
