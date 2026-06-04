package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// webhookProviderType is the provider label that triggers webhook step decoding.
const webhookProviderType = "webhook"

// webhookRequestExpectBlock is the schema for the expect { request { ... } }
// sub-block. gohcl rejects unknown attributes automatically.
type webhookRequestExpectBlock struct {
	Method  hcl.Expression `hcl:"method,optional"`
	Path    hcl.Expression `hcl:"path,optional"`
	Headers hcl.Expression `hcl:"headers,optional"`
	Query   hcl.Expression `hcl:"query,optional"`
	JSON    hcl.Expression `hcl:"json,optional"`
	Body    hcl.Expression `hcl:"body,optional"`
}

// webhookHMACBlock is the schema for the expect { hmac_signature { ... } }
// sub-block.
type webhookHMACBlock struct {
	Header             hcl.Expression `hcl:"header,optional"`
	Secret             hcl.Expression `hcl:"secret,optional"`
	Algorithm          hcl.Expression `hcl:"algorithm,optional"`
	Format             hcl.Expression `hcl:"format,optional"`
	Payload            hcl.Expression `hcl:"payload,optional"`
	TimestampTolerance hcl.Expression `hcl:"timestamp_tolerance,optional"`
	TimestampRequired  hcl.Expression `hcl:"timestamp_required,optional"`
}

// decodeWebhookStep builds a model.WebhookCall from a parsed step block. It
// validates that exactly one of start / wait / stop is present and that each
// operation carries its required fields.
func decodeWebhookStep(path string, rs stepBlock) (*model.WebhookCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	hasStart := rs.WebhookStart != nil
	hasWait := rs.WebhookWait != nil
	hasStop := rs.WebhookStop != nil

	count := 0

	for _, present := range []bool{hasStart, hasWait, hasStop} {
		if present {
			count++
		}
	}

	switch {
	case count == 0:
		diags = append(diags, diagError(
			"Missing webhook operation",
			"webhook step must define exactly one of start, wait, or stop.",
			nil,
		))
	case count > 1:
		diags = append(diags, diagError(
			"Conflicting webhook operation",
			"webhook step must define exactly one of start, wait, or stop.",
			nil,
		))
	}

	call := &model.WebhookCall{Target: optionalExpr(path, rs.Target)}

	if hasStart {
		diags = append(diags, decodeWebhookStart(path, rs, call)...)
	}

	if hasWait {
		diags = append(diags, decodeWebhookWait(path, rs, call)...)
	}

	if hasStop {
		diags = append(diags, decodeWebhookStop(path, rs, call)...)
	}

	expectBlk := rs.Expect
	if expectBlk == nil {
		expectBlk = rs.Response
	}

	if expectBlk != nil {
		webhookExpect, expectDiags := decodeWebhookExpect(path, expectBlk)
		diags = append(diags, expectDiags...)
		call.Expect = webhookExpect
	}

	return call, diags
}

func decodeWebhookStart(path string, rs stepBlock, call *model.WebhookCall) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)
	start := rs.WebhookStart

	if !exprIsSet(start.Path) {
		diags = append(diags, diagError(
			"Missing webhook path",
			"webhook start must declare path = \"/...\".",
			nil,
		))
	} else if literal, ok := staticString(start.Path); ok && !strings.HasPrefix(literal, "/") {
		rng := start.Path.Range()
		diags = append(diags, diagError(
			"Invalid webhook path",
			"webhook start path must start with \"/\".",
			&rng,
		))
	}

	if exprIsSet(rs.Target) {
		rng := rs.Target.Range()
		diags = append(diags, diagError(
			"Unexpected target on webhook start",
			"webhook start does not take a step-level target; target is for wait / stop.",
			&rng,
		))
	}

	call.Start = &model.WebhookStart{
		Address:      optionalExpr(path, start.Address),
		Path:         optionalExpr(path, start.Path),
		PublicURL:    optionalExpr(path, start.PublicURL),
		PublicScheme: optionalExpr(path, start.PublicScheme),
		PublicHost:   optionalExpr(path, start.PublicHost),
		PublicPort:   optionalExpr(path, start.PublicPort),
		MaxBodySize:  optionalExpr(path, start.MaxBodySize),
	}

	return diags
}

func decodeWebhookWait(path string, rs stepBlock, call *model.WebhookCall) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Target) {
		diags = append(diags, diagError(
			"Missing webhook target",
			"webhook wait must declare target = result.<start step>.id.",
			nil,
		))
	}

	call.Wait = &model.WebhookWait{
		Timeout: optionalExpr(path, rs.WebhookWait.Timeout),
		Count:   optionalExpr(path, rs.WebhookWait.Count),
	}

	return diags
}

func decodeWebhookStop(path string, rs stepBlock, call *model.WebhookCall) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.WebhookStop.Target) {
		diags = append(diags, diagError(
			"Missing webhook stop target",
			"webhook stop must declare target = result.<start step>.id.",
			nil,
		))
	}

	call.Stop = &model.WebhookStop{Target: optionalExpr(path, rs.WebhookStop.Target)}

	return diags
}

