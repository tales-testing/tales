package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// execProviderType is the provider label that triggers exec step decoding.
const execProviderType = "exec"

const (
	execExpectExitCode   = "exit_code"
	execExpectStdout     = "stdout"
	execExpectStderr     = "stderr"
	execExpectStdoutJSON = "stdout_json"
)

// looksLikeExecStep reports whether a step block carries exec-only fields
// (command or a sandbox block), used to flag them on a non-exec provider.
func looksLikeExecStep(rs stepBlock) bool {
	return rs.Provider == execProviderType || exprIsSet(rs.Command) || rs.Sandbox != nil
}

// decodeExecStepIfNeeded routes exec decoding: an exec provider step is fully
// decoded; a non-exec step carrying command/sandbox is rejected.
func decodeExecStepIfNeeded(path string, rs stepBlock, stepName string) (*model.ExecCall, hcl.Diagnostics) {
	if rs.Provider == execProviderType {
		return decodeExecStep(path, rs)
	}

	if !looksLikeExecStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Exec fields on non-exec step",
		fmt.Sprintf("Step %q uses exec-only fields (command or sandbox) but its provider is %q; use provider \"exec\".", stepName, rs.Provider),
		nil,
	)}
}

// decodeExecStep builds a model.ExecCall from a parsed step block. command is
// required; args / env / stdin / timeout / sandbox / expect are optional.
func decodeExecStep(path string, rs stepBlock) (*model.ExecCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Command) {
		diags = append(diags, diagError(
			"Missing exec command",
			"exec step must declare command = \"<program>\".",
			nil,
		))
	}

	call := &model.ExecCall{
		Command: optionalExpr(path, rs.Command),
		Args:    optionalExpr(path, rs.Args),
		Env:     optionalExpr(path, rs.Env),
		Stdin:   optionalExpr(path, rs.Stdin),
		Timeout: optionalExpr(path, rs.Timeout),
	}

	if rs.Sandbox != nil {
		call.Sandbox = &model.ExecSandbox{
			Mode:    optionalExpr(path, rs.Sandbox.Mode),
			Workdir: optionalExpr(path, rs.Sandbox.Workdir),
			Env:     optionalExpr(path, rs.Sandbox.Env),
			Network: optionalExpr(path, rs.Sandbox.Network),
		}
	}

	expectBlk := rs.Expect
	if expectBlk == nil {
		expectBlk = rs.Response
	}

	if expectBlk != nil {
		execExpect, expectDiags := decodeExecExpect(path, expectBlk)
		diags = append(diags, expectDiags...)
		call.Expect = execExpect
	}

	return call, diags
}

// decodeExecExpect decodes the exec-specific expect attributes (exit_code,
// stdout, stderr, stdout_json), all from the body remainder. Every typed
// HTTP-style attribute (status / headers / json / body / strict) is rejected,
// as is any unknown remainder attribute.
func decodeExecExpect(path string, expect *expectBlock) (*model.ExecExpect, hcl.Diagnostics) {
	diags := rejectHTTPExpectAttrs(expect, execProviderType)

	if exprIsSet(expect.JSON) {
		rng := expect.JSON.Range()
		diags = append(diags, diagError(
			"Unsupported expect attribute",
			"exec expect does not support \"json\"; use stdout_json.",
			&rng,
		))
	}

	out := &model.ExecExpect{}

	body, ok := expect.Remainder.(*hclsyntax.Body)
	if !ok {
		return out, diags
	}

	for name, attr := range body.Attributes {
		if typedExpectAttrs[name] {
			continue
		}

		switch name {
		case execExpectExitCode:
			out.ExitCode = expr(path, attr.Expr)
		case execExpectStdout:
			out.Stdout = expr(path, attr.Expr)
		case execExpectStderr:
			out.Stderr = expr(path, attr.Expr)
		case execExpectStdoutJSON:
			out.StdoutJSON = expr(path, attr.Expr)
		default:
			rng := attr.NameRange
			diags = append(diags, diagError(
				"Unknown exec expect attribute",
				fmt.Sprintf("exec expect supports exit_code, stdout, stderr and stdout_json; got %q.", name),
				&rng,
			))
		}
	}

	return out, diags
}
