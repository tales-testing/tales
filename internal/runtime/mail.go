package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// mailProviderType is the provider label that triggers mail step execution.
const mailProviderType = "mail"

// executeMailStep evaluates a mail step's target and message expressions,
// derives the Message-ID deterministically, and dispatches to the mail
// provider. Expect / capture / retry / skip semantics reuse the standard step
// pipeline; only the per-step inputs differ from the HTTP provider.
func (r *Runner) executeMailStep(ctx context.Context, evaluator *lang.Evaluator, scenarioName string, config map[string]cty.Value, state *ScenarioState, input map[string]cty.Value, step *model.Step, phase string, attempt int) *report.StepResult {
	stepReport := &report.StepResult{File: step.File, Scenario: scenarioName, Name: step.Name, Provider: step.Provider, Phase: phase, Status: report.StatusPass}
	start := time.Now()

	if step.Mail == nil {
		return failStep(stepReport, start, kindEval, "", "mail step is missing a message block")
	}

	scope := lang.ScopeData{Config: config, Result: state.GetResultMap(), Request: map[string]cty.Value{}, Response: map[string]cty.Value{}, Input: ensureValueMap(input)}

	if failedVar, err := evaluateStepVars(evaluator, &scope, scenarioName, step); err != nil {
		return failStep(stepReport, start, kindVars, failedVar, err.Error())
	}

	execution, evalErr := evaluateMailExecution(evaluator, scope, scenarioName, step, state.Seed())
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
		Mail:     execution,
	})
	if err != nil {
		return failStep(stepReport, start, kindProvider, "", err.Error())
	}

	stepReport.Request = diagnostic.FromCTYMap(output.Request)
	stepReport.Response = diagnostic.FromCTYMap(output.Response)

	scope.Request = output.Request
	scope.Response = output.Response

	if step.Expect != nil {
		if expectErr := evaluateExpect(evaluator, scope, scenarioName, step, output); expectErr != nil {
			stepReport.Status = report.StatusFail
			stepReport.Failure = toErrorDetail(expectErr)
			stepReport.Duration = time.Since(start)

			return stepReport
		}
	}

	if captureErr := applyMailCapture(evaluator, scope, scenarioName, state, step, output); captureErr != nil {
		return failStep(stepReport, start, kindCapture, captureErr.path, captureErr.message)
	}

	stepReport.Duration = time.Since(start)

	return stepReport
}

// failStep stamps a terminal failure on the report and returns it.
func failStep(stepReport *report.StepResult, start time.Time, kind, path, message string) *report.StepResult {
	stepReport.Status = report.StatusFail
	stepReport.Failure = &report.ErrorDetail{Kind: kind, Path: path, Message: message}
	stepReport.Duration = time.Since(start)

	return stepReport
}

type captureError struct {
	path    string
	message string
}

// applyMailCapture evaluates the step's capture expressions against the
// provider response and records the result.
func applyMailCapture(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output) *captureError {
	resultValue := map[string]cty.Value{
		outputRequest:  cty.ObjectVal(output.Request),
		outputResponse: cty.ObjectVal(output.Response),
	}

	for key, captureExpr := range step.Capture {
		captureVal, err := evaluator.Eval(captureExpr, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: "capture." + key})
		if err != nil {
			return &captureError{path: key, message: err.Error()}
		}

		resultValue[key] = captureVal
	}

	state.SetStepResult(step.Name, cty.ObjectVal(resultValue))

	return nil
}

// evaluateMailExecution lowers the step's mail block expressions into the
// concrete payload consumed by the mail provider.
func evaluateMailExecution(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, seed int64) (*provider.MailExecution, error) {
	mc := step.Mail

	target, err := evalMailString(evaluator, scope, scenarioName, step, "target", mc.Target, true)
	if err != nil {
		return nil, err
	}

	if mc.Message == nil {
		return nil, fmt.Errorf("mail step is missing a message block")
	}

	exec, err := evaluateMailMessage(evaluator, scope, scenarioName, step, mc.Message)
	if err != nil {
		return nil, err
	}

	exec.Target = target
	exec.MessageID = resolveMessageID(exec.Headers, seed, scenarioName, step.Name)

	return exec, nil
}

