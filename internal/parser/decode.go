package parser

import (
	"fmt"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

func decodeFile(path string, body hcl.Body) (*model.Suite, hcl.Diagnostics) {
	var raw fileSchema

	diags := gohcl.DecodeBody(body, nil, &raw)
	if diags.HasErrors() {
		return nil, diags
	}

	suite := &model.Suite{
		Version:    raw.Version,
		Files:      []string{path},
		ConfigExpr: map[string]model.Expression{},
		Generators: map[string]*model.Generator{},
		Keywords:   map[string]*model.Keyword{},
	}

	for _, cfg := range raw.Config {
		attrs, attrDiags := cfg.Body.JustAttributes()
		diags = append(diags, attrDiags...)

		for name, attr := range attrs {
			suite.ConfigExpr[name] = model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
		}
	}

	for _, gen := range raw.Generators {
		params, pDiags := bodyToNamedExprMap(path, gen.Body)
		diags = append(diags, pDiags...)
		suite.Generators[gen.Name] = &model.Generator{
			Type:   gen.Type,
			Name:   gen.Name,
			File:   path,
			Params: params,
		}
	}

	for _, kw := range raw.Keywords {
		inputs := map[string]model.Expression{}

		if kw.InputsBlock != nil {
			var iDiags hcl.Diagnostics

			inputs, iDiags = bodyToNamedExprMap(path, kw.InputsBlock.Body)
			diags = append(diags, iDiags...)
		}

		outputs := map[string]model.Expression{}

		if kw.Outputs != nil {
			var oDiags hcl.Diagnostics

			outputs, oDiags = bodyToNamedExprMap(path, kw.Outputs.Body)
			diags = append(diags, oDiags...)
		}

		steps, sDiags := decodeSteps(path, append(kw.Steps, kw.Cases...))
		diags = append(diags, sDiags...)

		suite.Keywords[kw.Name] = &model.Keyword{
			Name:    kw.Name,
			File:    path,
			Inputs:  inputs,
			Steps:   steps,
			Outputs: outputs,
		}
	}

	for _, sc := range raw.Scenarios {
		normalSteps, sDiags := decodeSteps(path, append(sc.Steps, sc.Cases...))
		diags = append(diags, sDiags...)

		teardownSteps := make([]*model.Step, 0)

		for _, td := range sc.Teardowns {
			steps, tDiags := decodeSteps(path, append(td.Steps, td.Cases...))
			diags = append(diags, tDiags...)
			teardownSteps = append(teardownSteps, steps...)
		}

		suite.Scenarios = append(suite.Scenarios, &model.Scenario{
			Name:     sc.Name,
			Tags:     sc.Tags,
			File:     path,
			Steps:    normalSteps,
			Teardown: teardownSteps,
		})
	}

	return suite, diags
}

func decodeSteps(path string, rawSteps []stepBlock) ([]*model.Step, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	steps := make([]*model.Step, 0, len(rawSteps))

	for _, rs := range rawSteps {
		step := &model.Step{
			Provider:  rs.Provider,
			Name:      rs.Name,
			File:      path,
			DependsOn: append([]string(nil), rs.DependsOn...),
			When:      expr(path, rs.When),
			Capture:   map[string]model.Expression{},
		}
		if rs.When != nil {
			step.Line = rs.When.Range().Start.Line
		}

		if step.Line == 0 {
			step.Line = 1
		}

		if rs.Request != nil {
			auth, authDiags := decodeRequestAuth(path, rs.Request.Auth)
			diags = append(diags, authDiags...)

			step.Request = &model.Request{
				Method:  expr(path, rs.Request.Method),
				URL:     expr(path, rs.Request.URL),
				Headers: expr(path, rs.Request.Headers),
				Query:   expr(path, rs.Request.Query),
				JSON:    expr(path, rs.Request.JSON),
				Body:    expr(path, rs.Request.Body),
				Timeout: expr(path, rs.Request.Timeout),
				Auth:    auth,
			}
		}

		expect := rs.Expect
		if expect == nil {
			expect = rs.Response
		}

		if expect != nil {
			step.Expect = &model.Expect{
				Status:  expr(path, expect.Status),
				Headers: expr(path, expect.Headers),
				JSON:    expr(path, expect.JSON),
				Body:    expr(path, expect.Body),
				Strict:  expr(path, expect.Strict),
			}
		}

		if rs.Retry != nil {
			retry, rDiags := decodeRetry(rs.Retry)
			diags = append(diags, rDiags...)
			step.Retry = retry
		}

		if rs.Capture != nil {
			capture, cDiags := bodyToNamedExprMap(path, rs.Capture.Body)
			diags = append(diags, cDiags...)
			step.Capture = capture
		}

		if rs.Provider == "keyword" {
			step.Keyword = &model.KeywordCall{
				Name:   expr(path, rs.CallName),
				Inputs: expr(path, rs.Inputs),
			}
		}

		if step.Provider == "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Missing step provider",
				Detail:   fmt.Sprintf("Step %q has no provider label.", step.Name),
			})
		}

		steps = append(steps, step)
	}

	return steps, diags
}

