package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// loadProviderType is the provider label that triggers load step execution.
const loadProviderType = "load"

// executeLoadStep prepares the request / run inputs for a load step and
// delegates to the load provider. Expect / capture / vars semantics
// reuse the standard step pipeline so the only divergence from a
// classic HTTP step is the run block and its derived LoadExecution.
//
//nolint:gocyclo // One stage per pipeline phase is flatter than threading a builder through the runner.
func (r *Runner) executeLoadStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass}
	start := time.Now()

	if step.Load == nil || step.Load.Request == nil || step.Load.Run == nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: "load step is missing http or run block"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindVars, Path: failedVar, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	requestValues, perCallTimeout, err := evaluateLoadRequest(evaluator, scope, scenarioName, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	execution, err := evaluateLoadExecution(evaluator, scope, scenarioName, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindProvider, Message: fmt.Sprintf("unknown provider %q", step.Provider)}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	output, err := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Phase:    phase,
		Attempt:  attempt,
		Config:   config,
		Request:  requestValues,
		Load:     execution,
		Timeout:  perCallTimeout,
	})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindProvider, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)

	scope.Request = output.Request
	scope.Response = output.Response

	if step.Expect != nil {
		if expectErr := evaluateExpect(evaluator, scope, scenarioName, step, output); expectErr != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = toErrorDetail(expectErr)
			stepReport.Duration = time.Since(start)

			return stepReport
		}
	}

	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = &report.ErrorDetail{Kind: kindCapture, Path: key, Message: err.Error()}
			stepReport.Duration = time.Since(start)

			return stepReport
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	stepReport.Duration = time.Since(start)

	return stepReport
}

// evaluateLoadRequest mirrors evaluateRequest but reads from step.Load.Request.
// It returns the cty map the provider consumes plus the per-call timeout
// (if request.timeout is set).
func evaluateLoadRequest(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (map[string]cty.Value, time.Duration, error) {
	saved := step.Request
	step.Request = step.Load.Request

	defer func() { step.Request = saved }()

	return evaluateRequest(evaluator, scope, scenarioName, step)
}

// evaluateLoadExecution lowers the run block into a provider.LoadExecution.
// Concurrency defaults to 1; rate / warmup remain zero when omitted.
func evaluateLoadExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.LoadExecution, error) {
	run := step.Load.Run

	concurrency, err := evalOptionalInt(evaluator, scope, scenarioName, step.Name, "run.concurrency", run.Concurrency, 1)
	if err != nil {
		return nil, err
	}

	if concurrency <= 0 {
		return nil, fmt.Errorf("run.concurrency must be > 0")
	}

	rate, err := evalOptionalFloat(evaluator, scope, scenarioName, step.Name, "run.rate", run.Rate, 0)
	if err != nil {
		return nil, err
	}

	if rate < 0 {
		return nil, fmt.Errorf("run.rate must be >= 0")
	}

	warmup, err := evalOptionalDuration(evaluator, scope, scenarioName, step, "run.warmup", run.Warmup)
	if err != nil {
		return nil, err
	}

	hasDuration := !run.Duration.Empty()
	hasRequests := !run.Requests.Empty()

	exec := &provider.LoadExecution{
		Concurrency: concurrency,
		Rate:        rate,
		Warmup:      warmup,
	}

	switch {
	case hasDuration && hasRequests:
		return nil, fmt.Errorf("run block must define exactly one of duration or requests")
	case hasDuration:
		d, err := evalDurationAttr(evaluator, scope, scenarioName, step.Name, "run.duration", run.Duration)
		if err != nil {
			return nil, err
		}

		if d <= 0 {
			return nil, fmt.Errorf("run.duration must be > 0")
		}

		exec.Mode = "duration"
		exec.Duration = d
	case hasRequests:
		count, err := evalOptionalInt(evaluator, scope, scenarioName, step.Name, "run.requests", run.Requests, 0)
		if err != nil {
			return nil, err
		}

		if count <= 0 {
			return nil, fmt.Errorf("run.requests must be > 0")
		}

		exec.Mode = "requests"
		exec.Requests = count
	default:
		return nil, fmt.Errorf("run block must define duration or requests")
	}

	return exec, nil
}

func evalOptionalInt(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, path string, expression model.Expression, fallback int) (int, error) {
	if expression.Empty() {
		return fallback, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: path})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}

	if value.IsNull() {
		return fallback, nil
	}

	if value.Type() != cty.Number {
		return 0, fmt.Errorf("%s must be a number, got %s", path, value.Type().FriendlyName())
	}

	f, _ := value.AsBigFloat().Float64()

	return int(f), nil
}

func evalOptionalFloat(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, path string, expression model.Expression, fallback float64) (float64, error) {
	if expression.Empty() {
		return fallback, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: path})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}

	if value.IsNull() {
		return fallback, nil
	}

	if value.Type() != cty.Number {
		return 0, fmt.Errorf("%s must be a number, got %s", path, value.Type().FriendlyName())
	}

	f, _ := value.AsBigFloat().Float64()

	return f, nil
}

func evalOptionalDuration(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression) (time.Duration, error) {
	if expression.Empty() {
		return 0, nil
	}

	return evalDurationAttr(evaluator, scope, scenarioName, step.Name, path, expression)
}
