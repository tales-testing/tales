package lang

import (
	"fmt"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/internal/model"
)

// FindStepRefs returns referenced result.<step> names from expression.
func FindStepRefs(expr hcl.Expression) []string {
	return findRootAttrRefs(expr, "result")
}

// FindVarRefs returns referenced vars.<name> names from expression.
func FindVarRefs(expr hcl.Expression) []string {
	return findRootAttrRefs(expr, "vars")
}

func findRootAttrRefs(expr hcl.Expression, rootName string) []string {
	if expr == nil {
		return nil
	}

	refs := map[string]struct{}{}

	for _, traversal := range expr.Variables() {
		if len(traversal) < 2 {
			continue
		}

		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok || root.Name != rootName {
			continue
		}

		attr, ok := traversal[1].(hcl.TraverseAttr)
		if !ok {
			continue
		}

		refs[attr.Name] = struct{}{}
	}

	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}

	return out
}

// StepDependencies builds dependency set for one step from explicit and implicit references.
func StepDependencies(step *model.Step) (map[string]struct{}, error) {
	deps := map[string]struct{}{}
	for _, dep := range step.DependsOn {
		deps[dep] = struct{}{}
	}

	collect := func(expression model.Expression) {
		for _, dep := range FindStepRefs(expression.Expr) {
			deps[dep] = struct{}{}
		}
	}

	if step.When.Expr != nil {
		collect(step.When)
	}

	collectRequestRefs(step.Request, collect)
	collectExpectRefs(step.Expect, collect)

	for _, capExpr := range step.Capture {
		collect(capExpr)
	}

	if step.Keyword != nil {
		collect(step.Keyword.Name)
		collect(step.Keyword.Inputs)
	}

	collectMobileRefs(step.Mobile, collect)
	collectSQLRefs(step.SQL, collect)
	collectSkipRefs(step.SkipRules, collect)

	// A step referencing its own name — through depends_on or a
	// result.<self> expression — is always invalid: its result does not
	// exist yet while it runs.
	if _, selfRef := deps[step.Name]; selfRef {
		return nil, fmt.Errorf("step %q cannot depend on itself", step.Name)
	}

	return deps, nil
}

// ValidateStepOrder verifies that every step references only steps defined
// earlier in file order, through either depends_on or result.<x> expressions.
// steps must be in .tales source order. externalDeps holds names resolvable
// outside the list (for example results injected into a keyword by its
// caller); they never trigger a forward-reference error.
func ValidateStepOrder(steps []*model.Step, externalDeps map[string]struct{}) error {
	known := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		known[step.Name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(steps))

	for _, step := range steps {
		if err := validateStepRefs(step, seen, known, externalDeps); err != nil {
			return err
		}

		seen[step.Name] = struct{}{}
	}

	return nil
}

// validateStepRefs checks one step's explicit and implicit dependencies
// against the steps already seen, the full set of known step names, and the
// externally resolvable names.
func validateStepRefs(step *model.Step, seen, known, externalDeps map[string]struct{}) error {
	for _, dep := range step.DependsOn {
		if dep == step.Name {
			continue // self-reference is reported by StepDependencies below
		}

		if _, ok := seen[dep]; ok {
			continue
		}

		if _, ok := externalDeps[dep]; ok {
			continue
		}

		if _, ok := known[dep]; ok {
			return fmt.Errorf("step %q depends on %q, but %q is defined later", step.Name, dep, dep)
		}

		return fmt.Errorf("step %q depends on unknown step %q", step.Name, dep)
	}

	deps, err := StepDependencies(step)
	if err != nil {
		return err
	}

	for dep := range deps {
		if slices.Contains(step.DependsOn, dep) {
			continue // already validated in the depends_on loop above
		}

		if _, ok := seen[dep]; ok {
			continue
		}

		if _, ok := externalDeps[dep]; ok {
			continue
		}

		if _, ok := known[dep]; ok {
			return fmt.Errorf("step %q references result.%s, but %q is defined later", step.Name, dep, dep)
		}

		return fmt.Errorf("step %q references unknown dependency %q", step.Name, dep)
	}

	return nil
}

func collectRequestRefs(req *model.Request, collect func(model.Expression)) {
	if req == nil {
		return
	}

	collect(req.Method)
	collect(req.URL)
	collect(req.Headers)
	collect(req.Query)

	if req.Body != nil {
		collect(req.Body.JSON)
		collect(req.Body.Form)
		collect(req.Body.Raw)
	}

	if req.Auth != nil && req.Auth.Basic != nil {
		collect(req.Auth.Basic.Username)
		collect(req.Auth.Basic.Password)
	}

	collect(req.Timeout)
}

