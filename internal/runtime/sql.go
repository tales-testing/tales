package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/hyperxlab/tales/internal/lang"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	sqlprovider "github.com/hyperxlab/tales/internal/provider/sql"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// sqlProviderType is the provider label that triggers SQL step execution.
const sqlProviderType = "sql"

// executeSQLStep evaluates a sql step's connection / sql / args expressions
// and dispatches to the SQL provider with the prepared SQLExecution payload.
// Expect / capture / retry / skip semantics reuse the standard step pipeline:
// only the per-step inputs differ from the HTTP provider.
func (r *Runner) executeSQLStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass}
	start := time.Now()

	if step.SQL == nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: "sql step is missing exec or query block"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "vars", Path: failedVar, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	execution, evalErr := evaluateSQLExecution(evaluator, scope, scenarioName, step)
	if evalErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: evalErr.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: fmt.Sprintf("unknown provider %q", step.Provider)}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	output, err := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Phase:    phase,
		Attempt:  attempt,
		Config:   config,
		SQL:      execution,
	})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: err.Error()}
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
		"request":  cty.ObjectVal(output.Request),
		"response": cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = &report.ErrorDetail{Kind: "capture", Path: key, Message: err.Error()}
			stepReport.Duration = time.Since(start)

			return stepReport
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	stepReport.Duration = time.Since(start)

	return stepReport
}

// evaluateSQLExecution lowers the step's SQL block expressions into the
// concrete payload consumed by the SQL provider.
func evaluateSQLExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.SQLExecution, error) {
	connection, err := evalSQLString(evaluator, scope, scenarioName, step, "connection", step.SQL.Connection)
	if err != nil {
		return nil, err
	}

	var op *model.SQLOp

	var mode string

	switch {
	case step.SQL.Exec != nil:
		op = step.SQL.Exec
		mode = "exec"
	case step.SQL.Query != nil:
		op = step.SQL.Query
		mode = "query"
	default:
		return nil, fmt.Errorf("sql step must define exec or query")
	}

	statement, err := evalSQLString(evaluator, scope, scenarioName, step, mode+".sql", op.SQL)
	if err != nil {
		return nil, err
	}

	args, err := evalSQLArgs(evaluator, scope, scenarioName, step, mode+".args", op.Args)
	if err != nil {
		return nil, err
	}

	return &provider.SQLExecution{
		Connection: connection,
		Mode:       mode,
		SQL:        statement,
		Args:       args,
	}, nil
}

func evalSQLString(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression) (string, error) {
	if expression.Empty() {
		return "", fmt.Errorf("%s is required", path)
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path})
	if err != nil {
		return "", fmt.Errorf("evaluate %s: %w", path, err)
	}

	if value.IsNull() || !value.IsKnown() {
		return "", fmt.Errorf("%s must be a non-null string", path)
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("%s must be a string, got %s", path, value.Type().FriendlyName())
	}

	return value.AsString(), nil
}

func evalSQLArgs(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression) ([]any, error) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path})
	if err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", path, err)
	}

	if value.IsNull() {
		return nil, nil
	}

	if !value.Type().IsTupleType() && !value.Type().IsListType() {
		return nil, fmt.Errorf("%s must be a list, got %s", path, value.Type().FriendlyName())
	}

	values := value.AsValueSlice()

	converted, err := sqlprovider.ConvertArgs(values)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return converted, nil
}
