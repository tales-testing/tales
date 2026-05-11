package lang

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
)

func parseExpr(t *testing.T, src string) model.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse failed: %s", diags.Error())
	}
	return model.Expression{Expr: expr, File: "test.hcl", Line: 1}
}

func TestStepDependencies(t *testing.T) {
	t.Parallel()
	step := &model.Step{
		Name:     "b",
		Provider: "http",
		Request: &model.Request{
			URL: parseExpr(t, `"http://example/${result.a.id}"`),
		},
		DependsOn: []string{"c"},
		Capture:   map[string]model.Expression{},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("want 2 deps got %d", len(deps))
	}
	if _, ok := deps["a"]; !ok {
		t.Fatalf("missing implicit dep")
	}
	if _, ok := deps["c"]; !ok {
		t.Fatalf("missing explicit dep")
	}
}

func TestStepDependenciesIncludesBasicAuth(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "protected",
		Provider: "http",
		Request: &model.Request{
			URL: parseExpr(t, `"http://example.test"`),
			Auth: &model.RequestAuth{Basic: &model.BasicAuth{
				Username: parseExpr(t, `result.create_client.id`),
				Password: parseExpr(t, `result.create_client.secret`),
			}},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("want 1 dep got %d: %#v", len(deps), deps)
	}

	if _, ok := deps["create_client"]; !ok {
		t.Fatalf("missing auth implicit dep")
	}
}
