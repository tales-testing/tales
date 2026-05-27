package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// Options controls runtime execution.
type Options struct {
	Seed     int64
	Parallel int
	Tags     []string
	Scenario string
	// Events optionally receives progress events as scenarios start and
	// end. Nil disables streaming (the historical behavior). The sink must
	// be safe for concurrent calls.
	Events EventSink
	// HeartbeatInterval, when > 0 and Events is non-nil, spawns a ticker
	// that calls Events.Heartbeat with the list of in-flight scenarios at
	// each tick. Used by --verbose to surface slow scenarios that would
	// otherwise stay quiet between their start and end lines.
	HeartbeatInterval time.Duration
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

	emitSuiteStarted(opts.Events, len(scenarios), opts.Parallel, opts.Seed)

	// runCtx wraps the caller's ctx so we can unblock the deadline watcher
	// on normal completion. context.Canceled does not satisfy
	// errors.Is(_, context.DeadlineExceeded), so canceling manually after
	// wg.Wait correctly distinguishes "the user ran out of budget" from
	// "the suite finished cleanly".
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	sem := make(chan struct{}, opts.Parallel)
	tracker := newLiveScenarioTracker()
	stalledCh := watchDeadlineFor(runCtx, tracker)

	stopHeartbeat := startHeartbeat(runCtx, opts.Events, tracker, opts.HeartbeatInterval)
	defer stopHeartbeat()

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

			tracker.start(sc.Name)
			emitScenarioStarted(opts.Events, sc.Name)

			scenarioResult, runErr := r.runScenario(runCtx, suite, sc, configValues, opts.Seed)
			result.Scenarios[index] = scenarioResult

			// Capture the ctx state BEFORE the post-run bookkeeping so
			// the deadline-watcher's snapshot stays race-free. If runCtx
			// fired before we got here, the scenario was either cancelled
			// mid-flight or finished an instant ahead of the deadline;
			// either way we keep it in the tracker so the watcher can
			// surface it in the stalled list. tracker.end is only called
			// on the clean-completion path.
			ctxAlreadyFired := runCtx.Err() != nil

			emitScenarioEnded(opts.Events, scenarioResult)

			if !ctxAlreadyFired {
				tracker.end(sc.Name)
			}

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

	r.closeProviders()

	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(result.StartedAt)

	// Unblock the deadline watcher before reading its channel. If the
	// parent ctx fired first, runCancel is a no-op and the snapshot has
	// already been published; otherwise this is what makes the watcher
	// exit on the normal-completion path.
	runCancel()

	result.StalledScenarios = <-stalledCh

	emitSuiteEnded(opts.Events, result.Duration)

	if firstErr != nil {
		return result, firstErr
	}

	return result, nil
}

// closeProviders best-effort closes every provider that implements io.Closer
// so long-lived sessions (e.g. xcodebuild subprocesses owned by the mobile
// provider) do not leak past the end of a suite.
func (r *Runner) closeProviders() {
	for _, p := range r.providers.All() {
		closer, ok := p.(io.Closer)
		if !ok {
			continue
		}

		_ = closer.Close()
	}
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

	if handled := applyScenarioSkip(evaluator, scenario, sResult, state, config, start); handled {
		return sResult, nil
	}

	tracker := newDepTracker()
	depsByStep := map[string]map[string]struct{}{}

	for _, step := range scenario.Steps {
		deps, err := lang.StepDependencies(step)
		if err != nil {
			return nil, fmt.Errorf("resolve dependencies for step %q: %w", step.Name, err)
		}

		depsByStep[step.Name] = deps
	}

	r.runScenarioSteps(ctx, scenario, sResult, depsByStep, tracker, evaluator, suite, config, state)

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

	return sResult, nil
}

// runScenarioSteps executes a scenario's steps sequentially in .tales file
// order, honoring step-level skip rules and the dependency-skip cascade.
// Once a step fails the scenario stops: every later step is reported as
// skipped without being executed. Teardown still runs afterwards.
func (r *Runner) runScenarioSteps(ctx context.Context, scenario *model.Scenario, sResult *report.ScenarioResult, depsByStep map[string]map[string]struct{}, tracker *depTracker, evaluator *lang.Evaluator, suite *model.Suite, config map[string]cty.Value, state *ScenarioState) {
	for _, step := range scenario.Steps {
		r.runOneStep(ctx, scenario, sResult, step, depsByStep, tracker, evaluator, suite, config, state)
	}
}

