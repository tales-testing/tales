package runtime

import (
	"context"
	"fmt"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/assertion"
	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	webhookprovider "github.com/tales-testing/tales/internal/provider/webhook"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// webhookProviderType is the provider label that triggers webhook step
// execution.
const webhookProviderType = "webhook"

const (
	defaultWebhookTimeout = 30 * time.Second
	defaultWebhookCount   = 1
	defaultHMACAlgorithm  = "sha256"

	webhookOpStart = "start"
	webhookOpWait  = "wait"
	webhookOpStop  = "stop"
)

// executeWebhookStep lowers a webhook step's start / wait / stop expressions,
// dispatches to the webhook provider, then runs the operation-specific finish
// (capture for start/stop; custom request + HMAC assertions plus capture for
// wait). Assertions reuse the shared assertion engine; secrets never reach the
// report.
func (r *Runner) executeWebhookStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	start := time.Now()
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass, StartedAt: start}

	if step.Webhook == nil {
		return failStep(stepReport, start, kindEval, "", "webhook step is missing an operation")
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		return failStep(stepReport, start, kindVars, failedVar, err.Error())
	}

	exec, evalErr := evaluateWebhookExecution(evaluator, scope, scenarioName, step, state.Seed())
	if evalErr != nil {
		return failStep(stepReport, start, kindEval, "", evalErr.Error())
	}

	providerImpl, ok := r.providers.Get(step.Provider)
	if !ok {
		return failStep(stepReport, start, kindProvider, "", fmt.Sprintf("unknown provider %q", step.Provider))
	}

	output, err := providerImpl.Execute(ctx, provider.Input{
		Scenario: scenarioName,
		Step:     step,
		Phase:    phase,
		Attempt:  attempt,
		Config:   config,
		Webhook:  exec,
		Timeout:  exec.Timeout,
	})
	if err != nil {
		return failStep(stepReport, start, kindProvider, "", err.Error())
	}

	switch exec.Operation {
	case webhookOpWait:
		return r.finishWebhookWait(evaluator, scope, scenarioName, state, step, output, stepReport, start)
	default: // start, stop
		return finishWebhookSimple(evaluator, scope, scenarioName, state, step, output, exec, stepReport, start)
	}
}

// finishWebhookSimple handles the start / stop completion: metadata-only report
// plus capture. For start, the `webhook` namespace (id / url / ...) is injected
// so capture blocks can read the receiver descriptor.
func finishWebhookSimple(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output, exec *provider.WebhookExecution, stepReport *report.StepResult, start time.Time) *report.StepResult {
	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)

	scope.Request = output.Request
	scope.Response = output.Response

	var extraVars map[string]cty.Value
	if exec.Operation == webhookOpStart {
		extraVars = map[string]cty.Value{"webhook": cty.ObjectVal(output.Response)}
	}

	if captureErr := applyWebhookCapture(evaluator, scope, scenarioName, state, step, output, extraVars); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// finishWebhookWait wires the received request into scope, runs the webhook
