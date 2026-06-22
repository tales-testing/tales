package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// rpcProviderType is the provider label that triggers rpc step execution.
const rpcProviderType = "rpc"

// defaultRPCTimeout matches the user spec ("default inherited from runtime
// or 30s") and the timeout the gRPC / Connect transports fall back to when
// no per-call deadline is set.
const defaultRPCTimeout = 30 * time.Second

// executeRPCStep evaluates an rpc step, dispatches to the rpc provider, and
// runs assertions + capture. The provider owns descriptor + transport caches
// across scenarios so concurrent steps targeting the same service share one
// gRPC connection / one Connect HTTP transport. The runtime only carries
// primitive Go values plus the request message as cty so the
// provider.Input boundary stays free of protobuf types.
func (r *Runner) executeRPCStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	start := time.Now()
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass, StartedAt: start}

	if step.RPC == nil {
		return failStep(stepReport, start, kindEval, "", "rpc step is missing its call block")
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		return failStep(stepReport, start, kindVars, failedVar, err.Error())
	}

	exec, evalErr := evaluateRPCExecution(evaluator, scope, scenarioName, state, step)
	if evalErr != nil {
		return failStep(stepReport, start, kindEval, "", evalErr.Error())
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		return failStep(stepReport, start, kindProvider, "", fmt.Sprintf("unknown provider %q", step.Provider))
	}

	output, runErr := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Phase:    phase,
		Attempt:  attempt,
		Config:   config,
		RPC:      exec,
		Timeout:  exec.Timeout,
	})
	if runErr != nil {
		return failStep(stepReport, start, kindProvider, "", runErr.Error())
	}

	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)

	scope.Request = output.Request
	scope.Response = output.Response

	if assertErr := assertRPC(evaluator, scope, scenarioName, step, output); assertErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = toErrorDetail(assertErr)
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	if captureErr := applyRPCCapture(evaluator, scope, scenarioName, state, step, output); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// evaluateRPCExecution lowers the rpc step's expressions into the primitive
// payload the provider consumes. The request message stays a cty map so the
// codec can drive protojson with the right resolver inside the provider.
func evaluateRPCExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step) (*provider.RPCExecution, error) {
	rc := step.RPC

	target, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "target", rc.Target)
	if err != nil {
		return nil, err
	}

	service, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "call.service", rc.Service)
	if err != nil {
		return nil, err
	}

	method, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "call.method", rc.Method)
	if err != nil {
		return nil, err
	}

	message, err := evalRPCMessage(evaluator, scope, scenarioName, step.Name, rc.Message)
	if err != nil {
		return nil, err
	}

	headers, err := evalRPCStringMap(evaluator, scope, scenarioName, step.Name, "call.headers", rc.Headers)
	if err != nil {
		return nil, err
	}

	metadata, err := evalRPCStringMap(evaluator, scope, scenarioName, step.Name, "call.metadata", rc.Metadata)
	if err != nil {
		return nil, err
	}

	timeout, err := evalRPCTimeout(evaluator, scope, scenarioName, step.Name, rc.Timeout)
	if err != nil {
		return nil, err
	}

	exec := &provider.RPCExecution{
		Target:           target,
		Service:          service,
		Method:           method,
		Message:          message,
		HeadersOverride:  headers,
		MetadataOverride: metadata,
		Timeout:          timeout,
		ArtifactsDir:     filepath.Join(state.Workdir(), "rpc", rpcArtifactsSegment(evaluator, step.Name)),
	}

	return exec, nil
}

// rpcArtifactsSegment mirrors execArtifactsSegment: derive a unique subdir
// per step name, mixed with the keyword call-site seed scope so the same
// step invoked from different keywords does not collide on disk.
func rpcArtifactsSegment(evaluator *lang.Evaluator, stepName string) string {
	segment := artifacts.SafePathSegment(stepName)

	if scope := evaluator.SeedScope(); scope != "" {
		segment = segment + "-" + artifacts.Hash(scope)
	}

	return segment
}

