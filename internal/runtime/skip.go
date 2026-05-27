package runtime

import (
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// applyScenarioSkip evaluates scenario-level skip rules and mutates
// sResult in place when the scenario must be marked skipped (or when
// the rule evaluation itself fails). Returns true when the scenario
// is terminal — caller must return without running steps or teardown.
func applyScenarioSkip(evaluator *lang.Evaluator, scenario *model.Scenario, sResult *report.ScenarioResult, state *ScenarioState, config map[string]cty.Value, start time.Time) bool {
	if len(scenario.SkipRules) == 0 {
		return false
	}

	scope := skipScope(config, state, nil)
	meta := lang.GenerateMeta{Scenario: scenario.Name, ExprPath: "scenario.skip"}

	skipped, reason, err := evaluateSkipRules(evaluator, scenario.SkipRules, scope, meta)
	if err != nil {
		sResult.Status = report.StatusFail
		sResult.Failure = &report.ErrorDetail{Kind: kindSkip, Message: err.Error()}
		sResult.Duration = time.Since(start)

		return true
	}

	if skipped {
		sResult.Status = report.StatusSkip
		sResult.SkipReason = reason
		sResult.Duration = time.Since(start)

		return true
	}

	return false
}

// evaluateStepSkip evaluates step-level skip rules and returns the
// StepResult to record when the step must not execute (either because
// it was skipped or because evaluating the rules itself errored).
// Returns nil when the step should proceed normally.
func evaluateStepSkip(evaluator *lang.Evaluator, step *model.Step, scenarioName string, state *ScenarioState, config map[string]cty.Value) *report.StepResult {
	if len(step.SkipRules) == 0 {
		return nil
	}

	scope := skipScope(config, state, nil)
	meta := lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "step.skip"}

	skipped, reason, err := evaluateSkipRules(evaluator, step.SkipRules, scope, meta)
	if err != nil {
		return &report.StepResult{
			File:     step.File,
			Scenario: scenarioName,
			Name:     step.Name,
			Provider: step.Provider,
			Phase:    phaseStep,
			Status:   report.StatusFail,
			Failure:  &report.ErrorDetail{Kind: kindSkip, Message: err.Error()},
		}
	}

	if !skipped {
		return nil
	}

	return &report.StepResult{
		File:       step.File,
		Scenario:   scenarioName,
		Name:       step.Name,
		Provider:   step.Provider,
		Phase:      phaseStep,
		Status:     report.StatusSkip,
		SkipReason: reason,
	}
}

// skipScope builds the ScopeData passed to skip-rule evaluation.
// Scenario-level rules should pass nil for input; step-level rules
// pass the keyword input map when relevant.
func skipScope(config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value) lang.ScopeData {
	return lang.ScopeData{
		Config:   config,
		Result:   state.GetResultMap(),
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    ensureValueMap(input),
	}
}

// skipDecision is the outcome of evaluating a single skip rule.
type skipDecision struct {
	skipped bool
	reason  string
}

// evaluateSkipRules walks a slice of skip rules in order and returns
// the first decision that triggers a skip. Returns (false, "", nil)
// when no rule triggers and (true, reason, nil) otherwise.
//
// scope and meta are forwarded verbatim to the expression evaluator
// so callers control which variables (config, result, host, ...) are
// in scope for the rule's `condition`.
//
// Evaluator errors (non-bool `condition`, non-list `os` / `arch` /
// `env_set`, non-string `env` value, ...) are propagated to the
// caller. The runner translates them into a failed scenario/step
// with kind="skip", because silently passing on a malformed rule
// would mask user-visible misconfiguration.
func evaluateSkipRules(evaluator *lang.Evaluator, rules []model.SkipRule, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	for i := range rules {
		decision, err := evaluateSkipRule(evaluator, rules[i], scope, meta)
		if err != nil {
			return false, "", fmt.Errorf("rule[%d]: %w", i, err)
		}

		if decision.skipped {
			return true, decision.reason, nil
		}
	}

	return false, "", nil
}

