package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// rpcProviderType is the step provider label that triggers rpc decoding.
const rpcProviderType = "rpc"

// rpc-specific attribute names walked from expect.Remainder. The shared
// typed expect block already carries status and headers; HTTP-typed
// attributes (json, body, strict) are rejected.
const (
	rpcExpectAttrError    = "error"
	rpcExpectAttrMessage  = "message"
	rpcExpectAttrMetadata = "metadata"
	rpcExpectAttrTrailers = "trailers"
)

//nolint:gochecknoglobals // immutable lookup table; effectively a const
var rpcExpectAttributes = map[string]struct{}{
	rpcExpectAttrError:    {},
	rpcExpectAttrMessage:  {},
	rpcExpectAttrMetadata: {},
	rpcExpectAttrTrailers: {},
}

// decodeRPCStepIfNeeded routes rpc decoding: an rpc provider step is fully
// decoded; a non-rpc step that carries a call { } block is rejected with a
// clear hint pointing at the provider mismatch.
func decodeRPCStepIfNeeded(path string, rs stepBlock, stepName string) (*model.RPCCall, hcl.Diagnostics) {
	if rs.Provider == rpcProviderType {
		return decodeRPCStep(path, rs)
	}

	if !looksLikeRPCStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Call block on non-rpc step",
		fmt.Sprintf("Step %q uses an rpc-only call { } block but its provider is %q; use provider \"rpc\".", stepName, rs.Provider),
		nil,
	)}
}

// looksLikeRPCStep reports whether a step carries an rpc-only block. Used
// by the dispatcher in decodeProviderSteps to flag cross-provider misuse.
func looksLikeRPCStep(rs stepBlock) bool {
	if rs.Provider == rpcProviderType {
		return true
	}

	return rs.RPCCall != nil
}

// decodeRPCStep builds a model.RPCCall from a parsed rpc step. Required
// fields: step.target, call { }, call.service, call.method. Optional:
// call.message (default empty message), call.headers, call.metadata,
// call.timeout. The expect block has its own decoder; nil is fine.
func decodeRPCStep(path string, rs stepBlock) (*model.RPCCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Target) {
		diags = append(diags, diagError(
			"Missing rpc target",
			"rpc step must declare target = \"<target-name>\".",
			nil,
		))
	}

	if rs.RPCCall == nil {
		diags = append(diags, diagError(
			"Missing rpc call block",
			"rpc step must declare a call { service = ...; method = ... } block.",
			nil,
		))

		return &model.RPCCall{Target: optionalExpr(path, rs.Target)}, diags
	}

	call := rs.RPCCall

	if !exprIsSet(call.Service) {
		diags = append(diags, diagError(
			"Missing rpc service",
			"rpc call must declare service = \"<package.Service>\".",
			nil,
		))
	}

	if !exprIsSet(call.Method) {
		diags = append(diags, diagError(
			"Missing rpc method",
			"rpc call must declare method = \"<Method>\".",
			nil,
		))
	}

	out := &model.RPCCall{
		Target:   optionalExpr(path, rs.Target),
		Service:  optionalExpr(path, call.Service),
		Method:   optionalExpr(path, call.Method),
		Message:  optionalExpr(path, call.Message),
		Headers:  optionalExpr(path, call.Headers),
		Metadata: optionalExpr(path, call.Metadata),
		Timeout:  optionalExpr(path, call.Timeout),
	}

	expectBlk := rs.Expect
	if expectBlk == nil {
		expectBlk = rs.Response
	}

	if expectBlk != nil {
		rpcExpect, expectDiags := decodeRPCExpect(path, expectBlk)
		diags = append(diags, expectDiags...)
		out.Expect = rpcExpect
	}

	return out, diags
}

// decodeRPCExpect parses the rpc-specific expect surface. Typed status /
// headers fields from the shared expectBlock are reused (status is the
// canonical lowercase string for rpc, headers is the response header map).
// JSON, Body, and Strict are rejected because they are HTTP-only. The
// Remainder carries the rpc-specific attributes (error, message, metadata,
// trailers).
func decodeRPCExpect(path string, expect *expectBlock) (*model.RPCExpect, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	for _, typed := range []struct {
		name string
		expr hcl.Expression
	}{
		{expectAttrJSON, expect.JSON},
		{expectAttrBody, expect.Body},
		{expectAttrStrict, expect.Strict},
	} {
		if exprIsSet(typed.expr) {
			rng := typed.expr.Range()
			diags = append(diags, diagError(
				"Unsupported rpc expect attribute",
				fmt.Sprintf("rpc expect uses status / error / message / headers / metadata / trailers, not %q.", typed.name),
				&rng,
			))
		}
	}

	out := &model.RPCExpect{
		Status:  optionalExpr(path, expect.Status),
		Headers: optionalExpr(path, expect.Headers),
	}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return out, diags
	}

	for name, attr := range body.Attributes {
		if typedExpectAttrs[name] {
			continue
		}

		if _, allowed := rpcExpectAttributes[name]; !allowed {
			rng := attr.NameRange
			diags = append(diags, diagError(
				"Unknown rpc expect attribute",
				fmt.Sprintf("rpc expect does not support %q; use status, error, message, headers, metadata, or trailers.", name),
				&rng,
			))

			continue
		}

		expression := model.Expression{Expr: attr.Expr, File: path, Line: attr.Range().Start.Line}

		switch name {
		case rpcExpectAttrError:
			out.Error = expression
		case rpcExpectAttrMessage:
			out.Message = expression
		case rpcExpectAttrMetadata:
			out.Metadata = expression
		case rpcExpectAttrTrailers:
			out.Trailers = expression
		}
	}

	for _, block := range body.Blocks {
		rng := block.DefRange()
		diags = append(diags, diagError(
			"Unknown rpc expect block",
			fmt.Sprintf("rpc expect does not support block %q; use attributes only.", block.Type),
			&rng,
		))
	}

	return out, diags
}
