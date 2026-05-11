package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/assertion"
	"github.com/hyperxlab/tales/internal/dag"
	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/hyperxlab/tales/internal/lang"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// Options controls runtime execution.
type Options struct {
	Seed     int64
	Parallel int
	Tags     []string
	Scenario string
}

// Runner executes a suite.
type Runner struct {
	providers *provider.Registry
}

// NewRunner creates runner.
func NewRunner(registry *provider.Registry) *Runner {
	return &Runner{providers: registry}
}

// Run executes the whole suite with scenario-level parallelism.
func (r *Runner) Run(ctx context.Context, suite *model.Suite, opts Options) (*report.SuiteResult, error) {
	if opts.Parallel <= 0 {
		opts.Parallel = 1
	}

	configValues, err := evalConfig(suite)
	if err != nil {
		return nil, err
	}

	scenarios := filterScenarios(suite.Scenarios, opts.Tags, opts.Scenario)
	result := &report.SuiteResult{Seed: opts.Seed, StartedAt: time.Now(), Scenarios: make([]*report.ScenarioResult, len(scenarios))}

	sem := make(chan struct{}, opts.Parallel)

	var (
		wg         sync.WaitGroup
		firstErr   error
		firstErrMu sync.Mutex
	)

	for i, scenario := range scenarios {
		wg.Add(1)

		index := i
		sc := scenario

		go func() {
			defer wg.Done()

			sem <- struct{}{}

			defer func() { <-sem }()

			scenarioResult, runErr := r.runScenario(ctx, suite, sc, configValues, opts.Seed)
			result.Scenarios[index] = scenarioResult

			if runErr != nil {
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = runErr
				}
				firstErrMu.Unlock()
			}
		}()
	}

	wg.Wait()

	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(result.StartedAt)

	if firstErr != nil {
		return result, firstErr
	}

	return result, nil
}

func (r *Runner) runScenario(ctx context.Context, suite *model.Suite, scenario *model.Scenario, config map[string]cty.Value, seed int64) (*report.ScenarioResult, error) {
	sResult := &report.ScenarioResult{File: scenario.File, Name: scenario.Name, Tags: scenario.Tags, Status: report.StatusPass}
	start := time.Now()

	stepNames := make([]string, 0, len(scenario.Steps)+len(scenario.Teardown))
	for _, step := range scenario.Steps {
		stepNames = append(stepNames, step.Name)
	}

	for _, step := range scenario.Teardown {
		stepNames = append(stepNames, step.Name)
	}

	state := NewScenarioState(stepNames)

	var evaluator *lang.Evaluator

	evaluator = lang.NewEvaluator(func(name string, meta lang.GenerateMeta) (cty.Value, error) {
		gen, ok := suite.Generators[name]
		if !ok {
			return cty.NilVal, fmt.Errorf("unknown generator %q", name)
		}

		params, err := evalGeneratorParams(evaluator, gen, config)
		if err != nil {
			return cty.NilVal, err
		}

		return runGenerator(gen.Type, params, newGeneratorRandom(seed, scenario.Name, meta.Step, name, meta.ExprPath))
	})

	layers, orderErr := buildLayers(scenario.Steps)
	if orderErr != nil {
		sResult.Status = report.StatusFail
		sResult.Failure = &report.ErrorDetail{Kind: "dag", Message: orderErr.Error()}
		sResult.Duration = time.Since(start)

		return sResult, orderErr
	}

	failedSteps := map[string]struct{}{}
	failedStepsMu := sync.RWMutex{}
	stepByName := map[string]*model.Step{}
	depsByStep := map[string]map[string]struct{}{}

	for _, step := range scenario.Steps {
		stepByName[step.Name] = step

		deps, err := lang.StepDependencies(step)
		if err != nil {
			return nil, fmt.Errorf("resolve dependencies for step %q: %w", step.Name, err)
		}

		depsByStep[step.Name] = deps
	}

	for _, layer := range layers {
		var wg sync.WaitGroup

		mu := sync.Mutex{}

		for _, stepName := range layer {
			step := stepByName[stepName]

			wg.Add(1)

			go func(step *model.Step) {
				defer wg.Done()

				dependencyFailed := false

				for dep := range depsByStep[step.Name] {
					failedStepsMu.RLock()

					_, failed := failedSteps[dep]

					failedStepsMu.RUnlock()

					if failed {
						dependencyFailed = true

						break
					}
				}

				if dependencyFailed {
					mu.Lock()

					sResult.Steps = append(sResult.Steps, &report.StepResult{File: step.File, Scenario: scenario.Name, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusSkip})

					mu.Unlock()

					failedStepsMu.Lock()
					failedSteps[step.Name] = struct{}{}
					failedStepsMu.Unlock()

					return
				}

				stepResult := r.executeStep(ctx, evaluator, suite, scenario.Name, config, state, nil, step)

				mu.Lock()

				sResult.Steps = append(sResult.Steps, stepResult)
				if stepResult.Status == report.StatusFail {
					failedStepsMu.Lock()
					failedSteps[step.Name] = struct{}{}
					failedStepsMu.Unlock()

					if sResult.Failure == nil {
						sResult.Failure = stepResult.Failure
					}

					sResult.Status = report.StatusFail
				}
				mu.Unlock()
			}(step)
		}

		wg.Wait()
	}

	for _, step := range scenario.Teardown {
		stepResult := r.executeTeardownStep(ctx, evaluator, suite, scenario.Name, config, state, nil, step)

		sResult.Teardown = append(sResult.Teardown, stepResult)
		if stepResult.Status == report.StatusFail {
			sResult.TeardownFailures = append(sResult.TeardownFailures, stepResult.Failure)
			if sResult.Status == report.StatusPass {
				sResult.Status = report.StatusFail
				sResult.Failure = stepResult.Failure
			}
		}
	}

	sResult.Duration = time.Since(start)
	sort.Slice(sResult.Steps, func(i, j int) bool { return sResult.Steps[i].Name < sResult.Steps[j].Name })

	return sResult, nil
}

