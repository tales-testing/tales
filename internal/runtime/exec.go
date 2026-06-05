package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/report"
	"github.com/tales-testing/tales/internal/workspace"
	"github.com/zclconf/go-cty/cty"
)

const execProviderType = "exec"

const (
	defaultExecTimeout = 30 * time.Second
	sandboxWorkdirScn  = "scenario"
	sandboxWorkdirPrj  = "project"
	execNamespaceOut   = "stdout"
	execNamespaceErr   = "stderr"
	execNamespaceExec  = "exec"
	pathSandboxWorkdir = "sandbox.workdir"
	keyExitCode        = "exit_code"
	keyDurationMS      = "duration_ms"
	pathTimeout        = "timeout"
)

// executeExecStep evaluates an exec step, resolves its working directory,
// invokes the exec provider, then runs the exec assertions and capture with
// the stdout / stderr / exec namespaces injected. The report is metadata-only;
// stdout / stderr artifacts are attached even when the step fails.
func (r *Runner) executeExecStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	start := time.Now()
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass, StartedAt: start}

	if step.Exec == nil {
		return failStep(stepReport, start, kindEval, "", "exec step is missing its command")
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		return failStep(stepReport, start, kindVars, failedVar, err.Error())
	}

	exec, detail := r.evaluateExecExecution(evaluator, scope, scenarioName, state, step)
	if detail != nil {
		return failStep(stepReport, start, detail.Kind, detail.Path, detail.Message)
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		return failStep(stepReport, start, kindProvider, "", fmt.Sprintf("unknown provider %q", step.Provider))
	}

	// Clear any artifacts a previous run left in this step's directory so the
	// report reflects only the current run (e.g. a disabled exec writes none).
	_ = os.RemoveAll(exec.ArtifactsDir)

	output, runErr := providerImpl.Execute(ctx, provider.Input{Scenario: scenarioName, Step: step, Phase: phase, Attempt: attempt, Config: config, Exec: exec})

	stepReport.Artifacts = execArtifacts(exec.ArtifactsDir)

	if runErr != nil {
		return failStep(stepReport, start, kindProvider, "", runErr.Error())
	}

	stepReport.StatusCode = output.StatusCode
	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = execResponseReport(output.Response)

	namespaces := execNamespaces(output.Response)

	if assertErr := assertExec(evaluator, scope, scenarioName, step, output.Response, namespaces); assertErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = toErrorDetail(assertErr)
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	if captureErr := applyExecCapture(evaluator, scope, scenarioName, state, step, output, namespaces); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// evaluateExecExecution lowers the exec step's expressions into the concrete
// provider payload, resolving the working directory against the scenario's
// allowed roots and deriving the artifacts directory.
func (r *Runner) evaluateExecExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step) (*provider.ExecExecution, *report.ErrorDetail) {
	ec := step.Exec

	command, detail := evalExecString(evaluator, scope, scenarioName, step, "command", ec.Command, true)
	if detail != nil {
		return nil, detail
	}

	args, detail := evalExecStringList(evaluator, scope, scenarioName, step, "args", ec.Args)
	if detail != nil {
		return nil, detail
	}

	env, detail := evalExecStringMap(evaluator, scope, scenarioName, step, "env", ec.Env)
	if detail != nil {
		return nil, detail
	}

	stdin, detail := evalExecString(evaluator, scope, scenarioName, step, "stdin", ec.Stdin, false)
	if detail != nil {
		return nil, detail
	}

	timeout, detail := evalExecTimeout(evaluator, scope, scenarioName, step, ec.Timeout)
	if detail != nil {
		return nil, detail
	}

	sandbox, detail := r.evalExecSandbox(evaluator, scope, scenarioName, state, step)
	if detail != nil {
		return nil, detail
	}

	return &provider.ExecExecution{
		Command:      command,
		Args:         args,
		Env:          env,
		EnvMode:      sandbox.envMode,
		Stdin:        stdin,
		Timeout:      timeout,
		SandboxMode:  sandbox.mode,
		Network:      sandbox.network,
		Workdir:      sandbox.workdir,
		ProjectDir:   r.projectDir,
		ArtifactsDir: filepath.Join(state.Workdir(), "exec", execArtifactsSegment(evaluator, step.Name)),
	}, nil
}

