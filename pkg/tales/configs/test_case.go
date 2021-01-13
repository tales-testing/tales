package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/pkg/tales/reporter"
)

// TestCase interface
type TestCase interface {
	Parse(c *Case, ctx *hcl.EvalContext) hcl.Diagnostics
	Execute(ctx *hcl.EvalContext) *reporter.Case
	Result() *reporter.Case
}