// assertions, and captures. The report stays metadata-only with masked headers.
func (r *Runner) finishWebhookWait(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output, stepReport *report.StepResult, start time.Time) *report.StepResult {
	scope.Request = output.Request
	scope.Response = output.Response

	stepReport.Request = webhookWaitRequestReport(output.Request)
	stepReport.Response = webhookWaitResponseReport(output.Response)

	if assertErr := assertWebhook(evaluator, scope, scenarioName, step, output.Request); assertErr != nil {
		stepReport.Status = report.StatusFail
		stepReport.Failure = toErrorDetail(assertErr)
		stepReport.Duration = time.Since(start)

		return stepReport
	}

	if captureErr := applyWebhookCapture(evaluator, scope, scenarioName, state, step, output, nil); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// applyWebhookCapture evaluates the capture expressions (with optional extra
// variables such as the `webhook` namespace) and records the step result.
func applyWebhookCapture(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output, extraVars map[string]cty.Value) *captureError {
	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	var extras []map[string]cty.Value
	if extraVars != nil {
		extras = append(extras, extraVars)
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.EvalWithExtras(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key}, nil, extras...)
		if err != nil {
			return &captureError{path: key, message: err.Error()}
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	return nil
}

// evaluateWebhookExecution lowers the step's webhook block into the concrete
// payload consumed by the provider.
func evaluateWebhookExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, seed int64) (*provider.WebhookExecution, error) {
	wc := step.Webhook

	switch {
	case wc.Start != nil:
		return evaluateWebhookStart(evaluator, scope, scenarioName, step, seed)
	case wc.Wait != nil:
		return evaluateWebhookWait(evaluator, scope, scenarioName, step)
	case wc.Stop != nil:
		return evaluateWebhookStop(evaluator, scope, scenarioName, step)
	default:
		return nil, fmt.Errorf("webhook step must define one of start, wait, or stop")
	}
}

func evaluateWebhookStart(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, seed int64) (*provider.WebhookExecution, error) {
	sc := step.Webhook.Start

	path, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "start.path", sc.Path)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("start.path must start with \"/\"")
	}

	exec := &provider.WebhookExecution{
		Operation: webhookOpStart,
		ID:        fmt.Sprintf("webhook_%016x", DeriveSeed(seed, scenarioName, step.Name, step.File, "webhook-id")),
		Path:      path,
	}

	for _, field := range []struct {
		path string
		expr model.Expression
		dst  *string
	}{
		{"start.address", sc.Address, &exec.Address},
		{"start.public_url", sc.PublicURL, &exec.PublicURL},
		{"start.public_scheme", sc.PublicScheme, &exec.PublicScheme},
		{"start.public_host", sc.PublicHost, &exec.PublicHost},
	} {
		value, strErr := evalWebhookOptionalString(evaluator, scope, scenarioName, step.Name, field.path, field.expr)
		if strErr != nil {
			return nil, strErr
		}

		*field.dst = value
	}

	port, err := evalWebhookOptionalInt(evaluator, scope, scenarioName, step.Name, "start.public_port", sc.PublicPort)
	if err != nil {
		return nil, err
	}

	exec.PublicPort = port

	size, err := evalWebhookSize(evaluator, scope, scenarioName, step.Name, "start.max_body_size", sc.MaxBodySize)
	if err != nil {
		return nil, err
	}

	exec.MaxBodySize = size

	return exec, nil
}

func evaluateWebhookWait(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.WebhookExecution, error) {
	wt := step.Webhook.Wait

	target, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "target", step.Webhook.Target)
	if err != nil {
		return nil, err
	}

	timeout, err := evalDurationAttr(evaluator, scope, scenarioName, step.Name, "wait.timeout", wt.Timeout)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}

	count := defaultWebhookCount

	if !wt.Count.Empty() {
		count, err = evalWebhookOptionalInt(evaluator, scope, scenarioName, step.Name, "wait.count", wt.Count)
		if err != nil {
			return nil, err
		}

		if count < 1 {
			return nil, fmt.Errorf("wait.count must be greater than or equal to 1")
		}
	}

	return &provider.WebhookExecution{Operation: webhookOpWait, Target: target, Timeout: timeout, Count: count}, nil
}

func evaluateWebhookStop(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (*provider.WebhookExecution, error) {
	target, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "stop.target", step.Webhook.Stop.Target)
	if err != nil {
		return nil, err
	}

	return &provider.WebhookExecution{Operation: webhookOpStop, Target: target}, nil
}

// assertWebhook runs the request and HMAC signature assertions against the
// received request. received is the cty map exposed as the `request` namespace.
func assertWebhook(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, received map[string]cty.Value) error {
	we := step.Webhook.Expect
	if we == nil {
		return nil
	}

	if we.Request != nil {
		if err := assertWebhookRequest(evaluator, scope, scenarioName, step, we.Request, received); err != nil {
			return err
		}
	}

	if we.HMAC != nil {
		if err := assertWebhookHMAC(evaluator, scope, scenarioName, step, we.HMAC, received); err != nil {
			return err
		}
	}

	return nil
}

