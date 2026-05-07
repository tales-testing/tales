package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/internal/model"
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
			step.Request = &model.Request{
				Method:  expr(path, rs.Request.Method),
				URL:     expr(path, rs.Request.URL),
				Headers: expr(path, rs.Request.Headers),
				Query:   expr(path, rs.Request.Query),
				JSON:    expr(path, rs.Request.JSON),
				Body:    expr(path, rs.Request.Body),
				Timeout: expr(path, rs.Request.Timeout),
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
				Strict:  expr(path, expect.Strict),
			}
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
