package generators

import (
	"github.com/euskadi31/go-faker"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/pkg/tales/configs"
	"github.com/zclconf/go-cty/cty"
)

func init() {
	configs.RegisterGenerator("mac_address", &MacAddressGeneratorFactory{})
}

// MacAddressGeneratorFactory struct
type MacAddressGeneratorFactory struct {
}

// Parse implements GeneratorFactory
func (f *MacAddressGeneratorFactory) Parse(g *configs.Generator, ctx *hcl.EvalContext) (configs.GeneratorExecutor, hcl.Diagnostics) {
	executor := &MacAddressGenerator{
		Name:      g.Name,
		Separator: ":",
	}

	return executor, gohcl.DecodeBody(g.HCL, ctx, executor)
}

// MacAddressGenerator struct
type MacAddressGenerator struct {
	Name      string
	Prefix    string `hcl:"prefix,optional"`
	Separator string `hcl:"sep,optional"`
}

// Generate implements GeneratorExecutor
func (g MacAddressGenerator) Generate() (cty.Value, error) {
	fg := faker.NewMacAddressGenerator()
	if g.Prefix != "" {
		fg.Prefix = g.Prefix
	}

	fg.Separator = g.Separator

	mac := fg.Generate()

	return cty.StringVal(mac), nil
}
