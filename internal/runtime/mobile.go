package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/hyperxlab/tales/internal/lang"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	mobileprovider "github.com/hyperxlab/tales/internal/provider/mobile"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

const mobileProviderType = "mobile"

func (r *Runner) executeMobileStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusPass}
	start := time.Now()

	if step.Mobile == nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: "mobile step is missing mobile block"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	exec, evalErr := evaluateMobileStep(evaluator, scope, scenarioName, step)
	if evalErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: evalErr.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: fmt.Sprintf("unknown provider %q", step.Provider)}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	output, err := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Config:   config,
		Mobile:   exec,
	})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: err.Error()}

		if output != nil {
			stepReport.Response = diagnostic.FromCTYMap(output.Response)
			stepReport.Artifacts = artifactsFromOutput(output)
		}

		stepReport.Duration = time.Since(start)

		return stepReport
	}

	stepReport.Request = mobileRequestSummary(exec)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)
	stepReport.Artifacts = artifactsFromOutput(output)

	scope.Response = output.Response

	resultValue := map[string]cty.Value{
		"request":  cty.ObjectVal(output.Request),
		"response": cty.ObjectVal(output.Response),
	}

	if len(step.Capture) > 0 {
		extras := mobileCaptureFunctions(providerImpl, scenarioName, step.Name)

		for key, captureExpr := range step.Capture {
			captureVal, err := evaluator.EvalWithExtras(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key}, extras)
			if err != nil {
				stepReport.Status = report.StatusFail
				stepReport.Failure = &report.ErrorDetail{Kind: "capture", Path: key, Message: err.Error()}
				stepReport.Duration = time.Since(start)

				return stepReport
			}

			resultValue[key] = captureVal
		}
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	stepReport.Duration = time.Since(start)

	return stepReport
}

func evaluateMobileStep(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.MobileExecution, error) {
	exec := &provider.MobileExecution{}

	platform, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "mobile.platform", step.Mobile.Platform)
	if err != nil {
		return nil, err
	}

	exec.Platform = platform

	target, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "mobile.target", step.Mobile.Target)
	if err != nil {
		return nil, err
	}

	exec.TargetName = target

	if step.Mobile.Launch != nil {
		launch, err := evalMobileLaunch(evaluator, scope, scenarioName, step, step.Mobile.Launch)
		if err != nil {
			return nil, err
		}

		exec.Launch = launch
	}

	if step.Mobile.Terminate != nil {
		exec.Terminate = &provider.MobileTerminateExec{}
	}

	actions, err := evalMobileActions(evaluator, scope, scenarioName, step, step.Mobile.Actions)
	if err != nil {
		return nil, err
	}

	exec.Actions = actions

	expect, err := evalMobileExpect(evaluator, scope, scenarioName, step, step.Mobile.Expect)
	if err != nil {
		return nil, err
	}

	exec.Expect = expect

	return exec, nil
}

func evalMobileLaunch(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, launch *model.MobileLaunch) (*provider.MobileLaunchExec, error) {
	out := &provider.MobileLaunchExec{}

	if launch.ClearState.Empty() {
		return out, nil
	}

	value, err := evaluator.Eval(launch.ClearState, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "mobile.launch.clear_state"})
	if err != nil {
		return nil, fmt.Errorf("mobile.launch.clear_state: %w", err)
	}

	if value.IsNull() {
		return out, nil
	}

	if value.Type() != cty.Bool {
		return nil, fmt.Errorf("mobile.launch.clear_state: must be a boolean")
	}

	out.ClearState = value.True()

	return out, nil
}

func evalMobileActions(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, actions []model.MobileAction) ([]provider.MobileActionExec, error) {
	out := make([]provider.MobileActionExec, 0, len(actions))

	for i, action := range actions {
		exec := provider.MobileActionExec{Kind: action.Kind, File: action.File, Line: action.Line}

		id, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("mobile.actions[%d].id", i), action.ID)
		if err != nil {
			return nil, err
		}

		exec.ID = id

		if !action.Value.Empty() {
			value, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("mobile.actions[%d].value", i), action.Value)
			if err != nil {
				return nil, err
			}

			exec.Value = value
		}

		if !action.Secure.Empty() {
			secure, err := evaluator.Eval(action.Secure, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: fmt.Sprintf("mobile.actions[%d].secure", i)})
			if err != nil {
				return nil, fmt.Errorf("mobile.actions[%d].secure: %w", i, err)
			}

			if !secure.IsNull() {
				if secure.Type() != cty.Bool {
					return nil, fmt.Errorf("mobile.actions[%d].secure: must be a boolean", i)
				}

				exec.Secure = secure.True()
			}
		}

		out = append(out, exec)
	}

	return out, nil
}

func evalMobileExpect(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, expect model.MobileExpect) (provider.MobileExpectExec, error) {
	out := provider.MobileExpectExec{}

	for i, v := range expect.Visible {
		exec, err := evalMobileVisibility(evaluator, scope, scenarioName, step, fmt.Sprintf("mobile.expect.visible[%d]", i), v)
		if err != nil {
			return provider.MobileExpectExec{}, err
		}

		out.Visible = append(out.Visible, exec)
	}

	for i, v := range expect.NotVisible {
		exec, err := evalMobileVisibility(evaluator, scope, scenarioName, step, fmt.Sprintf("mobile.expect.not_visible[%d]", i), v)
		if err != nil {
			return provider.MobileExpectExec{}, err
		}

		out.NotVisible = append(out.NotVisible, exec)
	}

	return out, nil
}

