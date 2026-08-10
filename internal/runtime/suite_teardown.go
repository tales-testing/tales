package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// runSuiteTeardown executes the suite-level teardown block once. It runs on
// the Run goroutine after wg.Wait (no scenario goroutine is alive, so nothing
// races the result) and before closeProviders, so long-lived sessions — SQL
// pools, gRPC connections, browser instances — are still usable by cleanup
// steps.
//
// It runs whenever the suite declares a teardown, including when --tag or
// --scenario selected no scenario at all: a safety net that only fires when
// the run happened to match something is not a safety net. Use `when` to gate
// individual steps.
//
// Steps run sequentially in source order and a failing step does not stop the
// ones after it, mirroring the scenario-level block: cleanup is a list of
// independent obligations, not a pipeline.
func (r *Runner) runSuiteTeardown(ctx context.Context, suite *model.Suite, config map[string]cty.Value, opts Options) ([]*report.StepResult, []*report.ErrorDetail) {
	if len(suite.Teardown) == 0 {
		return nil, nil
	}

	stepNames := make([]string, 0, len(suite.Teardown))
	for _, step := range suite.Teardown {
		stepNames = append(stepNames, step.Name)
	}

	workdir, artifactsDir, scopeVars, err := buildSuiteTeardownWorkspace(opts, suite.TeardownFile)
	if err != nil {
		return suiteTeardownSetupFailure(suite.TeardownFile, err)
	}

	state := NewScenarioState(stepNames, opts.Seed, workdir, artifactsDir)
	evaluator := newSeededEvaluator(suite, config, suiteTeardownScenario, opts.Seed)

	evaluator.SetScopeVars(scopeVars)

	cleanupCtx, release := cleanupContext(ctx, opts.TeardownGrace)
	defer release()

	results := make([]*report.StepResult, 0, len(suite.Teardown))

	var failures []*report.ErrorDetail

	for _, step := range suite.Teardown {
		stepResult := r.executeTeardownStepInPhase(cleanupCtx, evaluator, suite, suiteTeardownScenario, config, state, nil, step, phaseSuiteTeardown)

		results = append(results, stepResult)

		if stepResult.Status == report.StatusFail {
			failures = append(failures, stepResult.Failure)
		}
	}

	return results, failures
}

// suiteTeardownSetupFailure reports a workspace-creation failure as a failed
// synthetic step rather than as a fatal runner error: the scenario results are
// already complete and usable, so the run should exit 1 (a failed suite), not
// 3 (a broken runner).
func suiteTeardownSetupFailure(file string, err error) ([]*report.StepResult, []*report.ErrorDetail) {
	failure := &report.ErrorDetail{
		Kind:    kindRuntime,
		Message: fmt.Sprintf("suite teardown setup failed: %s", err),
	}

	return []*report.StepResult{{
		File:     file,
		Scenario: suiteTeardownScenario,
		Name:     "workspace",
		Phase:    phaseSuiteTeardown,
		Status:   report.StatusFail,
		Failure:  failure,
	}}, []*report.ErrorDetail{failure}
}

// buildSuiteTeardownWorkspace creates the workspace used by suite-level
// teardown steps and returns its absolute roots plus the namespaces to expose
// on the evaluator. The layout mirrors buildScenarioWorkspace but lives under
// <base>/suite-teardown, so it can never collide with a scenario directory.
//
// The `scenario` namespace is kept (naming the pseudo-scenario) so save / file
// / exec and every documented ${scenario.workdir} expression keep working
// unchanged inside the block; `suite` is the honest alias to prefer.
func buildSuiteTeardownWorkspace(opts Options, teardownFile string) (string, string, map[string]cty.Value, error) {
	rel := filepath.Join(opts.ArtifactsBase, "suite-teardown", artifacts.Hash(teardownFile, suiteTeardownScenario))

	workdir, err := filepath.Abs(rel)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve suite teardown workspace path: %w", err)
	}

	artifactsDir := filepath.Join(workdir, "artifacts")

	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return "", "", nil, fmt.Errorf("create suite teardown workspace: %w", err)
	}

	dirs := cty.ObjectVal(map[string]cty.Value{
		"workdir":       cty.StringVal(workdir),
		"artifacts_dir": cty.StringVal(artifactsDir),
	})

	scopeVars := map[string]cty.Value{
		"scenario": cty.ObjectVal(map[string]cty.Value{
			"name":          cty.StringVal(suiteTeardownScenario),
			"workdir":       cty.StringVal(workdir),
			"artifacts_dir": cty.StringVal(artifactsDir),
		}),
		"suite": dirs,
		"project": cty.ObjectVal(map[string]cty.Value{
			"dir": cty.StringVal(opts.ProjectDir),
		}),
	}

	return workdir, artifactsDir, scopeVars, nil
}
