package tdiags

import (
	"github.com/hashicorp/hcl/v2"
)

// Diagnostic interface
type Diagnostic interface {
	Severity() Severity
	Description() Description
	Source() Source

	// FromExpr returns the expression-related context for the diagnostic, if
	// available. Returns nil if the diagnostic is not related to an
	// expression evaluation.
	FromExpr() *FromExpr
}

// Severity type
type Severity rune

//go:generate go run golang.org/x/tools/cmd/stringer -type=Severity

// Severity enums
const (
	Error   Severity = 'E'
	Warning Severity = 'W'
)

// Description struct
type Description struct {
	Summary string
	Detail  string
}

// Source struct
type Source struct {
	Subject *SourceRange
	Context *SourceRange
}

// FromExpr struct
type FromExpr struct {
	Expression  hcl.Expression
	EvalContext *hcl.EvalContext
}
