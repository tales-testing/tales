package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

type GeneratorPlugin interface {
	Parse(g *Generator, ctx *hcl.EvalContext) (GeneratorExecutor, hcl.Diagnostics)
}

type GeneratorExecutor interface {
	Generate() (cty.Value, error)
}

var generators map[string]GeneratorPlugin

var generatorsInstance map[string]GeneratorExecutor

func init() {
	generators = make(map[string]GeneratorPlugin)
	generatorsInstance = make(map[string]GeneratorExecutor)
}

// RegisterGenerator GeneratorPlugin by type
func RegisterGenerator(t string, g GeneratorPlugin) error {
	if generators == nil {
		generators = map[string]GeneratorPlugin{}
	}

	if _, ok := generators[t]; ok {
		return fmt.Errorf("generator %s already exists", t)
	}

	generators[t] = g

	return nil
}

// Generator struct
type Generator struct {
	Type string   `hcl:",label"`
	Name string   `hcl:"name,label"`
	HCL  hcl.Body `hcl:",remain"`
}

func (g *Generator) Execute(module *Module, ctx *hcl.EvalContext) hcl.Diagnostics {
	factory, ok := generators[g.Type]
	if !ok {
		return hcl.Diagnostics{
			{
				Severity: hcl.DiagError,
				Summary:  "Generator not found",
				Detail:   fmt.Sprintf("The generator %s does not exist.", g.Type),
			},
		}
	}

	instance, diags := factory.Parse(g, ctx)
	if diags.HasErrors() {
		return diags
	}

	if _, ok := generatorsInstance[g.Name]; ok {
		return hcl.Diagnostics{
			{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("Duplicate generator %q configuration", g.Name),
				Detail:   fmt.Sprintf("A generator named %q was already declared. Generator names must be unique.", g.Name),
			},
		}
	}

	generatorsInstance[g.Name] = instance

	return hcl.Diagnostics{}
}