func (r *Runner) executeStep(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) *report.StepResult {
	retry := retryOptions(step)
	start := time.Now()

	var lastResult *report.StepResult

	for attempt := 1; attempt <= retry.Attempts; attempt++ {
		attemptResult := r.executeStepAttempt(ctx, evaluator, suite, scenarioName, config, state, input, step)
		attemptResult.Attempts = attempt
		lastResult = attemptResult

		if attemptResult.Status == report.StatusPass || attempt == retry.Attempts {
			attemptResult.Duration = time.Since(start)

			return attemptResult
		}

		if retry.Interval > 0 {
			if !sleepWithContext(ctx, retry.Interval) {
				attemptResult.Status = report.StatusFail
				attemptResult.Failure = &report.ErrorDetail{Kind: "runtime", Message: "step retry interrupted by context cancellation"}
				attemptResult.Duration = time.Since(start)

				return attemptResult
			}
		}
	}

	if lastResult != nil {
		lastResult.Duration = time.Since(start)

		return lastResult
	}

	return &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusFail, Attempts: retry.Attempts, Duration: time.Since(start), Failure: &report.ErrorDetail{Kind: "runtime", Message: "step was not executed"}}
}

func (r *Runner) executeStepAttempt(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusPass}
	start := time.Now()

	if step.Provider == "keyword" {
		return r.executeKeywordStep(ctx, evaluator, suite, scenarioName, config, state, input, step, start, stepReport)
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	requestValues, timeout, err := evaluateRequest(evaluator, scope, scenarioName, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: err.Error()}
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

	output, err := providerImpl.Execute(ctx, provider.Input{Scenario: scenarioName, Step: step, Config: config, Request: requestValues, Timeout: timeout})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	stepReport.StatusCode = output.StatusCode
	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)

	scope.Request = output.Request

	scope.Response = output.Response
	if step.Expect != nil {
		if err := evaluateExpect(evaluator, scope, scenarioName, step, output); err != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = toErrorDetail(err)
			stepReport.Duration = time.Since(start)

			return stepReport
		}
	}

	resultValue := map[string]cty.Value{
		"request":  cty.ObjectVal(output.Request),
		"response": cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = &report.ErrorDetail{Kind: "capture", Path: key, Message: err.Error()}
			stepReport.Duration = time.Since(start)

			return stepReport
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	stepReport.Duration = time.Since(start)

	return stepReport
}

func retryOptions(step *model.Step) model.Retry {
	if step.Retry == nil {
		return model.Retry{Attempts: 1}
	}

	retry := *step.Retry
	if retry.Attempts < 1 {
		retry.Attempts = 1
	}

	return retry
}

