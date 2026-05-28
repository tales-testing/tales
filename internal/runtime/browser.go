package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// browserProviderType is the provider label that triggers browser step
// execution.
const browserProviderType = "browser"

// executeBrowserStep evaluates the browser step's expressions, dispatches
// to the browser provider with the prepared BrowserExecution payload, then
// runs the standard capture / result pipeline. The capture phase injects
// browser-specific helpers (text / attribute / browser.url / browser.title)
// into the EvalContext so .tales capture blocks can read the recorded
// post-step snapshot.
func (r *Runner) executeBrowserStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	start := time.Now()
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass, StartedAt: start}

	if step.Browser == nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: "browser step is missing browser block"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindVars, Path: failedVar, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	exec, evalErr := evaluateBrowserStep(evaluator, scope, scenarioName, step)
	if evalErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: evalErr.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindProvider, Message: fmt.Sprintf("unknown provider %q", step.Provider)}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	output, err := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Phase:    phase,
		Attempt:  attempt,
		Config:   config,
		Browser:  exec,
	})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindProvider, Message: err.Error()}

		if output != nil {
			stepReport.Response = diagnostic.FromCTYMap(output.Response)
			stepReport.Artifacts = artifactsFromOutput(output)
			stepReport.Actions = actionsFromOutput(output)
		}

		stepReport.Duration = time.Since(start)

		return stepReport
	}

	stepReport.Request = browserRequestSummary(exec)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)
	stepReport.Artifacts = artifactsFromOutput(output)
	stepReport.Actions = actionsFromOutput(output)

	scope.Request = output.Request
	scope.Response = output.Response

	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	if len(step.Capture) > 0 {
		extras, extraVars := browserCaptureScope(providerImpl, scenarioName, step.Name)

		for key, captureExpr := range step.Capture {
			captureVal, err := evaluator.EvalWithExtras(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key}, extras, extraVars)
			if err != nil {
				stepReport.Status = report.StatusFail
				stepReport.Failure = &report.ErrorDetail{Kind: kindCapture, Path: key, Message: err.Error()}
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

// evaluateBrowserStep lowers a browser step's HCL expressions into the
// concrete payload consumed by the browser provider.
func evaluateBrowserStep(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.BrowserExecution, error) {
	exec := &provider.BrowserExecution{}

	if !step.Browser.Target.Empty() {
		target, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "browser.target", step.Browser.Target)
		if err != nil {
			return nil, err
		}

		exec.TargetName = target
	}

	actions, err := evalBrowserActions(evaluator, scope, scenarioName, step)
	if err != nil {
		return nil, err
	}

	exec.Actions = actions

	expect, err := evalBrowserExpect(evaluator, scope, scenarioName, step)
	if err != nil {
		return nil, err
	}

	exec.Expect = expect

	return exec, nil
}

//nolint:gocyclo // Each action kind sets a small subset of fields; splitting per-kind helpers would obscure the dispatch.
func evalBrowserActions(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) ([]provider.BrowserActionExec, error) {
	out := make([]provider.BrowserActionExec, 0, len(step.Browser.Actions))

	for i, action := range step.Browser.Actions {
		exec := provider.BrowserActionExec{Kind: action.Kind, File: action.File, Line: action.Line}

		//nolint:exhaustive // The default branch handles every selector-only action; only kinds with bespoke fields appear here.
		switch action.Kind {
		case model.BrowserActionGoto:
			url, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].url", i), action.URL)
			if err != nil {
				return nil, err
			}

			exec.URL = url
		case model.BrowserActionPress:
			key, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].key", i), action.Key)
			if err != nil {
				return nil, err
			}

			exec.Key = key

			if !action.Selector.Empty() {
				sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].selector", i), action.Selector)
				if err != nil {
					return nil, err
				}

				exec.Selector = sel
			}
		case model.BrowserActionScroll:
			if !action.Selector.Empty() {
				sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].selector", i), action.Selector)
				if err != nil {
					return nil, err
				}

				exec.Selector = sel
			} else {
				x, err := evalIntAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].x", i), action.X)
				if err != nil {
					return nil, err
				}

				y, err := evalIntAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].y", i), action.Y)
				if err != nil {
					return nil, err
				}

				exec.X = x
				exec.Y = y
			}
		case model.BrowserActionReload, model.BrowserActionBack, model.BrowserActionForward:
			// no-arg actions
		default:
			sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].selector", i), action.Selector)
			if err != nil {
				return nil, err
			}

			exec.Selector = sel

			if !action.Value.Empty() {
				val, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].value", i), action.Value)
				if err != nil {
					return nil, err
				}

				exec.Value = val
			}
		}

		if !action.Secure.Empty() {
			secure, err := evalBoolAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].secure", i), action.Secure)
			if err != nil {
				return nil, err
			}

			exec.Secure = secure
		}

		if !action.Timeout.Empty() {
			timeout, err := evalDurationAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].timeout", i), action.Timeout)
			if err != nil {
				return nil, err
			}

			exec.Timeout = timeout
		}

		if !action.Interval.Empty() {
			interval, err := evalDurationAttr(evaluator, scope, scenarioName, step.Name, fmt.Sprintf("browser.actions[%d].interval", i), action.Interval)
			if err != nil {
				return nil, err
			}

			exec.Interval = interval
		}

		out = append(out, exec)
	}

	return out, nil
}

