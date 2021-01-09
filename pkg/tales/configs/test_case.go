package configs

import "github.com/hashicorp/hcl/v2"

// TestCase interface
type TestCase interface {
	Parse(c *Case, ctx *hcl.EvalContext) hcl.Diagnostics
	Execute(ctx *hcl.EvalContext) Result
	Result() Result
}
