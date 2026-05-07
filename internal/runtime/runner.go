package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/assertion"
	"github.com/hyperxlab/tales/internal/dag"
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

	configValues, err := evalConfig(suite, opts.Seed)
	if err != nil {
		return nil, err
	}

	scenarios := filterScenarios(suite.Scenarios, opts.Tags, opts.Scenario)
	result := &report.SuiteResult{Seed: opts.Seed, StartedAt: time.Now(), Scenarios: make([]*report.ScenarioResult, len(scenarios))}

	sem := make(chan struct{}, opts.Parallel)
	var wg sync.WaitGroup
	var firstErr error
	var firstErrMu sync.Mutex

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
		rnd := NewDeterministicRand(seed, scenario.Name, meta.Step, name, meta.ExprPath)
		return runGenerator(gen.Type, params, rnd)
	})

	layers, orderErr := buildLayers(scenario.Steps)
	if orderErr != nil {
		sResult.Status = report.StatusFail
		sResult.Failure = &report.ErrorDetail{Kind: "dag", Message: orderErr.Error()}
		sResult.Duration = time.Since(start)
		return sResult, orderErr
	}

	failedSteps := map[string]struct{}{}
	stepByName := map[string]*model.Step{}
	depsByStep := map[string]map[string]struct{}{}
	for _, step := range scenario.Steps {
		stepByName[step.Name] = step
		deps, err := lang.StepDependencies(step)
		if err != nil {
			return nil, err
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

				for dep := range depsByStep[step.Name] {
					if _, failed := failedSteps[dep]; failed {
						mu.Lock()
						sResult.Steps = append(sResult.Steps, &report.StepResult{File: step.File, Scenario: scenario.Name, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusSkip})
						failedSteps[step.Name] = struct{}{}
						mu.Unlock()
						return
					}
				}

				stepResult := r.executeStep(ctx, evaluator, scenario.Name, config, state, step)
				mu.Lock()
				sResult.Steps = append(sResult.Steps, stepResult)
				if stepResult.Status == report.StatusFail {
					failedSteps[step.Name] = struct{}{}
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
		stepResult := r.executeTeardownStep(ctx, evaluator, scenario.Name, config, state, step)
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

func (r *Runner) executeStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, step *model.Step) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "step", Status: report.StatusPass}
	start := time.Now()

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}
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

	output, err := providerImpl.Execute(ctx, provider.Input{Scenario: scenarioName, Step: step, Request: requestValues, Timeout: timeout})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: "provider", Message: err.Error()}
		stepReport.Duration = time.Since(start)
		return stepReport
	}

	stepReport.Duration = output.Duration
	stepReport.StatusCode = output.StatusCode
	stepReport.Request = summarize(output.Request)
	stepReport.Response = summarize(output.Response)

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

func (r *Runner) executeTeardownStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, step *model.Step) *report.StepResult {
	if !evalWhen(step.When, evaluator, lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}, scenarioName, step.Name) {
		return &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: "teardown", Status: report.StatusSkip}
	}
	result := r.executeStep(ctx, evaluator, scenarioName, config, state, step)
	result.Phase = "teardown"
	return result
}