func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runner) executeTeardownStep(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) *report.StepResult {
	if !evalWhen(step.When, evaluator, lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}, scenarioName, step.Name) {
		return &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "teardown", Status: report.StatusSkip}
	}

	result := r.executeStep(ctx, evaluator, suite, scenarioName, config, state, input, step)
	result.Phase = "teardown"

	return result
}

func buildLayers(steps []*model.Step) ([][]string, error) {
	return buildLayersWithExternalDeps(steps, nil)
}

func buildLayersWithExternalDeps(steps []*model.Step, externalDeps map[string]struct{}) ([][]string, error) {
	g := dag.NewGraph()
	knownSteps := map[string]struct{}{}

	for _, step := range steps {
		if err := g.AddNode(step.Name); err != nil {
			return nil, fmt.Errorf("add node %q: %w", step.Name, err)
		}

		knownSteps[step.Name] = struct{}{}
	}

	for _, step := range steps {
		deps, err := lang.StepDependencies(step)
		if err != nil {
			return nil, fmt.Errorf("resolve dependencies for step %q: %w", step.Name, err)
		}

		for dep := range deps {
			if _, exists := knownSteps[dep]; !exists {
				if _, external := externalDeps[dep]; external {
					continue
				}

				return nil, fmt.Errorf("step %q references unknown dependency %q", step.Name, dep)
			}

			if err := g.AddEdge(dep, step.Name); err != nil {
				return nil, fmt.Errorf("step %q: %w", step.Name, err)
			}
		}
	}

	layers, err := dag.TopologicalLayers(g)
	if err != nil {
		return nil, fmt.Errorf("topological sort failed: %w", err)
	}

	return layers, nil
}

func evaluateRequest(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (map[string]cty.Value, time.Duration, error) {
	values := map[string]cty.Value{}
	if step.Request == nil {
		return values, 0, fmt.Errorf("step %q is missing request block", step.Name)
	}

	setExpr := func(name string, expression model.Expression) error {
		if expression.Empty() {
			return nil
		}

		value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "request." + name})
		if err != nil {
			return fmt.Errorf("evaluate %s: %w", name, err)
		}

		values[name] = value

		return nil
	}

	if err := setExpr("method", step.Request.Method); err != nil {
		return nil, 0, fmt.Errorf("request.method: %w", err)
	}

	if err := setExpr("url", step.Request.URL); err != nil {
		return nil, 0, fmt.Errorf("request.url: %w", err)
	}

	if err := setExpr("headers", step.Request.Headers); err != nil {
		return nil, 0, fmt.Errorf("request.headers: %w", err)
	}

	if err := setExpr("query", step.Request.Query); err != nil {
		return nil, 0, fmt.Errorf("request.query: %w", err)
	}

	if err := evaluateRequestBody(evaluator, scope, scenarioName, step, values); err != nil {
		return nil, 0, fmt.Errorf("request.body: %w", err)
	}

	if err := evaluateRequestAuth(evaluator, scope, scenarioName, step, values); err != nil {
		return nil, 0, fmt.Errorf("request.auth: %w", err)
	}

	timeout := time.Duration(0)

	if !step.Request.Timeout.Empty() {
		value, err := evaluator.Eval(step.Request.Timeout, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "request.timeout"})
		if err != nil {
			return nil, 0, fmt.Errorf("request.timeout: %w", err)
		}

		if value.IsNull() {
			return values, 0, nil
		}

		parsedTimeout, err := toDuration(value)
		if err != nil {
			return nil, 0, err
		}

		timeout = parsedTimeout
	}

	return values, timeout, nil
}

func evaluateRequestBody(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, values map[string]cty.Value) error {
	if step.Request.Body == nil {
		return nil
	}

	body := map[string]cty.Value{}

	setBodyExpr := func(name string, expression model.Expression) error {
		if expression.Empty() {
			return nil
		}

		value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "request.body." + name})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		body[name] = value

		return nil
	}

	if err := setBodyExpr("json", step.Request.Body.JSON); err != nil {
		return err
	}

	if err := setBodyExpr("form", step.Request.Body.Form); err != nil {
		return err
	}

	if err := setBodyExpr("raw", step.Request.Body.Raw); err != nil {
		return err
	}

	if len(body) > 0 {
		values["body"] = cty.ObjectVal(body)
	}

	return nil
}