// runOneStep handles the lifecycle of a single step: dep-failure
// cascade, skip-rule evaluation, provider execution, and bookkeeping.
func (r *Runner) runOneStep(ctx context.Context, scenario *model.Scenario, sResult *report.ScenarioResult, step *model.Step, depsByStep map[string]map[string]struct{}, tracker *depTracker, evaluator *lang.Evaluator, suite *model.Suite, config map[string]cty.Value, state *ScenarioState) {
	if blocker, failedDep, blocked := tracker.dependencyBlocker(depsByStep[step.Name]); blocked {
		recordDepCascadeSkip(sResult, step, scenario.Name, blocker, failedDep, tracker)

		return
	}

	if sResult.Status == report.StatusFail {
		recordHaltedSkip(sResult, step, scenario.Name, tracker)

		return
	}

	if stepSkip := evaluateStepSkip(evaluator, step, scenario.Name, state, config); stepSkip != nil {
		recordStepSkipResult(sResult, stepSkip, step.Name, tracker)

		return
	}

	stepResult := r.executeStep(ctx, evaluator, suite, scenario.Name, config, state, nil, step)

	sResult.Steps = append(sResult.Steps, stepResult)

	if stepResult.Status == report.StatusFail {
		tracker.markFailed(step.Name)

		if sResult.Failure == nil {
			sResult.Failure = stepResult.Failure
		}

		sResult.Status = report.StatusFail
	}
}

// recordDepCascadeSkip emits a StatusSkip result for a step that is
// being cascade-skipped because one of its dependencies did not
// produce a result, and propagates the skip downstream by marking
// the step as skipped in the tracker.
func recordDepCascadeSkip(sResult *report.ScenarioResult, step *model.Step, scenarioName, blocker string, blockerFailed bool, tracker *depTracker) {
	reason := fmt.Sprintf("depends on skipped step %q", blocker)
	if blockerFailed {
		reason = fmt.Sprintf("depends on failed step %q", blocker)
	}

	sResult.Steps = append(sResult.Steps, &report.StepResult{
		File:       step.File,
		Scenario:   scenarioName,
		Name:       step.Name,
		Provider:   step.Provider,
		Phase:      phaseStep,
		Status:     report.StatusSkip,
		SkipReason: reason,
	})

	tracker.markSkipped(step.Name)
}

// recordHaltedSkip emits a StatusSkip result for a step that is not executed
// because an earlier step in the scenario already failed. The scenario stops
// at the first failure; later steps are reported as skipped rather than run.
func recordHaltedSkip(sResult *report.ScenarioResult, step *model.Step, scenarioName string, tracker *depTracker) {
	sResult.Steps = append(sResult.Steps, &report.StepResult{
		File:       step.File,
		Scenario:   scenarioName,
		Name:       step.Name,
		Provider:   step.Provider,
		Phase:      phaseStep,
		Status:     report.StatusSkip,
		SkipReason: "not run: an earlier step in the scenario failed",
	})

	tracker.markSkipped(step.Name)
}

// recordStepSkipResult appends a skip-or-skip-eval-failure result
// to the scenario, propagating failure state when the rule itself
// errored, or registering the cascade-skip otherwise.
func recordStepSkipResult(sResult *report.ScenarioResult, stepSkip *report.StepResult, stepName string, tracker *depTracker) {
	sResult.Steps = append(sResult.Steps, stepSkip)

	if stepSkip.Status == report.StatusFail {
		if sResult.Failure == nil {
			sResult.Failure = stepSkip.Failure
		}

		sResult.Status = report.StatusFail
	}

	if stepSkip.Status == report.StatusFail {
		tracker.markFailed(stepName)

		return
	}

	tracker.markSkipped(stepName)
}

func (r *Runner) executeStep(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step) *report.StepResult {
	return r.executeStepInPhase(ctx, evaluator, suite, scenarioName, config, state, input, step, "step")
}