// execArtifactsSegment derives the per-step exec artifacts directory segment.
// It mixes in the evaluator seed scope (the keyword call stack) so the same
// exec step invoked from different keyword call sites gets distinct
// directories — otherwise the start-of-run cleanup of one call would wipe a
// sibling call's artifacts. At scenario level the scope is empty and the
// segment stays the plain step name.
func execArtifactsSegment(evaluator *lang.Evaluator, stepName string) string {
	segment := artifacts.SafePathSegment(stepName)

	if scope := evaluator.SeedScope(); scope != "" {
		segment = segment + "-" + artifacts.Hash(scope)
	}

	return segment
}

// execSandbox holds the resolved sandbox settings.
type execSandbox struct {
	mode    string
	envMode string
	workdir string
	network bool
}

// evalExecSandbox resolves the sandbox block, applying defaults (process /
// scenario / minimal / network=false) and resolving the working directory.
func (r *Runner) evalExecSandbox(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step) (execSandbox, *report.ErrorDetail) {
	out := execSandbox{mode: "process", envMode: "minimal", workdir: state.Workdir()}

	sb := step.Exec.Sandbox
	if sb == nil {
		return out, nil
	}

	if detail := assignExecString(evaluator, scope, scenarioName, step, "sandbox.mode", sb.Mode, &out.mode); detail != nil {
		return out, detail
	}

	if detail := assignExecString(evaluator, scope, scenarioName, step, "sandbox.env", sb.Env, &out.envMode); detail != nil {
		return out, detail
	}

	network, detail := evalExecBool(evaluator, scope, scenarioName, step, "sandbox.network", sb.Network)
	if detail != nil {
		return out, detail
	}

	out.network = network

	workdir, detail := r.resolveExecWorkdir(evaluator, scope, scenarioName, state, step, sb.Workdir)
	if detail != nil {
		return out, detail
	}

	out.workdir = workdir

	return out, nil
}

// resolveExecWorkdir maps the sandbox workdir mode to an absolute directory:
// scenario (default) → scenario workdir, project → project dir, any other
// value is treated as a custom path resolved under the scenario workspace.
func (r *Runner) resolveExecWorkdir(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, expression model.Expression) (string, *report.ErrorDetail) {
	if expression.Empty() {
		return state.Workdir(), nil
	}

	raw, detail := evalExecString(evaluator, scope, scenarioName, step, pathSandboxWorkdir, expression, false)
	if detail != nil {
		return "", detail
	}

	switch raw {
	case "", sandboxWorkdirScn:
		return state.Workdir(), nil
	case sandboxWorkdirPrj:
		return r.projectDir, nil
	default:
		resolver := workspace.Resolver{Workdir: state.Workdir(), ProjectDir: r.projectDir}

		resolved, err := resolver.ResolveOutput(raw)
		if err != nil {
			return "", &report.ErrorDetail{Kind: kindProvider, Path: pathSandboxWorkdir, Message: err.Error()}
		}

		if mkErr := os.MkdirAll(resolved, 0o755); mkErr != nil {
			return "", &report.ErrorDetail{Kind: kindProvider, Path: pathSandboxWorkdir, Message: mkErr.Error()}
		}

		return resolved, nil
	}
}

// execNamespaces builds the stdout / stderr / exec namespaces injected into
// expect and capture expressions.
func execNamespaces(response map[string]cty.Value) map[string]cty.Value {
	exec := cty.ObjectVal(map[string]cty.Value{
		keyExitCode:   response[keyExitCode],
		keyDurationMS: response[keyDurationMS],
	})

	return map[string]cty.Value{
		execNamespaceOut:  response[execNamespaceOut],
		execNamespaceErr:  response[execNamespaceErr],
		execNamespaceExec: exec,
	}
}

// applyExecCapture evaluates capture expressions with the exec namespaces
// injected and records the step result.
func applyExecCapture(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output, namespaces map[string]cty.Value) *captureError {
	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.EvalWithExtras(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key}, nil, namespaces)
		if err != nil {
			return &captureError{path: key, message: err.Error()}
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	return nil
}