func evaluateRequestAuth(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, values map[string]cty.Value) error {
	if step.Request.Auth == nil || step.Request.Auth.Basic == nil {
		return nil
	}

	usernameValue, err := evaluator.Eval(step.Request.Auth.Basic.Username, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "request.auth.basic.username"})
	if err != nil {
		return fmt.Errorf("basic.username: %w", err)
	}

	if err := validateBasicAuthString("username", usernameValue); err != nil {
		return err
	}

	passwordValue, err := evaluator.Eval(step.Request.Auth.Basic.Password, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "request.auth.basic.password"})
	if err != nil {
		return fmt.Errorf("basic.password: %w", err)
	}

	if err := validateBasicAuthString("password", passwordValue); err != nil {
		return err
	}

	values["auth"] = cty.ObjectVal(map[string]cty.Value{
		"basic": cty.ObjectVal(map[string]cty.Value{
			"username": usernameValue,
			"password": passwordValue,
		}),
	})

	return nil
}

func validateBasicAuthString(name string, value cty.Value) error {
	if !value.IsKnown() {
		return fmt.Errorf("basic.%s must be a known string", name)
	}

	if value.IsNull() {
		return fmt.Errorf("basic.%s must not be null", name)
	}

	if value.Type() != cty.String {
		return fmt.Errorf("basic.%s must be a string, got %s", name, value.Type().FriendlyName())
	}

	return nil
}

func evaluateExpect(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect == nil {
		return nil
	}

	if err := assertExpectedStatus(evaluator, scope, scenarioName, step, output); err != nil {
		return err
	}

	if err := assertExpectedHeaders(evaluator, scope, scenarioName, step, output); err != nil {
		return err
	}

	if err := assertExpectedJSON(evaluator, scope, scenarioName, step, output); err != nil {
		return err
	}

	if err := assertExpectedBody(evaluator, scope, scenarioName, step, output); err != nil {
		return err
	}

	return nil
}

