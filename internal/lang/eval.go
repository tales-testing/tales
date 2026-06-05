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
//
// seedScope carries the keyword call stack (calling step names joined by NUL)
// for the generation currently in flight. It lets the runtime mix the call
// site into the deterministic seed so the same keyword invoked from different
// steps produces distinct generated values. It is mutated single-threaded: one
// Evaluator is created per scenario and steps (including nested keywords) run
// sequentially, so no lock is needed.
type Evaluator struct {
	baseFunctions map[string]function.Function
	generate      GenerateFunc
	seedScope     string
	// scopeVars holds scenario-level namespaces (scenario, project) merged
	// into every EvalContext. Like seedScope it is scenario-scoped: one
	// Evaluator per scenario, set once before the steps run and read
	// single-threaded thereafter (keyword and teardown steps reuse it).
	scopeVars map[string]cty.Value
}

// NewEvaluator creates evaluator with built-in functions.
func NewEvaluator(generate GenerateFunc) *Evaluator {
	return &Evaluator{baseFunctions: baseFunctions(), generate: generate}
}

// SetScopeVars installs the scenario-level namespaces (scenario, project)
// exposed to every expression evaluated by this Evaluator. The runtime calls
// it once per scenario after creating the workspace; per-call extraVars passed
// to EvalWithExtras still override these of the same name.
func (e *Evaluator) SetScopeVars(vars map[string]cty.Value) {
	e.scopeVars = vars
}

// SeedScope returns the current keyword call-stack scope used for deterministic
// seed derivation. It is empty at scenario level.
func (e *Evaluator) SeedScope() string {
	return e.seedScope
}

// PushSeedScope appends part to the seed scope and returns a function that
// restores the previous scope. Callers wrap keyword execution with it so
// generations inside the keyword are namespaced by the calling step.
func (e *Evaluator) PushSeedScope(part string) func() {
	previous := e.seedScope

	if e.seedScope == "" {
		e.seedScope = part
	} else {
		e.seedScope = e.seedScope + "\x00" + part
	}

	return func() { e.seedScope = previous }
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

	// Scenario-level namespaces (scenario, project) come before per-call
	// extras so a caller-provided extra of the same name still wins.
	for name, value := range e.scopeVars {
		variables[name] = value
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