//nolint:gocyclo // Each declared expectation field is an independent, flat check; splitting per-field would only scatter the dispatch.
func assertWebhookRequest(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, expect *model.WebhookRequestExpect, received map[string]cty.Value) error {
	meta := func(path string) lang.GenerateMeta {
		return lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path}
	}

	if !expect.Method.Empty() {
		expected, err := evaluator.Eval(expect.Method, scope, meta("expect.request.method"))
		if err != nil {
			return fmt.Errorf("expect.request.method: %w", err)
		}

		if err := assertion.MatchJSON(expected, received["method"], true, "request.method"); err != nil {
			return fmt.Errorf("assert request.method: %w", err)
		}
	}

	if !expect.Path.Empty() {
		expected, err := evaluator.Eval(expect.Path, scope, meta("expect.request.path"))
		if err != nil {
			return fmt.Errorf("expect.request.path: %w", err)
		}

		if err := assertion.MatchJSON(expected, received["path"], true, "request.path"); err != nil {
			return fmt.Errorf("assert request.path: %w", err)
		}
	}

	if !expect.Headers.Empty() {
		if err := assertWebhookKeyedMap(evaluator, scope, meta("expect.request.headers"), expect.Headers, received["headers"], "request.headers", true); err != nil {
			return err
		}
	}

	if !expect.Query.Empty() {
		if err := assertWebhookKeyedMap(evaluator, scope, meta("expect.request.query"), expect.Query, received["query"], "request.query", false); err != nil {
			return err
		}
	}

	if !expect.JSON.Empty() {
		expected, err := evaluator.Eval(expect.JSON, scope, meta("expect.request.json"))
		if err != nil {
			return fmt.Errorf("expect.request.json: %w", err)
		}

		if err := assertion.MatchJSON(expected, received["json"], false, "request.json"); err != nil {
			return fmt.Errorf("assert request.json: %w", err)
		}
	}

	if !expect.Body.Empty() {
		expected, err := evaluator.Eval(expect.Body, scope, meta("expect.request.body"))
		if err != nil {
			return fmt.Errorf("expect.request.body: %w", err)
		}

		if err := assertion.MatchJSON(expected, webhookRawBody(received), true, "request.body"); err != nil {
			return fmt.Errorf("assert request.body: %w", err)
		}
	}

	return nil
}

// assertWebhookKeyedMap matches an expected object partially against a received
// object keyed by name. When canonical is set, keys are compared using the
// canonical MIME header form (used for headers).
func assertWebhookKeyedMap(evaluator *lang.Evaluator, scope lang.ScopeData, meta lang.GenerateMeta, expectExpr model.Expression, actual cty.Value, pathPrefix string, canonical bool) error {
	expected, err := evaluator.Eval(expectExpr, scope, meta)
	if err != nil {
		return fmt.Errorf("%s: %w", meta.ExprPath, err)
	}

	if expected.IsNull() {
		return nil
	}

	if !expected.Type().IsObjectType() && !expected.Type().IsMapType() {
		return fmt.Errorf("%s must be an object", meta.ExprPath)
	}

	for key, expectedVal := range expected.AsValueMap() {
		lookup := key
		if canonical {
			lookup = textproto.CanonicalMIMEHeaderKey(key)
		}

		actualVal := cty.NullVal(cty.String)
		if !actual.IsNull() && actual.Type().IsObjectType() && actual.Type().HasAttribute(lookup) {
			actualVal = actual.GetAttr(lookup)
		}

		if err := assertion.MatchJSON(expectedVal, actualVal, false, pathPrefix+"."+key); err != nil {
			return fmt.Errorf("assert %s.%s: %w", pathPrefix, key, err)
		}
	}

	return nil
}

