package runtime

import (
	"time"

	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// evalExecString evaluates an optional string expression for the exec step.
// When required is true an empty or null result is an error; otherwise it
// returns "".
func evalExecString(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression, required bool) (string, *report.ErrorDetail) {
	if expression.Empty() {
		if required {
			return "", &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " is required"}
		}

		return "", nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: name})
	if err != nil {
		return "", &report.ErrorDetail{Kind: kindEval, Path: name, Message: err.Error()}
	}

	if value.IsNull() {
		if required {
			return "", &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " must not be null"}
		}

		return "", nil
	}

	if value.Type() != cty.String {
		return "", &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " must be a string"}
	}

	return value.AsString(), nil
}

// assignExecString assigns the evaluated string into dst when the expression
// is set, leaving the default in place otherwise.
func assignExecString(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression, dst *string) *report.ErrorDetail {
	if expression.Empty() {
		return nil
	}

	value, detail := evalExecString(evaluator, scope, scenarioName, step, name, expression, false)
	if detail != nil {
		return detail
	}

	if value != "" {
		*dst = value
	}

	return nil
}

// evalExecStringList evaluates an optional list(string) expression (exec args).
func evalExecStringList(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression) ([]string, *report.ErrorDetail) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: name})
	if err != nil {
		return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: err.Error()}
	}

	if value.IsNull() {
		return nil, nil
	}

	if !value.Type().IsTupleType() && !value.Type().IsListType() {
		return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " must be a list of strings"}
	}

	out := make([]string, 0, value.LengthInt())

	for _, item := range value.AsValueSlice() {
		if item.IsNull() || item.Type() != cty.String {
			return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " entries must be strings"}
		}

		out = append(out, item.AsString())
	}

	return out, nil
}

// evalExecStringMap evaluates an optional map(string) expression (exec env).
func evalExecStringMap(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression) (map[string]string, *report.ErrorDetail) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: name})
	if err != nil {
		return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: err.Error()}
	}

	if value.IsNull() {
		return nil, nil
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " must be a map of strings"}
	}

	out := map[string]string{}

	for key, item := range value.AsValueMap() {
		if item.IsNull() || item.Type() != cty.String {
			return nil, &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " values must be strings"}
		}

		out[key] = item.AsString()
	}

	return out, nil
}

// evalExecBool evaluates an optional bool expression, defaulting to false.
func evalExecBool(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, name string, expression model.Expression) (bool, *report.ErrorDetail) {
	if expression.Empty() {
		return false, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: name})
	if err != nil {
		return false, &report.ErrorDetail{Kind: kindEval, Path: name, Message: err.Error()}
	}

	if value.IsNull() {
		return false, nil
	}

	if value.Type() != cty.Bool {
		return false, &report.ErrorDetail{Kind: kindEval, Path: name, Message: name + " must be a boolean"}
	}

	return value.True(), nil
}

// evalExecTimeout evaluates the optional timeout, defaulting to 30s. It accepts
// a duration string ("30s") or a number of milliseconds, reusing toDuration.
func evalExecTimeout(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, expression model.Expression) (time.Duration, *report.ErrorDetail) {
	if expression.Empty() {
		return defaultExecTimeout, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: pathTimeout})
	if err != nil {
		return 0, &report.ErrorDetail{Kind: kindEval, Path: pathTimeout, Message: err.Error()}
	}

	if value.IsNull() {
		return defaultExecTimeout, nil
	}

	duration, err := toDuration(value)
	if err != nil {
		return 0, &report.ErrorDetail{Kind: kindEval, Path: pathTimeout, Message: err.Error()}
	}

	return duration, nil
}