func assertExpectedStatus(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect.Status.Empty() {
		return nil
	}

	expectedStatus, err := evaluator.Eval(step.Expect.Status, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.status"})
	if err != nil {
		return fmt.Errorf("expect.status: %w", err)
	}

	if expectedStatus.IsNull() {
		return nil
	}

	if err := assertion.MatchJSON(expectedStatus, cty.NumberIntVal(int64(output.StatusCode)), true, "status"); err != nil {
		return fmt.Errorf("assert status: %w", err)
	}

	return nil
}

func assertExpectedHeaders(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect.Headers.Empty() {
		return nil
	}

	expectedHeaders, err := evaluator.Eval(step.Expect.Headers, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.headers"})
	if err != nil {
		return fmt.Errorf("expect.headers: %w", err)
	}

	if expectedHeaders.IsNull() {
		return nil
	}

	if !expectedHeaders.Type().IsObjectType() && !expectedHeaders.Type().IsMapType() {
		return fmt.Errorf("expect.headers must be object")
	}

	actualHeaders := output.Response["headers"]

	for key, expected := range expectedHeaders.AsValueMap() {
		actual := cty.NullVal(cty.String)
		if actualHeaders.Type().HasAttribute(key) {
			actual = actualHeaders.GetAttr(key)
		}

		if err := assertion.MatchJSON(expected, actual, true, "headers."+key); err != nil {
			return fmt.Errorf("assert header %s: %w", key, err)
		}
	}

	return nil
}

func assertExpectedJSON(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect.JSON.Empty() {
		return nil
	}

	expectedJSON, err := evaluator.Eval(step.Expect.JSON, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.json"})
	if err != nil {
		return fmt.Errorf("expect.json: %w", err)
	}

	if expectedJSON.IsNull() {
		return nil
	}

	strict, err := evalStrictExpect(evaluator, scope, scenarioName, step)
	if err != nil {
		return err
	}

	responseJSON := output.Response["json"]
	if err := assertion.MatchJSON(expectedJSON, responseJSON, strict, "$"); err != nil {
		return fmt.Errorf("assert json body: %w", err)
	}

	return nil
}

func assertExpectedBody(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect.Body.Empty() {
		return nil
	}

	expectedBody, err := evaluator.Eval(step.Expect.Body, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.body"})
	if err != nil {
		return fmt.Errorf("expect.body: %w", err)
	}

	if expectedBody.IsNull() {
		return nil
	}

	responseBody := output.Response["body"]
	if err := assertion.MatchJSON(expectedBody, responseBody, true, "body"); err != nil {
		return fmt.Errorf("assert body: %w", err)
	}

	return nil
}

func evalStrictExpect(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (bool, error) {
	if step.Expect.Strict.Empty() {
		return false, nil
	}

	strictValue, err := evaluator.Eval(step.Expect.Strict, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.strict"})
	if err != nil {
		return false, fmt.Errorf("expect.strict: %w", err)
	}

	if strictValue.IsNull() {
		return false, nil
	}

	strict, err := toBool(strictValue)
	if err != nil {
		return false, fmt.Errorf("expect.strict value: %w", err)
	}

	return strict, nil
}

func toDuration(value cty.Value) (time.Duration, error) {
	if value.Type() == cty.Number {
		ms, _ := value.AsBigFloat().Int64()

		return time.Duration(ms) * time.Millisecond, nil
	}

	if value.Type() == cty.String {
		duration, err := time.ParseDuration(value.AsString())
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", value.AsString(), err)
		}

		return duration, nil
	}

	return 0, fmt.Errorf("timeout must be number(milliseconds) or duration string")
}

func toBool(value cty.Value) (bool, error) {
	if value.Type() != cty.Bool {
		return false, fmt.Errorf("expected boolean value")
	}

	return value.True(), nil
}

func evalConfig(suite *model.Suite) (map[string]cty.Value, error) {
	result := map[string]cty.Value{}
	evaluator := lang.NewEvaluator(func(name string, meta lang.GenerateMeta) (cty.Value, error) {
		_ = meta

		return cty.NilVal, fmt.Errorf("generate(%q) cannot be used in config", name)
	})

	keys := make([]string, 0, len(suite.ConfigExpr))
	for key := range suite.ConfigExpr {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		value, err := evaluator.Eval(suite.ConfigExpr[key], lang.ScopeData{Config: result, Result: map[string]cty.Value{}, Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}, lang.GenerateMeta{ExprPath: "config." + key})
		if err != nil {
			return nil, fmt.Errorf("config.%s: %w", key, err)
		}

		result[key] = value
	}

	return result, nil
}

func evalGeneratorParams(evaluator *lang.Evaluator, gen *model.Generator, config map[string]cty.Value) (map[string]cty.Value, error) {
	params := make(map[string]cty.Value, len(gen.Params))
	for key, expression := range gen.Params {
		value, err := evaluator.Eval(expression, lang.ScopeData{Config: config, Result: map[string]cty.Value{}, Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}, lang.GenerateMeta{ExprPath: "generator." + gen.Name + "." + key})
		if err != nil {
			return nil, fmt.Errorf("generator %s param %s: %w", gen.Name, key, err)
		}

		params[key] = value
	}

	return params, nil
}

func evalWhen(condition model.Expression, evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName string) bool {
	if condition.Empty() {
		return true
	}

	if call, ok := condition.Expr.(*hclsyntax.FunctionCallExpr); ok && call.Name == "can" && len(call.Args) == 1 {
		_, err := evaluator.EvalRaw(call.Args[0], scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: "teardown.when.can"})

		return err == nil
	}

	value, err := evaluator.Eval(condition, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: "teardown.when"})
	if err != nil {
		return false
	}

	boolValue, err := toBool(value)
	if err != nil {
		return false
	}

	return boolValue
}

func (r *Runner) executeKeywordStep(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, start time.Time, stepReport *report.StepResult) *report.StepResult {
	if step.Keyword == nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "keyword", Message: "keyword step is missing keyword call configuration"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	callName, callInputs, requestSummary, err := r.evaluateKeywordCall(evaluator, scenarioName, config, state, input, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "eval", Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	keyword, ok := suite.Keywords[callName]
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "keyword", Message: fmt.Sprintf("unknown keyword %q", callName)}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	if err := validateKeywordInputs(keyword, callInputs); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "keyword", Message: err.Error()}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	outerResults := state.GetResultMap()

	keywordState, stateErr := newKeywordState(outerResults, keyword.Steps)
	if stateErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "keyword", Message: stateErr.Error()}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	externalDeps := toKeySet(outerResults)

	if err := r.executeKeywordSteps(ctx, evaluator, suite, scenarioName, config, keywordState, callInputs, keyword, externalDeps); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = err
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	outputValues, err := evaluateKeywordOutputs(evaluator, scenarioName, step.Name, config, keywordState, callInputs, keyword)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "keyword", Message: err.Error()}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	responseSummary := diagnostic.FromCTYMap(map[string]cty.Value{"outputs": cty.ObjectVal(outputValues)})
	stepReport.Request = requestSummary
	stepReport.Response = responseSummary

	state.SetStepResult(step.Name, cty.ObjectVal(outputValues))

	stepReport.Duration = time.Since(start)

	return stepReport
}

