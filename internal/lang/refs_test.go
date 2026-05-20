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

func TestStepDependenciesIncludesMobileAction(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "login_flow",
		Provider: "mobile",
		Mobile: &model.MobileStep{
			Actions: []model.MobileAction{
				{
					Kind:  model.MobileActionInputText,
					ID:    parseExpr(t, `"auth.login.email"`),
					Value: parseExpr(t, `result.user.email`),
				},
			},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := deps["user"]; !ok {
		t.Fatalf("missing implicit dep on user from mobile action value: %#v", deps)
	}
}

func TestStepDependenciesIncludesMobileExpect(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "verify_profile",
		Provider: "mobile",
		Mobile: &model.MobileStep{
			Expect: model.MobileExpect{
				Text: []model.MobileValueExpectation{
					{
						ID:       parseExpr(t, `"profile.name"`),
						Expected: parseExpr(t, `result.profile.name`),
					},
				},
			},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := deps["profile"]; !ok {
		t.Fatalf("missing implicit dep on profile from mobile expect.text: %#v", deps)
	}
}

func TestStepDependenciesIncludesMobilePermissions(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "launch",
		Provider: "mobile",
		Mobile: &model.MobileStep{
			Permissions: []model.MobilePermission{
				{Service: "camera", Decision: parseExpr(t, `result.policy.camera`)},
			},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := deps["policy"]; !ok {
		t.Fatalf("missing implicit dep on policy from mobile permissions decision: %#v", deps)
	}
}

func TestStepDependenciesIncludesMobilePlatformAndLaunch(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "boot",
		Provider: "mobile",
		Mobile: &model.MobileStep{
			Platform: parseExpr(t, `result.cfg.platform`),
			Launch: &model.MobileLaunch{
				ClearState: parseExpr(t, `result.flags.clear`),
			},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := deps["cfg"]; !ok {
		t.Fatalf("missing implicit dep on cfg from mobile platform: %#v", deps)
	}

	if _, ok := deps["flags"]; !ok {
		t.Fatalf("missing implicit dep on flags from mobile launch.clear_state: %#v", deps)
	}
}

func TestStepDependenciesIncludesSkipRule(t *testing.T) {
	t.Parallel()

	step := &model.Step{
		Name:     "guarded",
		Provider: "http",
		Request: &model.Request{
			URL: parseExpr(t, `"http://example.test"`),
		},
		SkipRules: []model.SkipRule{
			{
				Kind:      model.SkipIf,
				Condition: parseExpr(t, `result.precond.ready`),
			},
			{
				Kind:   model.SkipUnless,
				Reason: parseExpr(t, `"blocked by ${result.ticket.id}"`),
			},
		},
	}

	deps, err := StepDependencies(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := deps["precond"]; !ok {
		t.Fatalf("missing implicit dep on precond from skip_if condition: %#v", deps)
	}

	if _, ok := deps["ticket"]; !ok {
		t.Fatalf("missing implicit dep on ticket from skip_unless reason: %#v", deps)
	}
}