func decodeRequestAuth(path string, raw []authBlock) (*model.RequestAuth, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	if len(raw) == 0 {
		return nil, diags
	}

	if len(raw) > 1 {
		diags = append(diags, diagError("Duplicate auth block", "request supports at most one auth block.", nil))
	}

	auth := &model.RequestAuth{}
	first := raw[0]

	if len(first.Basic) == 0 {
		diags = append(diags, diagError("Missing auth scheme", "auth block must contain a basic block.", nil))

		return auth, diags
	}

	if len(first.Basic) > 1 {
		diags = append(diags, diagError("Duplicate basic auth block", "auth supports at most one basic block.", nil))
	}

	attrs, attrDiags := first.Basic[0].Body.JustAttributes()
	diags = append(diags, attrDiags...)

	usernameAttr, hasUsername := attrs["username"]
	if !hasUsername {
		diags = append(diags, diagError("Missing basic auth username", "auth.basic.username is required.", nil))
	}

	passwordAttr, hasPassword := attrs["password"]
	if !hasPassword {
		diags = append(diags, diagError("Missing basic auth password", "auth.basic.password is required.", nil))
	}

	auth.Basic = &model.BasicAuth{
		Username: attrExpr(path, usernameAttr),
		Password: attrExpr(path, passwordAttr),
	}

	return auth, diags
}

func attrExpr(path string, attr *hcl.Attribute) model.Expression {
	if attr == nil {
		return model.Expression{}
	}

	return model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
}

func decodeRetry(raw *retryBlock) (*model.Retry, hcl.Diagnostics) {
	retry := &model.Retry{Attempts: 1}
	diags := make(hcl.Diagnostics, 0, 2)

	if raw.Attempts == nil && raw.Interval == nil {
		return retry, diags
	}

	attempts, attemptsDiags := decodeRetryAttempts(raw.Attempts)

	diags = append(diags, attemptsDiags...)
	if !attemptsDiags.HasErrors() {
		retry.Attempts = attempts
	}

	interval, intervalDiags := decodeRetryInterval(raw.Interval)

	diags = append(diags, intervalDiags...)
	if !intervalDiags.HasErrors() {
		retry.Interval = interval
	}

	return retry, diags
}

func decodeRetryAttempts(expression hcl.Expression) (int, hcl.Diagnostics) {
	if expression == nil {
		return 1, nil
	}

	value, diags := expression.Value(nil)
	if diags.HasErrors() {
		return 1, diags
	}

	attempts, err := numberToInt(value)
	if err != nil {
		rangeValue := expression.Range()

		return 1, hcl.Diagnostics{diagError("Invalid retry attempts", err.Error(), &rangeValue)}
	}

	if attempts < 1 {
		rangeValue := expression.Range()

		return 1, hcl.Diagnostics{diagError("Invalid retry attempts", "retry.attempts must be greater than or equal to 1.", &rangeValue)}
	}

	return attempts, nil
}

func decodeRetryInterval(expression hcl.Expression) (time.Duration, hcl.Diagnostics) {
	if expression == nil {
		return 0, nil
	}

	value, diags := expression.Value(nil)
	if diags.HasErrors() {
		return 0, diags
	}

	interval, err := stringToDuration(value)
	if err != nil {
		rangeValue := expression.Range()

		return 0, hcl.Diagnostics{diagError("Invalid retry interval", err.Error(), &rangeValue)}
	}

	return interval, nil
}

func numberToInt(value cty.Value) (int, error) {
	if value.Type() != cty.Number {
		return 0, fmt.Errorf("retry.attempts must be a number")
	}

	attempts, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		return 0, fmt.Errorf("retry.attempts must be an integer")
	}

	return int(attempts), nil
}

func stringToDuration(value cty.Value) (time.Duration, error) {
	if value.Type() != cty.String {
		return 0, fmt.Errorf("retry.interval must be a duration string")
	}

	interval, err := time.ParseDuration(value.AsString())
	if err != nil {
		return 0, fmt.Errorf("retry.interval must be a valid duration: %w", err)
	}

	return interval, nil
}

func bodyToNamedExprMap(path string, body hcl.Body) (map[string]model.Expression, hcl.Diagnostics) {
	attrs, diags := body.JustAttributes()

	res := make(map[string]model.Expression, len(attrs))
	for name, attr := range attrs {
		res[name] = model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
	}

	return res, diags
}

func expr(path string, e hcl.Expression) model.Expression {
	if e == nil {
		return model.Expression{}
	}

	return model.Expression{Expr: e, File: path, Line: e.Range().Start.Line}
}
