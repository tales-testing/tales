package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// fileProviderType is the provider label that triggers file step decoding.
const fileProviderType = "file"

const (
	fileExpectExists    = "exists"
	fileExpectSizeBytes = "size_bytes"
	fileExpectText      = "text"
)

// fileHashAttrs are the hash-digest expect attributes a file step accepts in
// its expect body remainder; each maps to a lang.HashHex algorithm name.
var fileHashAttrs = map[string]bool{
	"sha1":       true,
	"sha224":     true,
	"sha256":     true,
	"sha384":     true,
	"sha512":     true,
	"sha512_224": true,
	"sha512_256": true,
}

// looksLikeFileStep reports whether a step block carries a step-level path,
// the file-only attribute, used to flag it appearing on a non-file provider.
func looksLikeFileStep(rs stepBlock) bool {
	return rs.Provider == fileProviderType || exprIsSet(rs.Path)
}

// decodeFileStepIfNeeded routes file decoding: a file provider step is fully
// decoded; a non-file step carrying a step-level path is rejected.
func decodeFileStepIfNeeded(path string, rs stepBlock, stepName string) (*model.FileCall, hcl.Diagnostics) {
	if rs.Provider == fileProviderType {
		return decodeFileStep(path, rs)
	}

	if !looksLikeFileStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Path on non-file step",
		fmt.Sprintf("Step %q sets a step-level path, which is file-only; use provider \"file\".", stepName),
		nil,
	)}
}

// decodeFileStep builds a model.FileCall from a parsed step block. path is
// required; the expect block (request{} / hmac excluded) is decoded into the
// file-specific assertions.
func decodeFileStep(path string, rs stepBlock) (*model.FileCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Path) {
		diags = append(diags, diagError(
			"Missing file path",
			"file step must declare path = \"<path>\".",
			nil,
		))
	}

	call := &model.FileCall{Path: optionalExpr(path, rs.Path)}

	expectBlk := rs.Expect
	if expectBlk == nil {
		expectBlk = rs.Response
	}

	if expectBlk != nil {
		fileExpect, expectDiags := decodeFileExpect(path, expectBlk)
		diags = append(diags, expectDiags...)
		call.Expect = fileExpect
	}

	return call, diags
}

// decodeFileExpect decodes the file-specific expect attributes. json is the
// typed expect.JSON field; exists / size_bytes / text / sha* live in the
// remainder. HTTP-style expect attributes (status / headers / body / strict)
// and any unknown remainder attribute are rejected.
func decodeFileExpect(path string, expect *expectBlock) (*model.FileExpect, hcl.Diagnostics) {
	diags := rejectHTTPExpectAttrs(expect, fileProviderType)

	out := &model.FileExpect{
		JSON:   optionalExpr(path, expect.JSON),
		Hashes: map[string]model.Expression{},
	}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return out, diags
	}

	for name, attr := range body.Attributes {
		if typedExpectAttrs[name] {
			continue
		}

		switch {
		case name == fileExpectExists:
			out.Exists = expr(path, attr.Expr)
		case name == fileExpectSizeBytes:
			out.SizeBytes = expr(path, attr.Expr)
		case name == fileExpectText:
			out.Text = expr(path, attr.Expr)
		case fileHashAttrs[name]:
			out.Hashes[name] = expr(path, attr.Expr)
		default:
			rng := attr.NameRange
			diags = append(diags, diagError(
				"Unknown file expect attribute",
				fmt.Sprintf("file expect supports exists, size_bytes, text, json and the sha* digests; got %q.", name),
				&rng,
			))
		}
	}

	return out, diags
}

// rejectHTTPExpectAttrs emits a diagnostic for each HTTP-style typed expect
// attribute (status / headers / body / strict) declared on a provider that
// uses a custom expect surface. json is intentionally allowed (file uses it).
func rejectHTTPExpectAttrs(expect *expectBlock, providerType string) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)

	for _, typed := range []struct {
		name string
		expr hcl.Expression
	}{
		{expectAttrStatus, expect.Status},
		{expectAttrHeaders, expect.Headers},
		{expectAttrBody, expect.Body},
		{expectAttrStrict, expect.Strict},
	} {
		if exprIsSet(typed.expr) {
			rng := typed.expr.Range()
			diags = append(diags, diagError(
				"Unsupported expect attribute",
				fmt.Sprintf("%s expect does not support %q.", providerType, typed.name),
				&rng,
			))
		}
	}

	return diags
}
