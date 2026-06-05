package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/tales-testing/tales/internal/workspace"
	"github.com/zclconf/go-cty/cty"
)

const fileProviderType = "file"

// executeFileStep resolves the file path against the scenario's allowed roots,
// inspects the file via the file provider, runs the file-specific assertions
// (exists / size_bytes / hashes / text / json), and captures with the file.*
// namespace injected. The report stays metadata-only (path, exists, size,
// hashes) — it never echoes file text or parsed JSON.
func (r *Runner) executeFileStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	start := time.Now()
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass, StartedAt: start}

	if step.FileOp == nil {
		return failStep(stepReport, start, kindEval, "", "file step is missing its path")
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		return failStep(stepReport, start, kindVars, failedVar, err.Error())
	}

	resolvedPath, detail := r.resolveFilePath(evaluator, scope, scenarioName, state, step)
	if detail != nil {
		return failStep(stepReport, start, detail.Kind, detail.Path, detail.Message)
	}

	exec := fileExecution(step, resolvedPath)

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		return failStep(stepReport, start, kindProvider, "", fmt.Sprintf("unknown provider %q", step.Provider))
	}

	output, err := providerImpl.Execute(ctx, provider.Input{Scenario: scenarioName, Step: step, Phase: phase, Attempt: attempt, Config: config, File: &exec})
	if err != nil {
		return failStep(stepReport, start, kindProvider, "", err.Error())
	}

	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = fileResponseReport(output.Response)

	if assertErr := assertFile(evaluator, scope, scenarioName, step, output.Response); assertErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = toErrorDetail(assertErr)
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	if captureErr := applyFileCapture(evaluator, scope, scenarioName, state, step, output); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// resolveFilePath evaluates the step path and resolves it against the
// scenario workspace (or the project dir, for absolute reads).
func (r *Runner) resolveFilePath(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step) (string, *report.ErrorDetail) {
	value, err := evaluator.Eval(step.FileOp.Path, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: keyPath})
	if err != nil {
		return "", &report.ErrorDetail{Kind: kindEval, Path: keyPath, Message: err.Error()}
	}

	if value.IsNull() || value.Type() != cty.String {
		return "", &report.ErrorDetail{Kind: kindEval, Path: keyPath, Message: "file path must evaluate to a string"}
	}

	resolver := workspace.Resolver{Workdir: state.Workdir(), ProjectDir: r.projectDir}

	resolved, resolveErr := resolver.ResolveInput(value.AsString())
	if resolveErr != nil {
		return "", &report.ErrorDetail{Kind: kindProvider, Path: keyPath, Message: resolveErr.Error()}
	}

	return resolved, nil
}

// fileExecution derives which reads the provider must perform from the
// declared expectations and the capture expressions referencing file.*.
func fileExecution(step *model.Step, resolvedPath string) provider.FileExecution {
	fe := provider.FileExecution{Path: resolvedPath}

	if e := step.FileOp.Expect; e != nil {
		fe.NeedSize = !e.SizeBytes.Empty()
		fe.NeedText = !e.Text.Empty()
		fe.NeedJSON = !e.JSON.Empty()
		fe.NeedHash = len(e.Hashes) > 0
	}

	for _, capExpr := range step.Capture {
		for _, ref := range lang.FindFileRefs(capExpr.Expr) {
			applyFileNeed(&fe, ref)
		}
	}

	return fe
}

// applyFileNeed turns a file.<ref> reference into the matching read flag. path
// and exists need no read; size_bytes / text / json map directly; any other
// reference (the sha* digests) requires reading the file to hash it.
func applyFileNeed(fe *provider.FileExecution, ref string) {
	switch ref {
	case keyPath, keyExists:
	case keySize:
		fe.NeedSize = true
	case keyText:
		fe.NeedText = true
	case keyJSON:
		fe.NeedJSON = true
	default:
		fe.NeedHash = true
	}
}

// assertFile runs the declared file assertions against the provider response.
func assertFile(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, response map[string]cty.Value) error {
	expect := step.FileOp.Expect
	if expect == nil {
		return nil
	}

	checks := []struct {
		attr string
		expr model.Expression
	}{
		{keyExists, expect.Exists},
		{keySize, expect.SizeBytes},
		{keyText, expect.Text},
		{keyJSON, expect.JSON},
	}

	for _, check := range checks {
		if err := assertFileField(evaluator, scope, scenarioName, step, check.attr, check.expr, response[check.attr]); err != nil {
			return err
		}
	}

	for algo, expression := range expect.Hashes {
		if err := assertFileField(evaluator, scope, scenarioName, step, algo, expression, response[algo]); err != nil {
			return err
		}
	}

	return nil
}

// assertFileField evaluates one expected value and matches it against the
// actual file attribute. Empty expressions are skipped.
func assertFileField(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression, actual cty.Value) error {
	if expression.Empty() {
		return nil
	}

	expected, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect." + name})
	if err != nil {
		return fmt.Errorf("expect.%s: %w", name, err)
	}

	if expected.IsNull() {
		return nil
	}

	if actual == cty.NilVal {
		actual = cty.NullVal(cty.DynamicPseudoType)
	}

	if err := assertion.MatchJSON(expected, actual, false, "file."+name); err != nil {
		return fmt.Errorf("assert file.%s: %w", name, err)
	}

	return nil
}

// applyFileCapture evaluates capture expressions with the file.* namespace
// injected and records the step result.
func applyFileCapture(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output) *captureError {
	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	fileNamespace := map[string]cty.Value{fileProviderType: cty.ObjectVal(output.Response)}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.EvalWithExtras(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key}, nil, fileNamespace)
		if err != nil {
			return &captureError{path: key, message: err.Error()}
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	return nil
}

// fileResponseReport curates the metadata-only report view: path, exists,
// size and digests. File text and parsed JSON are intentionally omitted so the
// report never echoes potentially large or sensitive file content.
func fileResponseReport(response map[string]cty.Value) map[string]any {
	curated := map[string]cty.Value{}

	for _, key := range []string{keyPath, keyExists, keySize} {
		if value, ok := response[key]; ok {
			curated[key] = value
		}
	}

	for _, algo := range lang.HashAlgorithms() {
		if value, ok := response[algo]; ok && !value.IsNull() {
			curated[algo] = value
		}
	}

	return diagnostic.FromCTYMap(curated)
}
