package lang

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func emptyScope() ScopeData {
	return ScopeData{
		Config:   map[string]cty.Value{},
		Result:   map[string]cty.Value{},
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    map[string]cty.Value{},
	}
}

func TestScopeVarsVisibleInExpressions(t *testing.T) {
	t.Parallel()

	ev := NewEvaluator(nil)
	ev.SetScopeVars(map[string]cty.Value{
		"scenario": cty.ObjectVal(map[string]cty.Value{
			"workdir":       cty.StringVal("/work/sc"),
			"artifacts_dir": cty.StringVal("/work/sc/artifacts"),
		}),
		"project": cty.ObjectVal(map[string]cty.Value{
			"dir": cty.StringVal("/project"),
		}),
	})

	for _, tc := range []struct {
		src  string
		want string
	}{
		{`scenario.workdir`, "/work/sc"},
		{`scenario.artifacts_dir`, "/work/sc/artifacts"},
		{`project.dir`, "/project"},
		{`"${project.dir}/fixtures/x"`, "/project/fixtures/x"},
	} {
		got, err := ev.Eval(parseExpr(t, tc.src), emptyScope(), GenerateMeta{})
		if err != nil {
			t.Fatalf("eval %q: %v", tc.src, err)
		}

		if got.AsString() != tc.want {
			t.Fatalf("eval %q = %q, want %q", tc.src, got.AsString(), tc.want)
		}
	}
}

func TestScopeVarsAbsentWhenUnset(t *testing.T) {
	t.Parallel()

	ev := NewEvaluator(nil)

	if _, err := ev.Eval(parseExpr(t, `scenario.workdir`), emptyScope(), GenerateMeta{}); err == nil {
		t.Fatal("expected scenario.workdir to be undefined without SetScopeVars")
	}
}

// TestExtraVarsOverrideScopeVars confirms a per-call extra of the same name
// wins over the scenario-level scope var, matching the documented precedence.
func TestExtraVarsOverrideScopeVars(t *testing.T) {
	t.Parallel()

	ev := NewEvaluator(nil)
	ev.SetScopeVars(map[string]cty.Value{
		"scenario": cty.ObjectVal(map[string]cty.Value{"workdir": cty.StringVal("base")}),
	})

	override := map[string]cty.Value{
		"scenario": cty.ObjectVal(map[string]cty.Value{"workdir": cty.StringVal("override")}),
	}

	got, err := ev.EvalWithExtras(parseExpr(t, `scenario.workdir`), emptyScope(), GenerateMeta{}, nil, override)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}

	if got.AsString() != "override" {
		t.Fatalf("eval = %q, want override", got.AsString())
	}
}