func buildLayers(steps []*model.Step) ([][]string, error) {
	g := dag.NewGraph()
	for _, step := range steps {
		if err := g.AddNode(step.Name); err != nil {
			return nil, err
		}
	}
	for _, step := range steps {
		deps, err := lang.StepDependencies(step)
		if err != nil {
			return nil, err
		}
		for dep := range deps {
			if err := g.AddEdge(dep, step.Name); err != nil {
				return nil, fmt.Errorf("step %q: %w", step.Name, err)
			}
		}
	}
	return dag.TopologicalLayers(g)
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
			return err
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
	if err := setExpr("json", step.Request.JSON); err != nil {
		return nil, 0, fmt.Errorf("request.json: %w", err)
	}
	if err := setExpr("body", step.Request.Body); err != nil {
		return nil, 0, fmt.Errorf("request.body: %w", err)
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

func evaluateExpect(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, output *provider.Output) error {
	if step.Expect == nil {
		return nil
	}
	if !step.Expect.Status.Empty() {
		expectedStatus, err := evaluator.Eval(step.Expect.Status, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.status"})
		if err != nil {
			return fmt.Errorf("expect.status: %w", err)
		}
		if !expectedStatus.IsNull() {
			if err := assertion.MatchJSON(expectedStatus, cty.NumberIntVal(int64(output.StatusCode)), true, "status"); err != nil {
				return err
			}
		}
	}
	if !step.Expect.Headers.Empty() {
		expectedHeaders, err := evaluator.Eval(step.Expect.Headers, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.headers"})
		if err != nil {
			return fmt.Errorf("expect.headers: %w", err)
		}
		if !expectedHeaders.IsNull() {
			actualHeaders := output.Response["headers"]
			if !expectedHeaders.Type().IsObjectType() && !expectedHeaders.Type().IsMapType() {
				return fmt.Errorf("expect.headers must be object")
			}
			for key, expected := range expectedHeaders.AsValueMap() {
				actual := cty.NullVal(cty.String)
				if actualHeaders.Type().HasAttribute(key) {
					actual = actualHeaders.GetAttr(key)
				}
				if err := assertion.MatchJSON(expected, actual, true, "headers."+key); err != nil {
					return err
				}
			}
		}
	}
	if !step.Expect.JSON.Empty() {
		expectedJSON, err := evaluator.Eval(step.Expect.JSON, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.json"})
		if err != nil {
			return fmt.Errorf("expect.json: %w", err)
		}
		if !expectedJSON.IsNull() {
			strict := false
			if !step.Expect.Strict.Empty() {
				strictValue, err := evaluator.Eval(step.Expect.Strict, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.strict"})
				if err != nil {
					return fmt.Errorf("expect.strict: %w", err)
				}
				if !strictValue.IsNull() {
					strict, err = toBool(strictValue)
					if err != nil {
						return err
					}
				}
			}
			responseJSON := output.Response["json"]
			if err := assertion.MatchJSON(expectedJSON, responseJSON, strict, "$"); err != nil {
				return err
			}
		}
	}
	return nil
}

func toDuration(value cty.Value) (time.Duration, error) {
	if value.Type() == cty.Number {
		ms, _ := value.AsBigFloat().Int64()
		return time.Duration(ms) * time.Millisecond, nil
	}
	if value.Type() == cty.String {
		return time.ParseDuration(value.AsString())
	}
	return 0, fmt.Errorf("timeout must be number(milliseconds) or duration string")
}

func toBool(value cty.Value) (bool, error) {
	if value.Type() != cty.Bool {
		return false, fmt.Errorf("expected boolean value")
	}
	return value.True(), nil
}

func evalConfig(suite *model.Suite, seed int64) (map[string]cty.Value, error) {
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
	_ = seed
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

func summarize(values map[string]cty.Value) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if value.Type() == cty.String {
			out[key] = value.AsString()
			continue
		}
		out[key] = value.GoString()
	}
	return out
}

func toErrorDetail(err error) *report.ErrorDetail {
	if mismatch, ok := err.(*assertion.Mismatch); ok {
		return &report.ErrorDetail{Kind: mismatch.Kind, Path: mismatch.Path, Want: mismatch.Want, Got: mismatch.Got, Message: mismatch.Error()}
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

func runGenerator(generatorType string, params map[string]cty.Value, rnd seededRandom) (cty.Value, error) {
	switch generatorType {
	case "email":
		prefix := ""
		if v, ok := params["prefix"]; ok && v.Type() == cty.String {
			prefix = v.AsString()
		}
		domain := "example.com"
		if v, ok := params["domain"]; ok && v.Type() == cty.String {
			domain = v.AsString()
		}
		return cty.StringVal(prefix + randomString(rnd, 10) + "@" + domain), nil
	default:
		return cty.NilVal, fmt.Errorf("generator type %q is not supported", generatorType)
	}
}

type seededRandom interface {
	Intn(n int) int
}

func randomString(rnd seededRandom, size int) string {
	letters := "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := strings.Builder{}
	buf.Grow(size)
	for i := 0; i < size; i++ {
		buf.WriteByte(letters[rnd.Intn(len(letters))])
	}
	return buf.String()
}
