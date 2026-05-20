package lang

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/internal/model"
)

// FindStepRefs returns referenced result.<step> names from expression.
func FindStepRefs(expr hcl.Expression) []string {
	if expr == nil {
		return nil
	}

	refs := map[string]struct{}{}

	for _, traversal := range expr.Variables() {
		if len(traversal) < 2 {
			continue
		}

		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok || root.Name != "result" {
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
	collectSkipRefs(step.SkipRules, collect)

	delete(deps, step.Name)

	for dep := range deps {
		if dep == step.Name {
			return nil, fmt.Errorf("step %q cannot depend on itself", step.Name)
		}
	}

	return deps, nil
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