// assertWebhookHMAC verifies the signature header on the received request. It
// never embeds the secret, payload, or computed digest in any error.
func assertWebhookHMAC(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, hmacExpect *model.WebhookHMACExpect, received map[string]cty.Value) error {
	headerName, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "expect.hmac_signature.header", hmacExpect.Header)
	if err != nil {
		return err
	}

	secret, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "expect.hmac_signature.secret", hmacExpect.Secret)
	if err != nil {
		return err
	}

	format, err := evalStringAttr(evaluator, scope, scenarioName, step.Name, "expect.hmac_signature.format", hmacExpect.Format)
	if err != nil {
		return err
	}

	algorithm := defaultHMACAlgorithm

	if !hmacExpect.Algorithm.Empty() {
		algorithm, err = evalWebhookOptionalString(evaluator, scope, scenarioName, step.Name, "expect.hmac_signature.algorithm", hmacExpect.Algorithm)
		if err != nil {
			return err
		}

		algorithm = strings.ToLower(algorithm)
	}

	headerValue, found := webhookHeaderValue(received, headerName)
	if !found {
		return fmt.Errorf("missing signature header %q", headerName)
	}

	sigFormat, err := webhookprovider.ParseSignatureFormat(format)
	if err != nil {
		return fmt.Errorf("expect.hmac_signature.format: %w", err)
	}

	timestamp, signature, ok := sigFormat.Parse(headerValue)
	if !ok {
		return fmt.Errorf("signature header does not match format %q", format)
	}

	if err := assertWebhookTimestampRequired(evaluator, scope, scenarioName, step, hmacExpect, sigFormat, timestamp); err != nil {
		return err
	}

	payload, err := evalWebhookPayload(evaluator, scope, scenarioName, step, hmacExpect.Payload, timestamp)
	if err != nil {
		return err
	}

	expected, err := webhookprovider.ComputeHMAC(algorithm, secret, payload)
	if err != nil {
		return fmt.Errorf("expect.hmac_signature: %w", err)
	}

	if !webhookprovider.SignaturesEqual(expected, signature) {
		return fmt.Errorf("invalid webhook signature")
	}

	return assertWebhookTolerance(evaluator, scope, scenarioName, step, hmacExpect, timestamp)
}

func assertWebhookTimestampRequired(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, hmacExpect *model.WebhookHMACExpect, sigFormat *webhookprovider.SignatureFormat, timestamp string) error {
	required := sigFormat.HasTimestamp

	if !hmacExpect.TimestampRequired.Empty() {
		value, err := evaluator.Eval(hmacExpect.TimestampRequired, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.hmac_signature.timestamp_required"})
		if err != nil {
			return fmt.Errorf("expect.hmac_signature.timestamp_required: %w", err)
		}

		if !value.IsNull() {
			parsed, boolErr := toBool(value)
			if boolErr != nil {
				return fmt.Errorf("expect.hmac_signature.timestamp_required: %w", boolErr)
			}

			required = parsed
		}
	}

	if required && strings.TrimSpace(timestamp) == "" {
		return fmt.Errorf("signature header is missing a timestamp")
	}

	return nil
}

func assertWebhookTolerance(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, hmacExpect *model.WebhookHMACExpect, timestamp string) error {
	if hmacExpect.TimestampTolerance.Empty() {
		return nil
	}

	tolerance, err := evalDurationAttr(evaluator, scope, scenarioName, step.Name, "expect.hmac_signature.timestamp_tolerance", hmacExpect.TimestampTolerance)
	if err != nil {
		return err
	}

	if tolerance <= 0 {
		return nil
	}

	within, err := webhookprovider.TimestampWithinTolerance(timestamp, time.Now(), tolerance)
	if err != nil {
		return fmt.Errorf("expect.hmac_signature.timestamp_tolerance: %w", err)
	}

	if !within {
		return fmt.Errorf("webhook signature timestamp is outside tolerance %s", tolerance)
	}

	return nil
}

func evalWebhookPayload(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, payloadExpr model.Expression, timestamp string) (string, error) {
	if payloadExpr.Empty() {
		return "", fmt.Errorf("expect.hmac_signature.payload is required")
	}

	value, err := evaluator.EvalWithExtras(payloadExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "expect.hmac_signature.payload"}, nil, map[string]cty.Value{"timestamp": cty.StringVal(timestamp)})
	if err != nil {
		return "", fmt.Errorf("expect.hmac_signature.payload: %w", err)
	}

	if value.IsNull() || value.Type() != cty.String {
		return "", fmt.Errorf("expect.hmac_signature.payload must evaluate to a string")
	}

	return value.AsString(), nil
}