func evaluateSkipRule(evaluator *lang.Evaluator, rule model.SkipRule, scope lang.ScopeData, meta lang.GenerateMeta) (skipDecision, error) {
	matched, reasons, err := matchSkipAttributes(evaluator, rule, scope, meta)
	if err != nil {
		return skipDecision{}, err
	}

	triggered := false

	switch rule.Kind {
	case model.SkipIf:
		triggered = matched
	case model.SkipUnless:
		triggered = !matched
	default:
		return skipDecision{}, fmt.Errorf("unknown skip kind %q", rule.Kind)
	}

	if !triggered {
		return skipDecision{}, nil
	}

	reason, err := resolveSkipReason(evaluator, rule, scope, meta, reasons)
	if err != nil {
		return skipDecision{}, err
	}

	return skipDecision{skipped: true, reason: reason}, nil
}

// matchSkipAttributes returns whether every populated attribute on the
// rule is satisfied (AND semantics). It also returns a list of
// per-attribute auto-reasons describing why each attribute did or did
// not match, used to build a default reason when the user did not
// supply one.
func matchSkipAttributes(evaluator *lang.Evaluator, rule model.SkipRule, scope lang.ScopeData, meta lang.GenerateMeta) (bool, []string, error) {
	matched := true
	reasons := make([]string, 0, 5)

	if !rule.OS.Empty() {
		ok, why, err := matchOS(evaluator, rule.OS, scope, meta)
		if err != nil {
			return false, nil, fmt.Errorf("os: %w", err)
		}

		matched = matched && ok

		reasons = append(reasons, why)
	}

	if !rule.Arch.Empty() {
		ok, why, err := matchArch(evaluator, rule.Arch, scope, meta)
		if err != nil {
			return false, nil, fmt.Errorf("arch: %w", err)
		}

		matched = matched && ok

		reasons = append(reasons, why)
	}

	if !rule.EnvSet.Empty() {
		ok, why, err := matchEnvSet(evaluator, rule.EnvSet, scope, meta)
		if err != nil {
			return false, nil, fmt.Errorf("env_set: %w", err)
		}

		matched = matched && ok

		reasons = append(reasons, why)
	}

	if !rule.Env.Empty() {
		ok, why, err := matchEnv(evaluator, rule.Env, scope, meta)
		if err != nil {
			return false, nil, fmt.Errorf("env: %w", err)
		}

		matched = matched && ok

		reasons = append(reasons, why)
	}

	if !rule.Condition.Empty() {
		ok, why, err := matchCondition(evaluator, rule.Condition, scope, meta)
		if err != nil {
			return false, nil, fmt.Errorf("condition: %w", err)
		}

		matched = matched && ok

		reasons = append(reasons, why)
	}

	return matched, reasons, nil
}

func matchOS(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	list, err := evalStringList(evaluator, expression, scope, meta, "os")
	if err != nil {
		return false, "", err
	}

	if slices.Contains(list, goruntime.GOOS) {
		return true, fmt.Sprintf("host.os %q is in %v", goruntime.GOOS, list), nil
	}

	return false, fmt.Sprintf("host.os %q is not in %v", goruntime.GOOS, list), nil
}

func matchArch(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	list, err := evalStringList(evaluator, expression, scope, meta, "arch")
	if err != nil {
		return false, "", err
	}

	if slices.Contains(list, goruntime.GOARCH) {
		return true, fmt.Sprintf("host.arch %q is in %v", goruntime.GOARCH, list), nil
	}

	return false, fmt.Sprintf("host.arch %q is not in %v", goruntime.GOARCH, list), nil
}

