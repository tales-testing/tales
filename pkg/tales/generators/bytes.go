package generators

import (
	"crypto/rand"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/pkg/tales/configs"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

func init() {
	configs.RegisterGenerator("bytes", &BytesGeneratorFactory{})
}

// BytesGeneratorFactory struct
type BytesGeneratorFactory struct {
}

// Parse implements GeneratorFactory
func (f *BytesGeneratorFactory) Parse(g *configs.Generator, ctx *hcl.EvalContext) (configs.GeneratorExecutor, hcl.Diagnostics) {
	executor := &BytesGenerator{
		Name: g.Name,
	}

	return executor, gohcl.DecodeBody(g.HCL, ctx, executor)
}

// BytesGenerator struct
type BytesGenerator struct {
	Name   string
	Length int `hcl:"length"`
}

// Generate implements GeneratorExecutor
func (eg BytesGenerator) Generate() (cty.Value, error) {
	b := make([]byte, eg.Length)

	_, err := rand.Read(b)
	if err != nil {
		return cty.NilVal, err
	}

	return stdlib.BytesVal(b), nil
}
