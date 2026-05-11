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

	if step.Request != nil {
		collect(step.Request.Method)
		collect(step.Request.URL)
		collect(step.Request.Headers)
		collect(step.Request.Query)
		collect(step.Request.JSON)
		collect(step.Request.Body)
		collect(step.Request.Timeout)
	}

	if step.Expect != nil {
		collect(step.Expect.Status)
		collect(step.Expect.Headers)
		collect(step.Expect.JSON)
		collect(step.Expect.Body)
		collect(step.Expect.Strict)
	}

	for _, capExpr := range step.Capture {
		collect(capExpr)
	}

	if step.Keyword != nil {
		collect(step.Keyword.Name)
		collect(step.Keyword.Inputs)
	}

	delete(deps, step.Name)

	for dep := range deps {
		if dep == step.Name {
			return nil, fmt.Errorf("step %q cannot depend on itself", step.Name)
		}
	}

	return deps, nil
}