func (r *Runner) executeStepInPhase(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string) *report.StepResult {
	retry := retryOptions(step)
	start := time.Now()

	var lastResult *report.StepResult

	for attempt := 1; attempt <= retry.Attempts; attempt++ {
		attemptResult := r.executeStepAttempt(ctx, evaluator, suite, scenarioName, config, state, input, step, phase, attempt)
		attemptResult.Attempts = attempt
		lastResult = attemptResult

		if attemptResult.Status == report.StatusPass || attempt == retry.Attempts {
			attemptResult.Duration = time.Since(start)
			if attemptResult.StartedAt.IsZero() {
				attemptResult.StartedAt = start
			}

			return attemptResult
		}

		if retry.Interval > 0 {
			if !sleepWithContext(ctx, retry.Interval) {
				attemptResult.Status = report.StatusFail
				attemptResult.Failure = &report.ErrorDetail{Kind: kindRuntime, Message: "step retry interrupted by context cancellation"}
				attemptResult.Duration = time.Since(start)

				return attemptResult
			}
		}
	}

	if lastResult != nil {
		lastResult.Duration = time.Since(start)
		if lastResult.StartedAt.IsZero() {
			lastResult.StartedAt = start
		}

		return lastResult
	}

	return &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusFail, Attempts: retry.Attempts, StartedAt: start, Duration: time.Since(start), Failure: &report.ErrorDetail{Kind: kindRuntime, Message: "step was not executed"}}
}

func (r *Runner) executeStepAttempt(ctx context.Context, evaluator *lang.Evaluator, suite *model.Suite, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass}
	start := time.Now()

	if step.Provider == kindKeyword {
		return r.executeKeywordStep(ctx, evaluator, suite, scenarioName, config, state, input, step, start, stepReport)
	}

	if step.Provider == mobileProviderType {
		return r.executeMobileStep(ctx, evaluator, scenarioName, config, state, input, step, phase, attempt)
	}

	if step.Provider == sqlProviderType {
		return r.executeSQLStep(ctx, evaluator, scenarioName, config, state, input, step, phase, attempt)
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindVars, Path: failedVar, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	requestValues, timeout, err := evaluateRequest(evaluator, scope, scenarioName, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: err.Error()}
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

	output, err := providerImpl.Execute(ctx, provider.Input{Scenario: scenarioName, Step: step, Phase: phase, Attempt: attempt, Config: config, Request: requestValues, Timeout: timeout})
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindProvider, Message: err.Error()}
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
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = &report.ErrorDetail{Kind: kindCapture, Path: key, Message: err.Error()}
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
		return &report.StepResult{
			File:       step.File,
			Scenario:   scenarioName,
			Name:       step.Name,
			Provider:   step.Provider,
			Phase:      "teardown",
			Status:     report.StatusSkip,
			SkipReason: "when condition evaluated to false",
		}
	}

	return r.executeStepInPhase(ctx, evaluator, suite, scenarioName, config, state, input, step, "teardown")
}

// evaluateStepVars evaluates each declared step-local var in source order
// and mounts the cumulative map onto scope.Vars. Each var is evaluated with
// a scope that already includes all previously-evaluated vars, so later vars
// can read earlier ones via vars.<name>. Returns the failing var's name and
// the error on the first evaluation failure.
func evaluateStepVars(evaluator *lang.Evaluator, scope *lang.ScopeData, scenarioName string, step *model.Step) (string, error) {
	if len(step.Vars) == 0 {
		return "", nil
	}

	values := make(map[string]cty.Value, len(step.Vars))

	for _, v := range step.Vars {
		scope.Vars = values

		val, err := evaluator.Eval(v.Expr, *scope, lang.GenerateMeta{
			Scenario: scenarioName,
			Step:     step.Name,
			ExprPath: "vars." + v.Name,
		})
		if err != nil {
			return v.Name, fmt.Errorf("evaluate vars.%s: %w", v.Name, err)
		}

		values[v.Name] = val
	}

	scope.Vars = values

	return "", nil
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

	if step.Request.Body.Multipart != nil {
		parts, err := evaluateMultipartParts(evaluator, scope, scenarioName, step)
		if err != nil {
			return fmt.Errorf("multipart: %w", err)
		}

		body["multipart"] = parts
	}

	if len(body) > 0 {
		values["body"] = cty.ObjectVal(body)
	}

	return nil
}