func evaluateMailMessage(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, msg *model.MailMessage) (*provider.MailExecution, error) {
	from, err := evalMailString(evaluator, scope, scenarioName, step, "message.from", msg.From, true)
	if err != nil {
		return nil, err
	}

	exec := &provider.MailExecution{From: from}

	for _, field := range []struct {
		path string
		expr model.Expression
		dst  *[]string
	}{
		{"message.to", msg.To, &exec.To},
		{"message.cc", msg.Cc, &exec.Cc},
		{"message.bcc", msg.Bcc, &exec.Bcc},
	} {
		list, err := evalMailStringList(evaluator, scope, scenarioName, step, field.path, field.expr)
		if err != nil {
			return nil, err
		}

		*field.dst = list
	}

	for _, field := range []struct {
		path string
		expr model.Expression
		dst  *string
	}{
		{"message.subject", msg.Subject, &exec.Subject},
		{"message.text", msg.Text, &exec.Text},
		{"message.html", msg.HTML, &exec.HTML},
	} {
		value, err := evalMailString(evaluator, scope, scenarioName, step, field.path, field.expr, false)
		if err != nil {
			return nil, err
		}

		*field.dst = value
	}

	headers, err := evalMailStringMap(evaluator, scope, scenarioName, step, "message.headers", msg.Headers)
	if err != nil {
		return nil, err
	}

	exec.Headers = headers

	attachments, err := evaluateMailAttachments(evaluator, scope, scenarioName, step, msg.Attachments)
	if err != nil {
		return nil, err
	}

	exec.Attachments = attachments

	return exec, nil
}

func evaluateMailAttachments(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, attachments []model.MailAttachment) ([]provider.MailAttachmentData, error) {
	out := make([]provider.MailAttachmentData, 0, len(attachments))

	for i := range attachments {
		att := attachments[i]
		path := fmt.Sprintf("attachment[%d]", i)

		filename, err := evalMailString(evaluator, scope, scenarioName, step, path+".filename", att.Filename, true)
		if err != nil {
			return nil, err
		}

		contentType, err := evalMailString(evaluator, scope, scenarioName, step, path+".content_type", att.ContentType, false)
		if err != nil {
			return nil, err
		}

		data := provider.MailAttachmentData{Filename: filename, ContentType: contentType}

		if !att.Content.Empty() {
			content, err := evalMailString(evaluator, scope, scenarioName, step, path+".content", att.Content, true)
			if err != nil {
				return nil, err
			}

			data.Content = content
			data.HasContent = true
		} else {
			location, err := evalMailString(evaluator, scope, scenarioName, step, path+".path", att.Path, true)
			if err != nil {
				return nil, err
			}

			data.Path = location
		}

		out = append(out, data)
	}

	return out, nil
}

// resolveMessageID returns the user-supplied Message-ID header (case
// insensitive) or a deterministic one derived from the seed.
func resolveMessageID(headers map[string]string, seed int64, scenario, step string) string {
	for key, value := range headers {
		if strings.EqualFold(key, "message-id") && strings.TrimSpace(value) != "" {
			return value
		}
	}

	return fmt.Sprintf("<%016x@tales.local>", DeriveSeed(seed, scenario, step, "message-id"))
}

func evalMailString(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression, required bool) (string, error) {
	if expression.Empty() {
		if required {
			return "", fmt.Errorf("%s is required", path)
		}

		return "", nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path})
	if err != nil {
		return "", fmt.Errorf("evaluate %s: %w", path, err)
	}

	if value.IsNull() || !value.IsKnown() {
		if required {
			return "", fmt.Errorf("%s must be a non-null string", path)
		}

		return "", nil
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("%s must be a string, got %s", path, value.Type().FriendlyName())
	}

	return value.AsString(), nil
}

func evalMailStringList(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression) ([]string, error) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path})
	if err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", path, err)
	}

	if value.IsNull() || !value.IsKnown() {
		return nil, nil
	}

	if !value.Type().IsTupleType() && !value.Type().IsListType() {
		return nil, fmt.Errorf("%s must be a list, got %s", path, value.Type().FriendlyName())
	}

	out := make([]string, 0, value.LengthInt())

	for _, item := range value.AsValueSlice() {
		if item.IsNull() || item.Type() != cty.String {
			return nil, fmt.Errorf("%s must be a list of strings", path)
		}

		out = append(out, item.AsString())
	}

	return out, nil
}

func evalMailStringMap(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step, path string, expression model.Expression) (map[string]string, error) {
	if expression.Empty() {
		return nil, nil
	}

	value, err := evaluator.Eval(expression, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: path})
	if err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", path, err)
	}

	if value.IsNull() || !value.IsKnown() {
		return nil, nil
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("%s must be a map, got %s", path, value.Type().FriendlyName())
	}

	out := make(map[string]string, len(value.AsValueMap()))

	for key, item := range value.AsValueMap() {
		str, ok := ctyToHeaderString(item)
		if !ok {
			return nil, fmt.Errorf("%s[%q] must be a string, number or bool", path, key)
		}

		out[key] = str
	}

	return out, nil
}

// ctyToHeaderString renders a scalar cty value as a header value.
func ctyToHeaderString(value cty.Value) (string, bool) {
	if value.IsNull() || !value.IsKnown() {
		return "", false
	}

	switch value.Type() {
	case cty.String:
		return value.AsString(), true
	case cty.Bool:
		if value.True() {
			return "true", true
		}

		return "false", true
	case cty.Number:
		bf := value.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()

			return strconv.FormatInt(i, 10), true
		}

		f, _ := bf.Float64()

		return strconv.FormatFloat(f, 'f', -1, 64), true
	default:
		return "", false
	}
}