func matchEnvSet(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	names, err := evalStringList(evaluator, expression, scope, meta, "env_set")
	if err != nil {
		return false, "", err
	}

	missing := make([]string, 0, len(names))

	for _, name := range names {
		value, present := os.LookupEnv(name)
		if !present || value == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return true, fmt.Sprintf("env vars %v are set", names), nil
	}

	return false, fmt.Sprintf("env vars not set: %s", strings.Join(missing, ", ")), nil
}

func matchEnv(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	value, err := evaluator.Eval(expression, scope, meta)
	if err != nil {
		return false, "", fmt.Errorf("evaluate: %w", err)
	}

	if value.IsNull() {
		return false, "env map is null", nil
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return false, "", fmt.Errorf("env must be an object/map of string to string")
	}

	mismatches := make([]string, 0)

	for name, expectedVal := range value.AsValueMap() {
		if expectedVal.IsNull() || expectedVal.Type() != cty.String {
			return false, "", fmt.Errorf("env[%q] must be a string", name)
		}

		expected := expectedVal.AsString()

		actual, present := os.LookupEnv(name)
		if !present {
			mismatches = append(mismatches, fmt.Sprintf("%s=<unset>(want %q)", name, expected))

			continue
		}

		if actual != expected {
			mismatches = append(mismatches, fmt.Sprintf("%s=%q(want %q)", name, actual, expected))
		}
	}

	if len(mismatches) == 0 {
		return true, "env values match", nil
	}

	return false, fmt.Sprintf("env mismatch: %s", strings.Join(mismatches, ", ")), nil
}

func matchCondition(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta) (bool, string, error) {
	value, err := evaluator.Eval(expression, scope, meta)
	if err != nil {
		return false, "", fmt.Errorf("evaluate: %w", err)
	}

	if value.IsNull() {
		return false, "condition is null", nil
	}

	if value.Type() != cty.Bool {
		return false, "", fmt.Errorf("condition must evaluate to bool, got %s", value.Type().FriendlyName())
	}

	if value.True() {
		return true, "condition is true", nil
	}

	return false, "condition is false", nil
}

func evalStringList(evaluator *lang.Evaluator, expression model.Expression, scope lang.ScopeData, meta lang.GenerateMeta, attr string) ([]string, error) {
	value, err := evaluator.Eval(expression, scope, meta)
	if err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", attr, err)
	}

	if value.IsNull() {
		return nil, fmt.Errorf("%s must not be null", attr)
	}

	if !value.Type().IsListType() && !value.Type().IsTupleType() && !value.Type().IsSetType() {
		return nil, fmt.Errorf("%s must be a list of strings, got %s", attr, value.Type().FriendlyName())
	}

	items := make([]string, 0)

	for it := value.ElementIterator(); it.Next(); {
		_, element := it.Element()
		if element.IsNull() || element.Type() != cty.String {
			return nil, fmt.Errorf("%s entries must be strings", attr)
		}

		items = append(items, element.AsString())
	}

	return items, nil
}

// resolveSkipReason returns the explicit reason from the rule when
// provided, falling back to an aggregated auto-reason built from the
// per-attribute reasons emitted while matching.
func resolveSkipReason(evaluator *lang.Evaluator, rule model.SkipRule, scope lang.ScopeData, meta lang.GenerateMeta, autoReasons []string) (string, error) {
	if rule.Reason.Empty() {
		return buildAutoReason(rule.Kind, autoReasons), nil
	}

	value, err := evaluator.Eval(rule.Reason, scope, meta)
	if err != nil {
		return "", fmt.Errorf("reason: %w", err)
	}

	if value.IsNull() {
		return buildAutoReason(rule.Kind, autoReasons), nil
	}

	if value.Type() != cty.String {
		return "", errors.New("reason must evaluate to a string")
	}

	return value.AsString(), nil
}

func buildAutoReason(kind model.SkipKind, autoReasons []string) string {
	if len(autoReasons) == 0 {
		return string(kind) + " rule triggered"
	}

	return string(kind) + ": " + strings.Join(autoReasons, "; ")
}