// execResponseReport curates the metadata-only report: exit code, duration and
// truncation flags. Raw stdout / stderr are intentionally omitted so the
// console / JSONL report never dumps potentially sensitive program output.
func execResponseReport(response map[string]cty.Value) map[string]any {
	curated := map[string]cty.Value{
		keyExitCode:   response[keyExitCode],
		keyDurationMS: response[keyDurationMS],
	}

	if stdout := response[execNamespaceOut]; stdout.Type().IsObjectType() && stdout.Type().HasAttribute("truncated") {
		curated["stdout_truncated"] = stdout.GetAttr("truncated")
	}

	if stderr := response[execNamespaceErr]; stderr.Type().IsObjectType() && stderr.Type().HasAttribute("truncated") {
		curated["stderr_truncated"] = stderr.GetAttr("truncated")
	}

	return diagnostic.FromCTYMap(curated)
}

// execArtifacts lists the artifact files the exec provider may have written,
// including only those that exist on disk (so a disabled / not-yet-run step
// reports none).
func execArtifacts(dir string) []report.Artifact {
	candidates := []struct {
		kind string
		name string
	}{
		{"stdout", "stdout.txt"},
		{"stderr", "stderr.txt"},
		{"metadata", "metadata.json"},
		{"stdout_json", "stdout.json"},
	}

	out := make([]report.Artifact, 0, len(candidates))

	for _, c := range candidates {
		path := filepath.Join(dir, c.name)
		if _, err := os.Stat(path); err == nil {
			out = append(out, report.Artifact{Type: c.kind, Path: path})
		}
	}

	return out
}

// assertExec runs the exec assertions against the provider response.
func assertExec(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, response, namespaces map[string]cty.Value) error {
	e := step.Exec.Expect
	if e == nil {
		return nil
	}

	if err := matchExecField(evaluator, scope, scenarioName, step, keyExitCode, e.ExitCode, response[keyExitCode], true, namespaces); err != nil {
		return err
	}

	if err := matchExecField(evaluator, scope, scenarioName, step, "stdout", e.Stdout, execStreamRaw(response, execNamespaceOut), false, namespaces); err != nil {
		return err
	}

	if err := matchExecField(evaluator, scope, scenarioName, step, "stderr", e.Stderr, execStreamRaw(response, execNamespaceErr), false, namespaces); err != nil {
		return err
	}

	return assertExecStdoutJSON(evaluator, scope, scenarioName, step, response, namespaces)
}

// assertExecStdoutJSON matches the stdout_json expectation against the parsed
// stdout JSON, failing clearly when stdout could not be parsed as JSON.
func assertExecStdoutJSON(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, response, namespaces map[string]cty.Value) error {
	expression := step.Exec.Expect.StdoutJSON
	if expression.Empty() {
		return nil
	}

	expected, err := evaluator.EvalWithExtras(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.stdout_json"}, nil, namespaces)
	if err != nil {
		return fmt.Errorf("expect.stdout_json: %w", err)
	}

	if expected.IsNull() {
		return nil
	}

	stdoutJSON := execStreamAttr(response, execNamespaceOut, "json")
	if stdoutJSON.IsNull() && execStreamRaw(response, execNamespaceOut).AsString() != "" {
		return fmt.Errorf("assert stdout_json: stdout is not valid JSON")
	}

	if err := assertion.MatchJSON(expected, stdoutJSON, false, "stdout_json"); err != nil {
		return fmt.Errorf("assert stdout_json: %w", err)
	}

	return nil
}

// matchExecField evaluates one expected expression (with namespaces) and
// matches it against the actual response value. Empty expressions are skipped.
func matchExecField(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression, actual cty.Value, strict bool, namespaces map[string]cty.Value) error {
	if expression.Empty() {
		return nil
	}

	expected, err := evaluator.EvalWithExtras(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect." + name}, nil, namespaces)
	if err != nil {
		return fmt.Errorf("expect.%s: %w", name, err)
	}

	if expected.IsNull() {
		return nil
	}

	if err := assertion.MatchJSON(expected, actual, strict, name); err != nil {
		return fmt.Errorf("assert %s: %w", name, err)
	}

	return nil
}

// execStreamRaw returns the raw string of the stdout / stderr stream object.
func execStreamRaw(response map[string]cty.Value, stream string) cty.Value {
	return execStreamAttr(response, stream, "raw")
}

// execStreamAttr reads an attribute from a stream object, returning a null
// string when absent.
func execStreamAttr(response map[string]cty.Value, stream, attr string) cty.Value {
	value, ok := response[stream]
	if !ok || value.IsNull() || !value.Type().IsObjectType() || !value.Type().HasAttribute(attr) {
		return cty.NullVal(cty.String)
	}

	return value.GetAttr(attr)
}