// webhookHeaderValue reads a first-value header from the received request's
// `headers` object using the canonical MIME key.
func webhookHeaderValue(received map[string]cty.Value, name string) (string, bool) {
	headers, ok := received["headers"]
	if !ok || headers.IsNull() || !headers.Type().IsObjectType() {
		return "", false
	}

	canonical := textproto.CanonicalMIMEHeaderKey(name)
	if !headers.Type().HasAttribute(canonical) {
		return "", false
	}

	value := headers.GetAttr(canonical)
	if value.IsNull() || value.Type() != cty.String {
		return "", false
	}

	return value.AsString(), true
}

func webhookRawBody(received map[string]cty.Value) cty.Value {
	body, ok := received["body"]
	if !ok || body.IsNull() || !body.Type().IsObjectType() || !body.Type().HasAttribute("raw") {
		return cty.NullVal(cty.String)
	}

	return body.GetAttr("raw")
}

// webhookWaitRequestReport builds the metadata-only report for a wait step. The
// headers are routed through the masking pipeline (the "headers" key triggers
// MaskHeaders), and the raw body / query are intentionally omitted.
func webhookWaitRequestReport(received map[string]cty.Value) map[string]any {
	curated := map[string]cty.Value{}

	for _, key := range []string{"method", "path", "remote_addr", "received_at"} {
		if value, ok := received[key]; ok {
			curated[key] = value
		}
	}

	if headers, ok := received["headers"]; ok {
		curated["headers"] = headers
	}

	return diagnostic.FromCTYMap(curated)
}

// webhookWaitResponseReport surfaces only the received / count summary so the
// report never carries inbound payloads.
func webhookWaitResponseReport(response map[string]cty.Value) map[string]any {
	summary, ok := response[outputResponse]
	if !ok {
		summary = response["json"]
	}

	curated := map[string]cty.Value{}

	if summary.Type().IsObjectType() {
		for _, key := range []string{"received", "count"} {
			if summary.Type().HasAttribute(key) {
				curated[key] = summary.GetAttr(key)
			}
		}
	}

	return diagnostic.FromCTYMap(curated)
}

// evalWebhookOptionalString evaluates an optional string expression, returning
// "" when unset or null.
func evalWebhookOptionalString(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (string, error) {
	if expression.Empty() {
		return "", nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return "", fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() {
		return "", nil
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("%s must be a string", exprPath)
	}

	return value.AsString(), nil
}

// evalWebhookOptionalInt evaluates an optional integer expression, returning 0
// when unset or null.
func evalWebhookOptionalInt(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (int, error) {
	if expression.Empty() {
		return 0, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() {
		return 0, nil
	}

	if value.Type() != cty.Number {
		return 0, fmt.Errorf("%s must be a number", exprPath)
	}

	n, acc := value.AsBigFloat().Int64()
	if acc != 0 {
		return 0, fmt.Errorf("%s must be an integer", exprPath)
	}

	return int(n), nil
}

// evalWebhookSize evaluates an optional body-size expression. It accepts a raw
// byte count (number) or a string with a unit suffix (B / KB / MB / GB, binary).
func evalWebhookSize(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName, stepName, exprPath string, expression model.Expression) (int64, error) {
	if expression.Empty() {
		return 0, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: stepName, ExprPath: exprPath})
	if err != nil {
		return 0, fmt.Errorf("%s: %w", exprPath, err)
	}

	if value.IsNull() {
		return 0, nil
	}

	if value.Type() == cty.Number {
		n, _ := value.AsBigFloat().Int64()

		return n, nil
	}

	if value.Type() != cty.String {
		return 0, fmt.Errorf("%s must be a number of bytes or a size string", exprPath)
	}

	size, err := parseByteSize(value.AsString())
	if err != nil {
		return 0, fmt.Errorf("%s: %w", exprPath, err)
	}

	return size, nil
}

// parseByteSize parses a human size such as "10MB" or "512". KB/MB/GB are
// binary multiples (1024-based).
func parseByteSize(raw string) (int64, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(raw))
	if trimmed == "" {
		return 0, nil
	}

	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		if prefix, found := strings.CutSuffix(trimmed, unit.suffix); found {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(prefix), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q", raw)
			}

			return int64(parsed * float64(unit.mult)), nil
		}
	}

	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", raw)
	}

	return parsed, nil
}
