package runtime

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/lang"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

func newSkipEvaluator() *lang.Evaluator {
	return lang.NewEvaluator(nil)
}

func emptySkipScope() lang.ScopeData {
	return lang.ScopeData{
		Config:   map[string]cty.Value{},
		Result:   map[string]cty.Value{},
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    map[string]cty.Value{},
	}
}

func skipRule(t *testing.T, kind model.SkipKind, attrs map[string]string) model.SkipRule {
	t.Helper()

	rule := model.SkipRule{Kind: kind}

	for name, src := range attrs {
		expression := expr(src)

		switch name {
		case "condition":
			rule.Condition = expression
		case "reason":
			rule.Reason = expression
		case "os":
			rule.OS = expression
		case "arch":
			rule.Arch = expression
		case "env_set":
			rule.EnvSet = expression
		case "env":
			rule.Env = expression
		default:
			t.Fatalf("unknown skip attr %q", name)
		}
	}

	return rule
}

func TestSkipUnlessOSMatchesNotSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"os": `["` + goruntime.GOOS + `"]`,
	})

	skipped, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skipped {
		t.Fatalf("expected not skipped, got reason: %s", reason)
	}
}

func TestSkipUnlessOSMismatchSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"os": `["definitely-not-a-real-os"]`,
	})

	skipped, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped")
	}

	if !strings.Contains(reason, goruntime.GOOS) {
		t.Fatalf("auto reason should mention host.os, got %q", reason)
	}
}

func TestSkipIfOSMatchesSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"os": `["` + goruntime.GOOS + `"]`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped for skip_if os matching")
	}
}

func TestSkipUnlessArchMatchesNotSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"arch": `["` + goruntime.GOARCH + `"]`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skipped {
		t.Fatalf("expected not skipped")
	}
}

func TestSkipUnlessEnvSetMissingSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"env_set": `["TALES_SKIP_TEST_UNSET_VAR"]`,
	})

	skipped, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped because env var is unset")
	}

	if !strings.Contains(reason, "TALES_SKIP_TEST_UNSET_VAR") {
		t.Fatalf("auto reason should mention missing env var, got %q", reason)
	}
}

func TestSkipUnlessEnvSetPresentNotSkipped(t *testing.T) {
	t.Setenv("TALES_SKIP_TEST_SET_VAR", "1")

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"env_set": `["TALES_SKIP_TEST_SET_VAR"]`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skipped {
		t.Fatalf("expected not skipped when env var is set")
	}
}

func TestSkipIfEnvMatchingSkipped(t *testing.T) {
	t.Setenv("TALES_SKIP_TEST_VALUE", "yes")

	rule := skipRule(t, model.SkipIf, map[string]string{
		"env": `{ TALES_SKIP_TEST_VALUE = "yes" }`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped when env value matches")
	}
}

func TestSkipUnlessEnvMismatchSkipped(t *testing.T) {
	t.Setenv("TALES_SKIP_TEST_VALUE_MM", "actual")

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"env": `{ TALES_SKIP_TEST_VALUE_MM = "expected" }`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped when env value mismatches under skip_unless")
	}
}

func TestSkipIfConditionTrue(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"condition": `true`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped for condition=true")
	}
}

func TestSkipIfConditionFalseNotSkipped(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"condition": `false`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skipped {
		t.Fatalf("expected not skipped for condition=false")
	}
}

func TestSkipConditionNonBoolErrors(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"condition": `"not-a-bool"`,
	})

	_, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err == nil {
		t.Fatalf("expected error for non-bool condition")
	}
}

func TestSkipUnlessANDSemanticsWithinBlock(t *testing.T) {
	// os matches, but env_set is missing — combined matched is false, so
	// skip_unless triggers a skip.
	rule := skipRule(t, model.SkipUnless, map[string]string{
		"os":      `["` + goruntime.GOOS + `"]`,
		"env_set": `["TALES_SKIP_TEST_UNSET_AND_VAR"]`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped because env_set fails despite os match (AND semantics)")
	}
}

func TestSkipMultipleRulesFirstTriggeringWins(t *testing.T) {
	t.Parallel()

	first := skipRule(t, model.SkipIf, map[string]string{
		"condition": `false`,
		"reason":    `"first not triggered"`,
	})
	second := skipRule(t, model.SkipIf, map[string]string{
		"condition": `true`,
		"reason":    `"second wins"`,
	})

	skipped, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{first, second}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !skipped {
		t.Fatalf("expected skipped")
	}

	if reason != "second wins" {
		t.Fatalf("expected second rule reason, got %q", reason)
	}
}

func TestSkipExplicitReasonUsed(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"condition": `true`,
		"reason":    `"because I said so"`,
	})

	_, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reason != "because I said so" {
		t.Fatalf("expected explicit reason, got %q", reason)
	}
}

func TestSkipAutoReasonWhenNoExplicit(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipIf, map[string]string{
		"condition": `true`,
	})

	_, reason, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reason == "" {
		t.Fatalf("expected an auto-generated reason")
	}

	if !strings.Contains(reason, "skip_if") {
		t.Fatalf("auto reason should mention skip kind, got %q", reason)
	}
}

func TestSkipConditionWithHostReference(t *testing.T) {
	t.Parallel()

	rule := skipRule(t, model.SkipUnless, map[string]string{
		"condition": `host.os == "` + goruntime.GOOS + `"`,
	})

	skipped, _, err := evaluateSkipRules(newSkipEvaluator(), []model.SkipRule{rule}, emptySkipScope(), lang.GenerateMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skipped {
		t.Fatalf("expected not skipped — host.os should be available in condition")
	}
}
