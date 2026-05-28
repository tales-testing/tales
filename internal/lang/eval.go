package lang

import (
	"errors"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// GenerateMeta carries source data for deterministic generation.
type GenerateMeta struct {
	Scenario string
	Step     string
	ExprPath string
}

// GenerateFunc resolves generate(name).
type GenerateFunc func(name string, meta GenerateMeta) (cty.Value, error)

// ScopeData holds values available while evaluating one expression.
//
// Vars holds step-local variables declared in a step's vars block. It is
// populated by the runtime after evaluating each var in source order, and
// is empty (nil) outside the step body — when, skip rules, config and
// generator expressions never see it.
type ScopeData struct {
	Config   map[string]cty.Value
	Result   map[string]cty.Value
	Request  map[string]cty.Value
	Response map[string]cty.Value
	Input    map[string]cty.Value
	Vars     map[string]cty.Value
}

// Evaluator evaluates HCL expressions for runtime.
type Evaluator struct {
	baseFunctions map[string]function.Function
	generate      GenerateFunc
}

// NewEvaluator creates evaluator with built-in functions.
func NewEvaluator(generate GenerateFunc) *Evaluator {
	return &Evaluator{baseFunctions: baseFunctions(), generate: generate}
}

// Eval evaluates expression using scope data.
func (e *Evaluator) Eval(expression model.Expression, scope ScopeData, meta GenerateMeta) (cty.Value, error) {
	return e.EvalWithExtras(expression, scope, meta, nil)
}

// EvalWithExtras evaluates expression with the standard scope and an
// additional, caller-provided set of functions merged into the EvalContext.
// Caller-provided functions override any built-in of the same name.
//
// extraVars (variadic, optional) are merged on top of the built-in scope
// variables. Later maps override earlier ones; extras override built-ins of
// the same name. This is how the browser provider exposes the `browser`
// namespace (browser.url, browser.title) inside step-scoped capture eval.
func (e *Evaluator) EvalWithExtras(expression model.Expression, scope ScopeData, meta GenerateMeta, extras map[string]function.Function, extraVars ...map[string]cty.Value) (cty.Value, error) {
	if expression.Empty() {
		return cty.NullVal(cty.DynamicPseudoType), nil
	}

	varsValue := cty.EmptyObjectVal
	if len(scope.Vars) > 0 {
		varsValue = cty.ObjectVal(scope.Vars)
	}

	variables := map[string]cty.Value{
		"config":   cty.ObjectVal(scope.Config),
		"result":   cty.ObjectVal(scope.Result),
		"request":  cty.ObjectVal(scope.Request),
		"response": cty.ObjectVal(scope.Response),
		"input":    cty.ObjectVal(scope.Input),
		"host":     hostObject(),
		"vars":     varsValue,
	}

	for _, extra := range extraVars {
		for name, value := range extra {
			variables[name] = value
		}
	}

	ctx := &hcl.EvalContext{
		Variables: variables,
		Functions: map[string]function.Function{},
	}

	for name, fn := range e.baseFunctions {
		ctx.Functions[name] = fn
	}

	ctx.Functions["generate"] = function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramName, Type: cty.String}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if e.generate == nil {
				return cty.NilVal, fmt.Errorf("generate() is unavailable")
			}

			return e.generate(args[0].AsString(), meta)
		},
	})

	for name, fn := range extras {
		ctx.Functions[name] = fn
	}

	val, diags := expression.Expr.Value(ctx)
	if diags.HasErrors() {
		return cty.NilVal, errors.New(diags.Error())
	}

	return val, nil
}

// EvalRaw evaluates hcl expression directly.
func (e *Evaluator) EvalRaw(expr hcl.Expression, scope ScopeData, meta GenerateMeta) (cty.Value, error) {
	return e.Eval(model.Expression{Expr: expr}, scope, meta)
}