//nolint:gocyclo // One loop per expectation kind keeps the lowering flat and parallels the model surface.
func evalBrowserExpect(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (provider.BrowserExpectExec, error) {
	out := provider.BrowserExpectExec{}

	for i, v := range step.Browser.Expect.Visible {
		exec, err := evalBrowserVisibility(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.visible[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Visible = append(out.Visible, exec)
	}

	for i, v := range step.Browser.Expect.NotVisible {
		exec, err := evalBrowserVisibility(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.not_visible[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.NotVisible = append(out.NotVisible, exec)
	}

	for i, v := range step.Browser.Expect.Text {
		exec, err := evalBrowserValueExpectation(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.text[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Text = append(out.Text, exec)
	}

	for i, v := range step.Browser.Expect.Value {
		exec, err := evalBrowserValueExpectation(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.value[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Value = append(out.Value, exec)
	}

	for i, v := range step.Browser.Expect.Enabled {
		exec, err := evalBrowserStateExpectation(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.enabled[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Enabled = append(out.Enabled, exec)
	}

	for i, v := range step.Browser.Expect.Disabled {
		exec, err := evalBrowserStateExpectation(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.disabled[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Disabled = append(out.Disabled, exec)
	}

	for i, v := range step.Browser.Expect.Attribute {
		exec, err := evalBrowserAttribute(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.attribute[%d]", i), v)
		if err != nil {
			return out, err
		}

		out.Attribute = append(out.Attribute, exec)
	}

	for i, v := range step.Browser.Expect.URL {
		exec, err := evalBrowserURLOrTitle(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.url[%d]", i), v.Expected, v.Timeout, v.Interval)
		if err != nil {
			return out, err
		}

		out.URL = append(out.URL, provider.BrowserURLExpectationExec(exec))
	}

	for i, v := range step.Browser.Expect.Title {
		exec, err := evalBrowserURLOrTitle(evaluator, scope, scenarioName, step, fmt.Sprintf("browser.expect.title[%d]", i), v.Expected, v.Timeout, v.Interval)
		if err != nil {
			return out, err
		}

		out.Title = append(out.Title, provider.BrowserTitleExpectationExec(exec))
	}

	for _, v := range step.Browser.Expect.WebPerf {
		expected, err := evaluator.Eval(v.Expected, scope, lang.GenerateMeta{
			Scenario: scenarioName,
			Step:     step.Name,
			ExprPath: fmt.Sprintf("browser.expect.web_perf.%s", v.Metric),
		})
		if err != nil {
			return out, fmt.Errorf("browser.expect.web_perf.%s: %w", v.Metric, err)
		}

		out.WebPerf = append(out.WebPerf, provider.BrowserWebPerfExpectationExec{
			Metric:   v.Metric,
			Expected: expected,
		})
	}

	return out, nil
}

func evalBrowserVisibility(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, v model.BrowserVisibility) (provider.BrowserVisibilityExec, error) {
	exec := provider.BrowserVisibilityExec{}

	sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, path+".selector", v.Selector)
	if err != nil {
		return exec, err
	}

	exec.Selector = sel

	if !v.Timeout.Empty() {
		exec.Timeout, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".timeout", v.Timeout)
		if err != nil {
			return exec, err
		}
	}

	if !v.Interval.Empty() {
		exec.Interval, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".interval", v.Interval)
		if err != nil {
			return exec, err
		}
	}

	return exec, nil
}

func evalBrowserValueExpectation(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, v model.BrowserValueExpectation) (provider.BrowserValueExpectationExec, error) {
	exec := provider.BrowserValueExpectationExec{}

	sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, path+".selector", v.Selector)
	if err != nil {
		return exec, err
	}

	exec.Selector = sel

	expected, err := evaluator.Eval(v.Expected, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path + ".value"})
	if err != nil {
		return exec, fmt.Errorf("%s.value: %w", path, err)
	}

	exec.Expected = expected

	if !v.Timeout.Empty() {
		exec.Timeout, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".timeout", v.Timeout)
		if err != nil {
			return exec, err
		}
	}

	if !v.Interval.Empty() {
		exec.Interval, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".interval", v.Interval)
		if err != nil {
			return exec, err
		}
	}

	return exec, nil
}

func evalBrowserStateExpectation(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, v model.BrowserStateExpectation) (provider.BrowserStateExpectationExec, error) {
	exec := provider.BrowserStateExpectationExec{}

	sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, path+".selector", v.Selector)
	if err != nil {
		return exec, err
	}

	exec.Selector = sel

	if !v.Timeout.Empty() {
		exec.Timeout, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".timeout", v.Timeout)
		if err != nil {
			return exec, err
		}
	}

	if !v.Interval.Empty() {
		exec.Interval, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".interval", v.Interval)
		if err != nil {
			return exec, err
		}
	}

	return exec, nil
}

func evalBrowserAttribute(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, v model.BrowserAttributeExpectation) (provider.BrowserAttributeExpectationExec, error) {
	exec := provider.BrowserAttributeExpectationExec{}

	sel, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, path+".selector", v.Selector)
	if err != nil {
		return exec, err
	}

	exec.Selector = sel

	name, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, path+".name", v.Name)
	if err != nil {
		return exec, err
	}

	exec.Name = name

	expected, err := evaluator.Eval(v.Expected, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path + ".value"})
	if err != nil {
		return exec, fmt.Errorf("%s.value: %w", path, err)
	}

	exec.Expected = expected

	if !v.Timeout.Empty() {
		exec.Timeout, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".timeout", v.Timeout)
		if err != nil {
			return exec, err
		}
	}

	if !v.Interval.Empty() {
		exec.Interval, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".interval", v.Interval)
		if err != nil {
			return exec, err
		}
	}

	return exec, nil
}