// evaluateMultipartParts evaluates each multipart part's expressions in
// declaration order and packs the result as a cty.TupleVal so per-part
// heterogeneity (file vs field, path vs content) survives the cty round-trip
// without forcing every part to share the same attribute set.
func evaluateMultipartParts(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (cty.Value, error) {
	parts := make([]cty.Value, 0, len(step.Request.Body.Multipart.Parts))

	for i, part := range step.Request.Body.Multipart.Parts {
		switch {
		case part.File != nil:
			value, err := evaluateMultipartFilePart(evaluator, scope, scenarioName, step, i, part.File)
			if err != nil {
				return cty.NilVal, err
			}

			parts = append(parts, value)
		case part.Field != nil:
			value, err := evaluateMultipartFieldPart(evaluator, scope, scenarioName, step, i, part.Field)
			if err != nil {
				return cty.NilVal, err
			}

			parts = append(parts, value)
		}
	}

	if len(parts) == 0 {
		return cty.EmptyTupleVal, nil
	}

	return cty.TupleVal(parts), nil
}

func evaluateMultipartFilePart(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, index int, file *model.MultipartFilePart) (cty.Value, error) {
	attrs := map[string]cty.Value{attrKind: cty.StringVal("file")}

	eval := func(name string, expression model.Expression) error {
		if expression.Empty() {
			return nil
		}

		val, err := evaluator.Eval(expression, scope, lang.GenerateMeta{
			Scenario: scenarioName,
			Step:     step.Name,
			ExprPath: fmt.Sprintf("request.body.multipart[%d].%s", index, name),
		})
		if err != nil {
			return fmt.Errorf("part %d %s: %w", index, name, err)
		}

		attrs[name] = val

		return nil
	}

	if err := eval("field", file.Field); err != nil {
		return cty.NilVal, err
	}

	if err := eval("path", file.Path); err != nil {
		return cty.NilVal, err
	}

	if err := eval("content", file.Content); err != nil {
		return cty.NilVal, err
	}

	if err := eval("filename", file.Filename); err != nil {
		return cty.NilVal, err
	}

	if err := eval("content_type", file.ContentType); err != nil {
		return cty.NilVal, err
	}

	return cty.ObjectVal(attrs), nil
}

func evaluateMultipartFieldPart(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, index int, field *model.MultipartFieldPart) (cty.Value, error) {
	attrs := map[string]cty.Value{attrKind: cty.StringVal("field")}

	eval := func(name string, expression model.Expression) error {
		val, err := evaluator.Eval(expression, scope, lang.GenerateMeta{
			Scenario: scenarioName,
			Step:     step.Name,
			ExprPath: fmt.Sprintf("request.body.multipart[%d].%s", index, name),
		})
		if err != nil {
			return fmt.Errorf("part %d %s: %w", index, name, err)
		}

		attrs[name] = val

		return nil
	}

	if err := eval("name", field.Name); err != nil {
		return cty.NilVal, err
	}

	if err := eval(keyValue, field.Value); err != nil {
		return cty.NilVal, err
	}

	return cty.ObjectVal(attrs), nil
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
			"username":  usernameValue,
			keyPassword: passwordValue,
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
		stepReport.Failure = &report.ErrorDetail{Kind: kindKeyword, Message: "keyword step is missing keyword call configuration"}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	scope := lang.ScopeData{
		Config:   config,
		Result:   state.GetResultMap(),
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    ensureValueMap(input),
	}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindVars, Path: failedVar, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	callName, callInputs, requestSummary, err := r.evaluateKeywordCall(evaluator, scope, scenarioName, step)
	if err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindEval, Message: err.Error()}
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	keyword, ok := suite.Keywords[callName]
	if !ok {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindKeyword, Message: fmt.Sprintf("unknown keyword %q", callName)}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	if err := validateKeywordInputs(keyword, callInputs); err != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindKeyword, Message: err.Error()}
		stepReport.Duration = time.Since(start)
		stepReport.Request = requestSummary

		return stepReport
	}

	outerResults := state.GetResultMap()

	keywordState, stateErr := newKeywordState(outerResults, keyword.Steps)
	if stateErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = &report.ErrorDetail{Kind: kindKeyword, Message: stateErr.Error()}
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
		stepReport.Failure = &report.ErrorDetail{Kind: kindKeyword, Message: err.Error()}
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

func (r *Runner) evaluateKeywordCall(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (string, map[string]cty.Value, map[string]interface{}, error) {
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
		keyName:  cty.StringVal(keywordName),
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
	if err := lang.ValidateStepOrder(keyword.Steps, externalDeps); err != nil {
		return &report.ErrorDetail{Kind: kindKeyword, Message: fmt.Sprintf("keyword %q graph error: %v", keyword.Name, err)}
	}

	for _, step := range keyword.Steps {
		stepResult := r.executeStep(ctx, evaluator, suite, scenarioName, config, keywordState, input, step)
		if stepResult.Status == report.StatusFail {
			message := "keyword step failed"
			if stepResult.Failure != nil {
				message = stepResult.Failure.Message
			}

			return &report.ErrorDetail{
				Kind:    kindKeyword,
				Path:    step.Name,
				Message: fmt.Sprintf("keyword %q step %q failed: %s", keyword.Name, step.Name, message),
			}
		}
	}

	return nil
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