func (r *Runner) evaluateKeywordCall(evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) (string, map[string]cty.Value, map[string]interface{}, error) {
	scope := lang.ScopeData{
		Config:   config,
		Result:   state.GetResultMap(),
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    ensureValueMap(input),
	}

	nameValue, err := evaluator.Eval(step.Keyword.Name, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "keyword.name"})
	if err != nil {
		return "", nil, nil, fmt.Errorf("keyword.name: %w", err)
	}

	if nameValue.Type() != cty.String {
		return "", nil, nil, fmt.Errorf("keyword.name must be a string")
	}

	keywordName := nameValue.AsString()
	inputValues := map[string]cty.Value{}

	if !step.Keyword.Inputs.Empty() {
		evaluatedInputs, evalErr := evaluator.Eval(step.Keyword.Inputs, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "keyword.inputs"})
		if evalErr != nil {
			return "", nil, nil, fmt.Errorf("keyword.inputs: %w", evalErr)
		}

		valueMap, mapErr := toValueMap(evaluatedInputs, "keyword.inputs")
		if mapErr != nil {
			return "", nil, nil, mapErr
		}

		inputValues = valueMap
	}

	requestSummary := diagnostic.FromCTYMap(map[string]cty.Value{
		"name":   cty.StringVal(keywordName),
		"inputs": cty.ObjectVal(inputValues),
	})

	return keywordName, inputValues, requestSummary, nil
}

func newKeywordState(outerResults map[string]cty.Value, steps []*model.Step) (*ScenarioState, error) {
	stepNames := make([]string, 0, len(outerResults)+len(steps))
	outerStepSet := toKeySet(outerResults)

	for name := range outerResults {
		stepNames = append(stepNames, name)
	}

	for _, step := range steps {
		if _, conflict := outerStepSet[step.Name]; conflict {
			return nil, fmt.Errorf("keyword step %q collides with existing scenario step name", step.Name)
		}

		stepNames = append(stepNames, step.Name)
	}

	keywordState := NewScenarioState(stepNames)

	for name, value := range outerResults {
		keywordState.SetStepResult(name, value)
	}

	return keywordState, nil
}

func (r *Runner) executeKeywordSteps(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, keywordState *ScenarioState, input map[string]cty.Value, keyword *model.Keyword, externalDeps map[string]struct{}) *report.ErrorDetail {
	layers, err := buildLayersWithExternalDeps(keyword.Steps, externalDeps)
	if err != nil {
		return &report.ErrorDetail{Kind: "keyword", Message: fmt.Sprintf("keyword %q graph error: %v", keyword.Name, err)}
	}

	stepByName := map[string]*model.Step{}
	depsByStep := map[string]map[string]struct{}{}
	failedSteps := map[string]struct{}{}

	for _, step := range keyword.Steps {
		stepByName[step.Name] = step

		deps, depErr := lang.StepDependencies(step)
		if depErr != nil {
			return &report.ErrorDetail{Kind: "keyword", Message: fmt.Sprintf("keyword %q dependencies for step %q: %v", keyword.Name, step.Name, depErr)}
		}

		depsByStep[step.Name] = deps
	}

	for _, layer := range layers {
		for _, stepName := range layer {
			step := stepByName[stepName]

			if hasFailedDependency(step.Name, depsByStep, failedSteps) {
				failedSteps[step.Name] = struct{}{}

				continue
			}

			stepResult := r.executeStep(ctx, evaluator, suite, scenarioName, config, keywordState, input, step)
			if stepResult.Status == report.StatusFail {
				failedSteps[step.Name] = struct{}{}

				message := "keyword step failed"
				if stepResult.Failure != nil {
					message = stepResult.Failure.Message
				}

				return &report.ErrorDetail{
					Kind:    "keyword",
					Path:    step.Name,
					Message: fmt.Sprintf("keyword %q step %q failed: %s", keyword.Name, step.Name, message),
				}
			}
		}
	}

	return nil
}