// browserURLOrTitleExec is the common shape for URL / title expectation exec.
// The Browser*ExpectationExec types in provider have the same fields, but
// distinct named types so the provider switches on them cleanly.
type browserURLOrTitleExec struct {
	Expected cty.Value
	Timeout  time.Duration
	Interval time.Duration
}

func evalBrowserURLOrTitle(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expected, timeout, interval model.Expression) (browserURLOrTitleExec, error) {
	exec := browserURLOrTitleExec{}

	value, err := evaluator.Eval(expected, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path + ".value"})
	if err != nil {
		return exec, fmt.Errorf("%s.value: %w", path, err)
	}

	exec.Expected = value

	if !timeout.Empty() {
		exec.Timeout, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".timeout", timeout)
		if err != nil {
			return exec, err
		}
	}

	if !interval.Empty() {
		exec.Interval, err = evalDurationAttr(evaluator, scope, scenarioName, step.Name, path+".interval", interval)
		if err != nil {
			return exec, err
		}
	}

	return exec, nil
}

func evalIntAttr(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (int, error) {
	if expression.Empty() {
		return 0, fmt.Errorf("%s: required", exprPath)
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() || value.Type() != cty.Number {
		return 0, fmt.Errorf("%s: must be a number", exprPath)
	}

	n, acc := value.AsBigFloat().Int64()
	if acc != 0 {
		return 0, fmt.Errorf("%s: must be an integer", exprPath)
	}

	return int(n), nil
}

func evalBoolAttr(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (bool, error) {
	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return false, fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() {
		return false, nil
	}

	if value.Type() != cty.Bool {
		return false, fmt.Errorf("%s: must be a boolean", exprPath)
	}

	return value.True(), nil
}

// browserRequestSummary builds the structured summary attached to the step
// report under stepReport.Request so consumers (console, JSONL, visual) can
// render what was executed.
func browserRequestSummary(exec *provider.BrowserExecution) map[string]any {
	if exec == nil {
		return nil
	}

	summary := map[string]any{
		keyTarget: exec.TargetName,
	}

	if len(exec.Actions) > 0 {
		actions := make([]map[string]any, 0, len(exec.Actions))

		for _, action := range exec.Actions {
			actions = append(actions, browserActionSummary(action))
		}

		summary["actions"] = actions
	}

	if exec.Expect.HasAny() {
		summary["expect"] = browserExpectSummary(exec.Expect)
	}

	return summary
}

func browserActionSummary(action provider.BrowserActionExec) map[string]any {
	entry := map[string]any{attrKind: string(action.Kind)}

	if action.Selector != "" {
		entry[keySelector] = action.Selector
	}

	if action.URL != "" {
		entry[keyURL] = action.URL
	}

	if action.Key != "" {
		entry["key"] = action.Key
	}

	if action.Timeout > 0 {
		entry["timeout"] = action.Timeout.String()
	}

	if action.Interval > 0 {
		entry["interval"] = action.Interval.String()
	}

	if action.Kind == model.BrowserActionScroll && action.Selector == "" {
		entry["x"] = action.X
		entry["y"] = action.Y
	}

	if action.Value != "" {
		if action.Secure {
			entry[keyValue] = keyMasked
		} else {
			entry[keyValue] = action.Value
		}
	}

	return entry
}

func browserExpectSummary(expect provider.BrowserExpectExec) map[string]any {
	out := map[string]any{}

	if len(expect.Visible) > 0 {
		out["visible"] = browserSelectorList(expect.Visible)
	}

	if len(expect.NotVisible) > 0 {
		out["not_visible"] = browserSelectorList(expect.NotVisible)
	}

	if len(expect.Text) > 0 {
		out[keyText] = len(expect.Text)
	}

	if len(expect.Value) > 0 {
		out[keyValue] = len(expect.Value)
	}

	if len(expect.Attribute) > 0 {
		out["attribute"] = len(expect.Attribute)
	}

	if len(expect.URL) > 0 {
		out[keyURL] = len(expect.URL)
	}

	if len(expect.Title) > 0 {
		out[keyTitle] = len(expect.Title)
	}

	return out
}

func browserSelectorList(in []provider.BrowserVisibilityExec) []string {
	out := make([]string, 0, len(in))

	for _, v := range in {
		out = append(out, v.Selector)
	}

	return out
}
