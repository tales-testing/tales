package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/internal/model"
)

// sqlProviderType is the provider label that triggers SQL step decoding.
const sqlProviderType = "sql"

// decodeSQLStep builds a model.SQLCall from a parsed step block whenever the
// step uses the sql provider. It returns nil when the step is not a SQL step.
func decodeSQLStep(path string, rs stepBlock) (*model.SQLCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Connection) {
		diags = append(diags, diagError(
			"Missing SQL connection",
			"sql step must declare connection = \"<name>\" referencing config.sql.connections.<name>.",
			nil,
		))
	}

	hasExec := rs.Exec != nil
	hasQuery := rs.Query != nil

	switch {
	case hasExec && hasQuery:
		diags = append(diags, diagError(
			"Conflicting SQL operation",
			"sql step must define exactly one of exec or query.",
			nil,
		))
	case !hasExec && !hasQuery:
		diags = append(diags, diagError(
			"Missing SQL operation",
			"sql step must define exec or query.",
			nil,
		))
	}

	call := &model.SQLCall{Connection: expr(path, rs.Connection)}

	if rs.Exec != nil {
		if !exprIsSet(rs.Exec.SQL) {
			diags = append(diags, diagError(
				"Missing exec.sql",
				"sql step exec block must define sql = \"<statement>\".",
				nil,
			))
		}

		call.Exec = &model.SQLOp{SQL: expr(path, rs.Exec.SQL), Args: expr(path, rs.Exec.Args)}
	}

	if rs.Query != nil {
		if !exprIsSet(rs.Query.SQL) {
			diags = append(diags, diagError(
				"Missing query.sql",
				"sql step query block must define sql = \"<statement>\".",
				nil,
			))
		}

		call.Query = &model.SQLOp{SQL: expr(path, rs.Query.SQL), Args: expr(path, rs.Query.Args)}
	}

	return call, diags
}

// looksLikeSQLStep reports whether a step block carries any SQL-specific
// attribute or block. Used to flag SQL-only fields appearing on a non-sql
// provider step.
func looksLikeSQLStep(rs stepBlock) bool {
	if rs.Provider == sqlProviderType {
		return true
	}

	if exprIsSet(rs.Connection) {
		return true
	}

	if rs.Exec != nil || rs.Query != nil {
		return true
	}

	return false
}

// decodeSQLStepIfNeeded routes SQL decoding similarly to
// decodeMobileStepIfNeeded: a sql provider step is fully decoded; a non-sql
// step that nonetheless carries SQL-only fields is rejected with a clear
// migration hint.
func decodeSQLStepIfNeeded(path string, rs stepBlock, stepName string) (*model.SQLCall, hcl.Diagnostics) {
	if rs.Provider == sqlProviderType {
		return decodeSQLStep(path, rs)
	}

	if !looksLikeSQLStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"SQL fields on non-sql step",
		fmt.Sprintf("Step %q uses sql-only fields (connection, exec, or query) but its provider is %q; use provider \"sql\".", stepName, rs.Provider),
		nil,
	)}
}
