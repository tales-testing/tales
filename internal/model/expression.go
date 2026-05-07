package model

import "github.com/hashicorp/hcl/v2"

// Expression wraps HCL expression with source metadata.
type Expression struct {
	Expr hcl.Expression
	File string
	Line int
}

// Empty returns true when expression is not set.
func (e Expression) Empty() bool {
	return e.Expr == nil
}