func collectExpectRefs(expect *model.Expect, collect func(model.Expression)) {
	if expect == nil {
		return
	}

	collect(expect.Status)
	collect(expect.Headers)
	collect(expect.JSON)
	collect(expect.Body)
	collect(expect.Strict)
}

func collectMobileRefs(mob *model.MobileStep, collect func(model.Expression)) {
	if mob == nil {
		return
	}

	collect(mob.Platform)
	collect(mob.Target)

	if mob.Launch != nil {
		collect(mob.Launch.ClearState)
	}

	for _, action := range mob.Actions {
		collect(action.ID)
		collect(action.Value)
		collect(action.Secure)
		collect(action.Timeout)
		collect(action.Interval)
		collect(action.Direction)
		collect(action.Distance)
		collect(action.Duration)
	}

	for _, permission := range mob.Permissions {
		collect(permission.Decision)
	}

	collectMobileExpectRefs(mob.Expect, collect)
}

func collectMobileExpectRefs(expect model.MobileExpect, collect func(model.Expression)) {
	for _, v := range expect.Visible {
		collect(v.ID)
		collect(v.Timeout)
		collect(v.Interval)
	}

	for _, v := range expect.NotVisible {
		collect(v.ID)
		collect(v.Timeout)
		collect(v.Interval)
	}

	for _, v := range expect.Text {
		collect(v.ID)
		collect(v.Expected)
		collect(v.Timeout)
		collect(v.Interval)
	}

	for _, v := range expect.Value {
		collect(v.ID)
		collect(v.Expected)
		collect(v.Timeout)
		collect(v.Interval)
	}

	for _, v := range expect.Enabled {
		collect(v.ID)
		collect(v.Timeout)
		collect(v.Interval)
	}

	for _, v := range expect.Disabled {
		collect(v.ID)
		collect(v.Timeout)
		collect(v.Interval)
	}
}

func collectSQLRefs(sql *model.SQLCall, collect func(model.Expression)) {
	if sql == nil {
		return
	}

	collect(sql.Connection)

	if sql.Exec != nil {
		collect(sql.Exec.SQL)
		collect(sql.Exec.Args)
	}

	if sql.Query != nil {
		collect(sql.Query.SQL)
		collect(sql.Query.Args)
	}
}

func collectSkipRefs(rules []model.SkipRule, collect func(model.Expression)) {
	for _, rule := range rules {
		collect(rule.Condition)
		collect(rule.Reason)
		collect(rule.OS)
		collect(rule.Arch)
		collect(rule.EnvSet)
		collect(rule.Env)
	}
}

// ValidateStepVars enforces the contract for step-local vars at load time:
// each var may reference only vars declared earlier in the same block, no
// var name is duplicated, and every vars.<name> consumed by the rest of the
// step (request, expect, capture, etc.) is declared in this step's vars.
// Cross-step var sharing is intentionally not supported — use capture.
func ValidateStepVars(step *model.Step) error {
	declared := make(map[string]struct{}, len(step.Vars))

	for _, v := range step.Vars {
		for _, ref := range FindVarRefs(v.Expr.Expr) {
			if ref == v.Name {
				return fmt.Errorf("step %q variable %q cannot reference itself", step.Name, v.Name)
			}

			if _, ok := declared[ref]; !ok {
				return fmt.Errorf("step %q variable %q references vars.%s before it is defined", step.Name, v.Name, ref)
			}
		}

		if _, dup := declared[v.Name]; dup {
			return fmt.Errorf("duplicate variable %q in step %q", v.Name, step.Name)
		}

		declared[v.Name] = struct{}{}
	}

	seen := map[string]struct{}{}

	collect := func(expression model.Expression) {
		for _, ref := range FindVarRefs(expression.Expr) {
			seen[ref] = struct{}{}
		}
	}

	if step.When.Expr != nil {
		collect(step.When)
	}

	collectRequestRefs(step.Request, collect)
	collectExpectRefs(step.Expect, collect)

	for _, capExpr := range step.Capture {
		collect(capExpr)
	}

	if step.Keyword != nil {
		collect(step.Keyword.Name)
		collect(step.Keyword.Inputs)
	}

	collectMobileRefs(step.Mobile, collect)
	collectSQLRefs(step.SQL, collect)
	collectSkipRefs(step.SkipRules, collect)

	for ref := range seen {
		if _, ok := declared[ref]; !ok {
			return fmt.Errorf("step %q references unknown variable vars.%s", step.Name, ref)
		}
	}

	return nil
}