func evalRPCMessage(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName string, expression model.Expression) (map[string]cty.Value, error) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: "call.message"})
	if err != nil {
		return nil, fmt.Errorf("call.message: %w", err)
	}

	if value.IsNull() {
		return nil, nil
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("call.message must be an object")
	}

	return value.AsValueMap(), nil
}

func evalRPCStringMap(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, path string, expression model.Expression) (map[string]string, error) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: path})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if value.IsNull() {
		return nil, nil
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("%s must be an object", path)
	}

	out := map[string]string{}

	for k, v := range value.AsValueMap() {
		if v.IsNull() {
			continue
		}

		if v.Type() != cty.String {
			return nil, fmt.Errorf("%s.%s must be a string", path, k)
		}

		out[k] = v.AsString()
	}

	return out, nil
}

func evalRPCTimeout(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName string, expression model.Expression) (time.Duration, error) {
	if expression.Empty() {
		return defaultRPCTimeout, nil
	}

	timeout, err := evalDurationAttr(evaluator, scope, scenarioName, stepName, "call.timeout", expression)
	if err != nil {
		return 0, err
	}

	if timeout <= 0 {
		return defaultRPCTimeout, nil
	}

	return timeout, nil
}

// assertRPC runs the rpc-specific assertions against the provider response.
// status / message / error are validated; headers / metadata / trailers run
// through the same MatchJSON path as the HTTP provider's headers, so user
// matchers (contains, matches, ...) keep working.
func assertRPC(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	expect := step.RPC.Expect
	if expect == nil {
		return nil
	}

	meta := func(path string) lang.GenerateMeta {
		return lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path}
	}

	for _, check := range []struct {
		path     string
		expr     model.Expression
		actual   cty.Value
		strict   bool
		readable string
	}{
		{exprPathExpectStatus, expect.Status, output.Response[rpcKeyStatus], true, "response.status"},
		{"expect.message", expect.Message, output.Response[rpcKeyMessage], false, "response.message"},
		{"expect.error", expect.Error, output.Response[rpcKeyError], false, "response.error"},
		{exprPathExpectHeaders, expect.Headers, output.Response[rpcKeyHeaders], false, "response.headers"},
		{"expect.metadata", expect.Metadata, output.Response[rpcKeyMetadata], false, "response.metadata"},
		{"expect.trailers", expect.Trailers, output.Response[rpcKeyTrailers], false, "response.trailers"},
	} {
		if check.expr.Empty() {
			continue
		}

		expected, err := evaluator.Eval(check.expr, scope, meta(check.path))
		if err != nil {
			return fmt.Errorf("%s: %w", check.path, err)
		}

		if err := assertion.MatchJSON(expected, check.actual, check.strict, check.readable); err != nil {
			return fmt.Errorf("assert %s: %w", check.readable, err)
		}
	}

	return nil
}

// applyRPCCapture evaluates the capture expressions in the post-call scope.
// response.* is already the standard scope key (Set by the runner above),
// so capture blocks can read response.message.id directly without an
// rpc namespace.
func applyRPCCapture(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output) *captureError {
	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			return &captureError{path: key, message: err.Error()}
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	return nil
}

// Response key names matching the provider entry point's exposed shape.
// Kept in sync with internal/provider/rpc/provider.go's constants of the
// same content.
const (
	rpcKeyStatus   = "status"
	rpcKeyMessage  = "message"
	rpcKeyError    = "error"
	rpcKeyHeaders  = "headers"
	rpcKeyMetadata = "metadata"
	rpcKeyTrailers = "trailers"
)

// Expect attribute paths reused across the rpc assertion table and the
// other runtime files (the HTTP status assertion in runner.go, the
// webhook headers check). Centralizing them placates the goconst linter
// when the same path appears in two files.
const (
	exprPathExpectStatus  = "expect.status"
	exprPathExpectHeaders = "expect.headers"
)
