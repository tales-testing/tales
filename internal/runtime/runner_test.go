package runtime

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/lang"
	"github.com/hyperxlab/tales/internal/model"
)

func parseExprForTest(t *testing.T, src string) model.Expression {
	t.Helper()

	expr, diags := hclsyntax.ParseExpression([]byte(src), "runner_test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("parse expression %q: %s", src, diags.Error())
	}

	return model.Expression{Expr: expr, File: "runner_test.hcl", Line: 1}
}

func TestFilterScenarios(t *testing.T) {
	t.Parallel()

	scenarios := []*model.Scenario{
		{Name: "alpha", Tags: []string{"smoke", "fast"}},
		{Name: "beta", Tags: []string{"smoke", "slow"}},
		{Name: "gamma", Tags: []string{"auth"}},
		{Name: "delta"}, // no tags
	}

	cases := []struct {
		name     string
		tags     []string
		scenario string
		want     []string
	}{
		{
			name: "no filter returns all scenarios",
			want: []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			name: "single tag matches scenarios carrying it",
			tags: []string{"smoke"},
			want: []string{"alpha", "beta"},
		},
		{
			name: "multiple tags union (OR semantics)",
			tags: []string{"smoke", "auth"},
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "unknown tag matches nothing",
			tags: []string{"missing"},
			want: []string{},
		},
		{
			name:     "scenario name selects exactly one regardless of tags",
			scenario: "delta",
			want:     []string{"delta"},
		},
		{
			name:     "scenario name combined with tag must satisfy both",
			scenario: "alpha",
			tags:     []string{"smoke"},
			want:     []string{"alpha"},
		},
		{
			name:     "scenario name combined with non-matching tag drops it",
			scenario: "alpha",
			tags:     []string{"auth"},
			want:     []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filterScenarios(scenarios, tc.tags, tc.scenario)

			names := make([]string, 0, len(got))
			for _, s := range got {
				names = append(names, s.Name)
			}

			if len(names) != len(tc.want) {
				t.Fatalf("got %d scenarios (%v), want %d (%v)", len(names), names, len(tc.want), tc.want)
			}

			for i, want := range tc.want {
				if names[i] != want {
					t.Fatalf("scenario[%d] = %q, want %q (full=%v)", i, names[i], want, names)
				}
			}
		})
	}
}

// TestValidateStepOrderMobileAfterKeyword checks that a mobile step whose
// action references a keyword step's result is accepted when defined after
// that keyword step, and rejected as a forward reference when defined before.
func TestValidateStepOrderMobileAfterKeyword(t *testing.T) {
	t.Parallel()

	userStep := &model.Step{
		Name:     "user",
		Provider: "keyword",
		Keyword: &model.KeywordCall{
			Name:   parseExprForTest(t, `"register_user"`),
			Inputs: model.Expression{},
		},
	}

	loginStep := &model.Step{
		Name:     "login_flow",
		Provider: "mobile",
		Mobile: &model.MobileStep{
			Platform: parseExprForTest(t, `"ios"`),
			Actions: []model.MobileAction{
				{
					Kind:  model.MobileActionInputText,
					ID:    parseExprForTest(t, `"auth.login.email"`),
					Value: parseExprForTest(t, `result.user.email`),
				},
			},
		},
	}

	if err := lang.ValidateStepOrder([]*model.Step{userStep, loginStep}, nil); err != nil {
		t.Fatalf("mobile step after the keyword it references must be valid: %v", err)
	}

	if err := lang.ValidateStepOrder([]*model.Step{loginStep, userStep}, nil); err == nil {
		t.Fatal("mobile step before the keyword it references must be rejected")
	}
}
