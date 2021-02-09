package generators

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/pkg/tales/configs"
	"github.com/zclconf/go-cty/cty"
)

func init() {
	configs.RegisterGenerator("email", &EmailGeneratorFactory{})
}

// EmailGeneratorFactory struct
type EmailGeneratorFactory struct {
}

// Parse implements GeneratorFactory
func (f *EmailGeneratorFactory) Parse(g *configs.Generator, ctx *hcl.EvalContext) (configs.GeneratorExecutor, hcl.Diagnostics) {
	executor := &EmailGenerator{
		Name: g.Name,
	}

	return executor, gohcl.DecodeBody(g.HCL, ctx, executor)
}

// EmailGenerator struct
type EmailGenerator struct {
	Name   string
	Prefix string `hcl:"prefix,optional"`
}

// Generate implements GeneratorExecutor
func (eg EmailGenerator) Generate() (cty.Value, error) {

	return cty.StringVal("axel@etcheverry.biz"), nil
}
