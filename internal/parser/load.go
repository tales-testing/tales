package parser

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// typedExpectAttrs are the attribute names already consumed by typed
// fields on expectBlock. They appear in the remain body too (hclsyntax
// only hides them from schema-driven reads, not from raw iteration),
// so the load decoder and the non-load validator must skip them.
var typedExpectAttrs = map[string]bool{
	"status":  true,
	"headers": true,
	"json":    true,
	"body":    true,
	"strict":  true,
}

// loadExpectShortcuts is the set of attribute names a load step is
// allowed to declare at the top level of its expect block. Each maps
// 1:1 to a key the load provider exposes on its response value, so
// the runtime assertion is a straight `MatchJSON(expr, response[k])`.
var loadExpectShortcuts = map[string]bool{
	"requests":         true,
	"errors":           true,
	"duration_ms":      true,
	"rps":              true,
	"error_ratio":      true,
	"status_2xx_ratio": true,
	"status_3xx_ratio": true,
	"status_4xx_ratio": true,
	"status_5xx_ratio": true,
	"p50":              true,
	"p90":              true,
	"p95":              true,
	"p99":              true,
	"min":              true,
	"max":              true,
	"mean":             true,
}

// decodeLoadExpect walks the load step's expect body remainder and
// turns every recognized shortcut attribute into a model.ExpectShortcut.
// Unknown attributes are rejected with a typed diagnostic.
func decodeLoadExpect(path string, expect *expectBlock, out *model.Expect) hcl.Diagnostics {
	if expect == nil || expect.Remainder == nil {
		return nil
	}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return nil
	}

	if len(body.Attributes) == 0 {
		return nil
	}

	var diags hcl.Diagnostics

	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		attrs = append(attrs, a)
	}

	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].Range().Start.Byte < attrs[j].Range().Start.Byte
	})

	for _, attr := range attrs {
		if typedExpectAttrs[attr.Name] {
			continue
		}

		if !loadExpectShortcuts[attr.Name] {
			rng := attr.NameRange

			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Unknown load expect attribute",
				Detail: fmt.Sprintf(
					"load expect supports: requests, errors, duration_ms, rps, error_ratio, "+
						"status_2xx_ratio, status_3xx_ratio, status_4xx_ratio, status_5xx_ratio, "+
						"p50, p90, p95, p99, min, max, mean. Got %q.",
					attr.Name,
				),
				Subject: &rng,
			})

			continue
		}

		out.Shortcuts = append(out.Shortcuts, model.ExpectShortcut{
			Name:     attr.Name,
			Expected: expr(path, attr.Expr),
		})
	}

	return diags
}

// validateExpectExtras rejects any attribute that appears in
// expect.Remainder for a non-load provider. The full attribute name
// is surfaced so users can fix the typo without having to consult
// docs.
func validateExpectExtras(expect *expectBlock) hcl.Diagnostics {
	if expect == nil || expect.Remainder == nil {
		return nil
	}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return nil
	}

	if len(body.Attributes) == 0 {
		return nil
	}

	var diags hcl.Diagnostics

	for name, attr := range body.Attributes {
		if typedExpectAttrs[name] {
			continue
		}

		rng := attr.NameRange

		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Unknown expect attribute",
			Detail:   fmt.Sprintf("expect does not support attribute %q on this provider; flat shortcuts like p95 are only allowed in load steps.", name),
			Subject:  &rng,
		})
	}

	return diags
}

// loadProviderType is the provider label that triggers load step decoding.
const loadProviderType = "load"

// decodeLoadStep builds a model.LoadCall from a parsed step block. The
// caller has already verified rs.Provider == loadProviderType.
func decodeLoadStep(path string, rs stepBlock) (*model.LoadCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if rs.HTTPReq == nil {
		diags = append(diags, diagError(
			"Missing http block",
			"load step must declare an http { ... } block describing the request to replay.",
			nil,
		))
	}

	if rs.Run == nil {
		diags = append(diags, diagError(
			"Missing run block",
			"load step must declare a run { ... } block with duration or requests, concurrency, optional rate and warmup.",
			nil,
		))
	}

	call := &model.LoadCall{}

	if rs.HTTPReq != nil {
		auth, authDiags := decodeRequestAuth(path, rs.HTTPReq.Auth)
		diags = append(diags, authDiags...)
		body, bodyDiags := decodeRequestBody(path, rs.HTTPReq.Body)
		diags = append(diags, bodyDiags...)

		call.Request = &model.Request{
			Method:  expr(path, rs.HTTPReq.Method),
			URL:     expr(path, rs.HTTPReq.URL),
			Headers: expr(path, rs.HTTPReq.Headers),
			Query:   expr(path, rs.HTTPReq.Query),
			Body:    body,
			Timeout: expr(path, rs.HTTPReq.Timeout),
			Auth:    auth,
		}
	}

	if rs.Run != nil {
		runModel, runDiags := decodeLoadRun(path, rs.Run)
		diags = append(diags, runDiags...)
		call.Run = runModel
	}

	return call, diags
}

// decodeLoadRun converts a parsed run block into the model layer,
// rejecting conflicting / missing duration/requests modes inline.
func decodeLoadRun(path string, raw *runBlock) (*model.LoadRun, hcl.Diagnostics) {
	hasDuration := exprIsSet(raw.Duration)
	hasRequests := exprIsSet(raw.Requests)

	var diags hcl.Diagnostics

	switch {
	case hasDuration && hasRequests:
		diags = append(diags, diagError(
			"Conflicting run mode",
			"load run block must define exactly one of duration or requests.",
			nil,
		))
	case !hasDuration && !hasRequests:
		diags = append(diags, diagError(
			"Missing run mode",
			"load run block must define duration = \"...\" or requests = N.",
			nil,
		))
	}

	out := &model.LoadRun{}

	if hasDuration {
		out.Duration = expr(path, raw.Duration)
	}

	if hasRequests {
		out.Requests = expr(path, raw.Requests)
	}

	if exprIsSet(raw.Concurrency) {
		out.Concurrency = expr(path, raw.Concurrency)
	}

	if exprIsSet(raw.Rate) {
		out.Rate = expr(path, raw.Rate)
	}

	if exprIsSet(raw.Warmup) {
		out.Warmup = expr(path, raw.Warmup)
	}

	return out, diags
}

// looksLikeLoadStep reports whether a step block carries any load-only
// attribute. Used to reject load-only fields on a non-load provider.
func looksLikeLoadStep(rs stepBlock) bool {
	return rs.Provider == loadProviderType || rs.HTTPReq != nil || rs.Run != nil
}

// decodeLoadStepIfNeeded routes load decoding similarly to
// decodeSQLStepIfNeeded.
func decodeLoadStepIfNeeded(path string, rs stepBlock, stepName string) (*model.LoadCall, hcl.Diagnostics) {
	if rs.Provider == loadProviderType {
		return decodeLoadStep(path, rs)
	}

	if !looksLikeLoadStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Load fields on non-load step",
		fmt.Sprintf("Step %q uses load-only blocks (http or run) but its provider is %q; use provider \"load\".", stepName, rs.Provider),
		nil,
	)}
}