func hasFailedDependency(stepName string, depsByStep map[string]map[string]struct{}, failedSteps map[string]struct{}) bool {
	for dependency := range depsByStep[stepName] {
		if _, failed := failedSteps[dependency]; failed {
			return true
		}
	}

	return false
}

func evaluateKeywordOutputs(evaluator *lang.Evaluator, scenarioName, callingStepName string, config map[string]cty.Value, keywordState *ScenarioState, input map[string]cty.Value, keyword *model.Keyword) (map[string]cty.Value, error) {
	outputValues := map[string]cty.Value{}

	if len(keyword.Outputs) == 0 {
		return outputValues, nil
	}

	keys := make([]string, 0, len(keyword.Outputs))
	for key := range keyword.Outputs {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	scope := lang.ScopeData{
		Config:   config,
		Result:   keywordState.GetResultMap(),
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    ensureValueMap(input),
	}

	for _, key := range keys {
		value, err := evaluator.Eval(keyword.Outputs[key], scope, lang.GenerateMeta{Scenario: scenarioName, Step: callingStepName, ExprPath: "keyword." + keyword.Name + ".outputs." + key})
		if err != nil {
			return nil, fmt.Errorf("keyword %q outputs.%s: %w", keyword.Name, key, err)
		}

		outputValues[key] = value
	}

	return outputValues, nil
}

func validateKeywordInputs(keyword *model.Keyword, inputs map[string]cty.Value) error {
	if len(keyword.Inputs) == 0 {
		return nil
	}

	for name := range keyword.Inputs {
		if _, ok := inputs[name]; !ok {
			return fmt.Errorf("keyword %q missing required input %q", keyword.Name, name)
		}
	}

	return nil
}

func ensureValueMap(values map[string]cty.Value) map[string]cty.Value {
	if values == nil {
		return map[string]cty.Value{}
	}

	return values
}

func toValueMap(value cty.Value, field string) (map[string]cty.Value, error) {
	if value.IsNull() {
		return map[string]cty.Value{}, nil
	}

	if !value.Type().IsMapType() && !value.Type().IsObjectType() {
		return nil, fmt.Errorf("%s must be an object", field)
	}

	return value.AsValueMap(), nil
}

func toKeySet(values map[string]cty.Value) map[string]struct{} {
	keys := make(map[string]struct{}, len(values))

	for key := range values {
		keys[key] = struct{}{}
	}

	return keys
}

func toErrorDetail(err error) *report.ErrorDetail {
	var mismatch *assertion.Mismatch
	if errors.As(err, &mismatch) {
		message := mismatch.Message
		if message == "" {
			message = fmt.Sprintf("assertion failed at %s", mismatch.Path)
		}

		return &report.ErrorDetail{
			Kind:    mismatch.Kind,
			Path:    mismatch.Path,
			Want:    diagnostic.Normalize(mismatch.Want),
			Got:     diagnostic.Normalize(mismatch.Got),
			Message: message,
		}
	}

	return &report.ErrorDetail{Kind: "assertion", Message: err.Error()}
}

func filterScenarios(scenarios []*model.Scenario, tags []string, scenarioName string) []*model.Scenario {
	if len(tags) == 0 && scenarioName == "" {
		return scenarios
	}

	tagSet := map[string]struct{}{}
	for _, tag := range tags {
		tagSet[tag] = struct{}{}
	}

	out := make([]*model.Scenario, 0)

	for _, scenario := range scenarios {
		if scenarioName != "" && scenario.Name != scenarioName {
			continue
		}

		if len(tagSet) > 0 {
			match := false

			for _, tag := range scenario.Tags {
				if _, ok := tagSet[tag]; ok {
					match = true

					break
				}
			}

			if !match {
				continue
			}
		}

		out = append(out, scenario)
	}

	return out
}