// decodeWebhookExpect decodes the request / hmac_signature blocks from the
// expect body remainder and rejects HTTP-style expect attributes that the
// webhook runtime does not interpret.
func decodeWebhookExpect(path string, expect *expectBlock) (*model.WebhookExpect, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	for _, typed := range []struct {
		name string
		expr hcl.Expression
	}{
		{expectAttrStatus, expect.Status},
		{expectAttrHeaders, expect.Headers},
		{expectAttrJSON, expect.JSON},
		{expectAttrBody, expect.Body},
		{expectAttrStrict, expect.Strict},
	} {
		if exprIsSet(typed.expr) {
			rng := typed.expr.Range()
			diags = append(diags, diagError(
				"Unsupported webhook expect attribute",
				fmt.Sprintf("webhook expect uses request { ... } and hmac_signature { ... } blocks, not %q.", typed.name),
				&rng,
			))
		}
	}

	out := &model.WebhookExpect{}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return out, diags
	}

	for name, attr := range body.Attributes {
		if typedExpectAttrs[name] {
			continue
		}

		rng := attr.NameRange
		diags = append(diags, diagError(
			"Unknown webhook expect attribute",
			fmt.Sprintf("webhook expect does not support attribute %q; use request { ... } and hmac_signature { ... }.", name),
			&rng,
		))
	}

	for _, block := range body.Blocks {
		switch block.Type {
		case "request":
			req, reqDiags := decodeWebhookRequestExpect(path, block)
			diags = append(diags, reqDiags...)
			out.Request = req
		case "hmac_signature":
			hmacExpect, hmacDiags := decodeWebhookHMAC(path, block)
			diags = append(diags, hmacDiags...)
			out.HMAC = hmacExpect
		default:
			rng := block.DefRange()
			diags = append(diags, diagError(
				"Unknown webhook expect block",
				fmt.Sprintf("webhook expect supports request and hmac_signature blocks only, found %q.", block.Type),
				&rng,
			))
		}
	}

	return out, diags
}

func decodeWebhookRequestExpect(path string, block *hclsyntax.Block) (*model.WebhookRequestExpect, hcl.Diagnostics) {
	var raw webhookRequestExpectBlock

	diags := gohcl.DecodeBody(block.Body, nil, &raw)

	return &model.WebhookRequestExpect{
		Method:  optionalExpr(path, raw.Method),
		Path:    optionalExpr(path, raw.Path),
		Headers: optionalExpr(path, raw.Headers),
		Query:   optionalExpr(path, raw.Query),
		JSON:    optionalExpr(path, raw.JSON),
		Body:    optionalExpr(path, raw.Body),
	}, diags
}

func decodeWebhookHMAC(path string, block *hclsyntax.Block) (*model.WebhookHMACExpect, hcl.Diagnostics) {
	var raw webhookHMACBlock

	diags := gohcl.DecodeBody(block.Body, nil, &raw)

	for _, required := range []struct {
		name string
		expr hcl.Expression
	}{
		{"header", raw.Header},
		{"secret", raw.Secret},
		{"payload", raw.Payload},
		{"format", raw.Format},
	} {
		if !exprIsSet(required.expr) {
			diags = append(diags, diagError(
				"Missing hmac_signature field",
				fmt.Sprintf("hmac_signature must declare %q.", required.name),
				nil,
			))
		}
	}

	return &model.WebhookHMACExpect{
		Header:             optionalExpr(path, raw.Header),
		Secret:             optionalExpr(path, raw.Secret),
		Algorithm:          optionalExpr(path, raw.Algorithm),
		Format:             optionalExpr(path, raw.Format),
		Payload:            optionalExpr(path, raw.Payload),
		TimestampTolerance: optionalExpr(path, raw.TimestampTolerance),
		TimestampRequired:  optionalExpr(path, raw.TimestampRequired),
	}, diags
}

// looksLikeWebhookStep reports whether a step block carries any webhook-only
// block, used to flag webhook fields appearing on a non-webhook provider step.
func looksLikeWebhookStep(rs stepBlock) bool {
	if rs.Provider == webhookProviderType {
		return true
	}

	return rs.WebhookStart != nil || rs.WebhookWait != nil || rs.WebhookStop != nil
}

// decodeWebhookStepIfNeeded routes webhook decoding: a webhook provider step is
// fully decoded; a non-webhook step carrying a start/wait/stop block is rejected
// with a clear hint.
func decodeWebhookStepIfNeeded(path string, rs stepBlock, stepName string) (*model.WebhookCall, hcl.Diagnostics) {
	if rs.Provider == webhookProviderType {
		return decodeWebhookStep(path, rs)
	}

	if !looksLikeWebhookStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Webhook fields on non-webhook step",
		fmt.Sprintf("Step %q uses webhook-only blocks (start, wait, or stop) but its provider is %q; use provider \"webhook\".", stepName, rs.Provider),
		nil,
	)}
}

// optionalExpr returns a populated model.Expression only when the user actually
// set the field. gohcl wraps unset optional hcl.Expression fields with a
// non-nil zero-range placeholder, so calling expr() unconditionally would make
// model.Expression.Empty() report false for fields the user never wrote, and
// the runtime would then evaluate (and assert on) absent expectations.
func optionalExpr(path string, e hcl.Expression) model.Expression {
	if !exprIsSet(e) {
		return model.Expression{}
	}

	return expr(path, e)
}

// staticString returns the literal string value of an expression when it can be
// evaluated with no context (no variables / functions), used for best-effort
// load-time validation of literals like the receiver path.
func staticString(e hcl.Expression) (string, bool) {
	if e == nil {
		return "", false
	}

	value, diags := e.Value(nil)
	if diags.HasErrors() || value.IsNull() || !value.IsKnown() {
		return "", false
	}

	if value.Type() != cty.String {
		return "", false
	}

	return value.AsString(), true
}