func evalMobileVisibility(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, exprPath string, v model.MobileVisibility) (provider.MobileVisibilityExec, error) {
	id, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, exprPath+".id", v.ID)
	if err != nil {
		return provider.MobileVisibilityExec{}, err
	}

	exec := provider.MobileVisibilityExec{ID: id}

	if v.Timeout.Empty() {
		return exec, nil
	}

	value, err := evaluator.Eval(v.Timeout, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: exprPath + ".timeout"})
	if err != nil {
		return provider.MobileVisibilityExec{}, fmt.Errorf("%s.timeout: %w", exprPath, err)
	}

	if value.IsNull() {
		return exec, nil
	}

	duration, err := toDuration(value)
	if err != nil {
		return provider.MobileVisibilityExec{}, fmt.Errorf("%s.timeout: %w", exprPath, err)
	}

	exec.Timeout = duration

	return exec, nil
}

func evalStringAttr(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (string, error) {
	if expression.Empty() {
		return "", fmt.Errorf("%s: required", exprPath)
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return "", fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() {
		return "", fmt.Errorf("%s: must not be null", exprPath)
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("%s: must be a string", exprPath)
	}

	return value.AsString(), nil
}

func mobileRequestSummary(exec *provider.MobileExecution) map[string]any {
	if exec == nil {
		return nil
	}

	summary := map[string]any{
		"platform": exec.Platform,
		"target":   exec.TargetName,
	}

	if exec.Launch != nil {
		summary["launch"] = map[string]any{"clear_state": exec.Launch.ClearState}
	}

	if exec.Terminate != nil {
		summary["terminate"] = true
	}

	if len(exec.Actions) > 0 {
		actions := make([]map[string]any, 0, len(exec.Actions))

		for _, action := range exec.Actions {
			entry := map[string]any{"kind": string(action.Kind), "id": action.ID}
			if action.Value != "" {
				if action.Secure {
					entry["value"] = "***"
				} else {
					entry["value"] = action.Value
				}
			}

			actions = append(actions, entry)
		}

		summary["actions"] = actions
	}

	if len(exec.Expect.Visible) > 0 || len(exec.Expect.NotVisible) > 0 {
		summary["expect"] = expectSummary(exec.Expect)
	}

	return summary
}

func expectSummary(expect provider.MobileExpectExec) map[string]any {
	summary := map[string]any{}

	if len(expect.Visible) > 0 {
		visibles := make([]map[string]any, 0, len(expect.Visible))

		for _, v := range expect.Visible {
			visibles = append(visibles, map[string]any{"id": v.ID, "timeout": v.Timeout.String()})
		}

		summary["visible"] = visibles
	}

	if len(expect.NotVisible) > 0 {
		notVisibles := make([]map[string]any, 0, len(expect.NotVisible))

		for _, v := range expect.NotVisible {
			notVisibles = append(notVisibles, map[string]any{"id": v.ID, "timeout": v.Timeout.String()})
		}

		summary["not_visible"] = notVisibles
	}

	return summary
}

func artifactsFromOutput(output *provider.Output) []report.Artifact {
	if output == nil {
		return nil
	}

	value, ok := output.Response["artifacts"]
	if !ok || value.IsNull() {
		return nil
	}

	if !value.Type().IsListType() && !value.Type().IsTupleType() {
		return nil
	}

	artifacts := make([]report.Artifact, 0, value.LengthInt())

	for it := value.ElementIterator(); it.Next(); {
		_, item := it.Element()
		if !item.Type().IsObjectType() {
			continue
		}

		a := report.Artifact{}
		if item.Type().HasAttribute("type") {
			a.Type = item.GetAttr("type").AsString()
		}

		if item.Type().HasAttribute("path") {
			a.Path = item.GetAttr("path").AsString()
		}

		artifacts = append(artifacts, a)
	}

	return artifacts
}

func mobileCaptureFunctions(providerImpl provider.Provider, scenarioName, stepName string) map[string]function.Function {
	mobile, ok := providerImpl.(*mobileprovider.Provider)
	if !ok {
		return nil
	}

	hierarchy := mobile.LastHierarchy(scenarioName, stepName)

	return map[string]function.Function{
		"value": valueFunction(hierarchy),
		"text":  textFunction(hierarchy),
	}
}

func valueFunction(hierarchy *tree.ViewNode) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "id", Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			node, found, err := tree.FindByID(hierarchy, args[0].AsString())
			if err != nil {
				return cty.NilVal, fmt.Errorf("value(%q): %w", args[0].AsString(), err)
			}

			if !found {
				return cty.NilVal, fmt.Errorf("value(%q): element not found", args[0].AsString())
			}

			return cty.StringVal(tree.Value(node)), nil
		},
	})
}

func textFunction(hierarchy *tree.ViewNode) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "id", Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			node, found, err := tree.FindByID(hierarchy, args[0].AsString())
			if err != nil {
				return cty.NilVal, fmt.Errorf("text(%q): %w", args[0].AsString(), err)
			}

			if !found {
				return cty.NilVal, fmt.Errorf("text(%q): element not found", args[0].AsString())
			}

			return cty.StringVal(tree.Text(node)), nil
		},
	})
}
