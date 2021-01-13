package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/pkg/tales/reporter"
	"github.com/zclconf/go-cty/cty"
)

// KeywordCase struct
type KeywordCase struct {
	Name    string
	Args    []*Arg `hcl:"arg,block"`
	Module  *Module
	Keyword *Keyword
}

// Parse implements TestCase
func (r *KeywordCase) Parse(c *Case, ctx *hcl.EvalContext) hcl.Diagnostics {
	r.Name = c.Name

	if diag := gohcl.DecodeBody(c.HCL, ctx, r); diag.HasErrors() {
		return diag
	}

	args := map[string]cty.Value{}

	for _, arg := range r.Args {
		args[arg.Name] = arg.Value
	}

	ctx.Variables["arg"] = cty.ObjectVal(args)

	r.Keyword.Case = engineExec(r.Module, r.Keyword.CaseConfig, ctx)

	return nil
}

// Result implements TestCase
func (r *KeywordCase) Result() *reporter.Case {
	return r.Keyword.Case.Result()
}

// Execute implements TestCase
func (r *KeywordCase) Execute(ctx *hcl.EvalContext) (result *reporter.Case) {
	result = r.Keyword.Case.Execute(ctx)

	outputs := &KeywordOutput{}

	if diag := gohcl.DecodeBody(r.Keyword.HCL, ctx, outputs); diag.HasErrors() {
		result.FromError(diag)

		return result
	}

	r.Keyword.Outputs = outputs.Outputs

	keywords := ctx.Variables["keyword"].AsValueMap()
	if keywords == nil {
		keywords = map[string]cty.Value{}
	}

	outputsVars := map[string]cty.Value{}

	for _, op := range r.Keyword.Outputs {
		outputsVars[op.Name] = op.Value
	}

	keywords[r.Name] = cty.ObjectVal(outputsVars)

	ctx.Variables["keyword"] = cty.ObjectVal(keywords)

	return
}
