package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
)

// LoadConfigFile reads the file at the given path and parses it as a config
// file.
//
// If the file cannot be read -- for example, if it does not exist -- then
// a nil *File will be returned along with error diagnostics. Callers may wish
// to disregard the returned diagnostics in this case and instead generate
// their own error message(s) with additional context.
//
// If the returned diagnostics has errors when a non-nil map is returned
// then the map may be incomplete but should be valid enough for careful
// static analysis.
//
// This method wraps LoadHCLFile, and so it inherits the syntax selection
// behaviors documented for that method.
func (p *Parser) LoadConfigFile(path string) (*File, hcl.Diagnostics) {
	return p.loadConfigFile(path, false)
}

// LoadConfigFileOverride is the same as LoadConfigFile except that it relaxes
// certain required attribute constraints in order to interpret the given
// file as an overrides file.
func (p *Parser) LoadConfigFileOverride(path string) (*File, hcl.Diagnostics) {
	return p.loadConfigFile(path, true)
}

func (p *Parser) loadConfigFile(path string, override bool) (*File, hcl.Diagnostics) {
	body, diags := p.LoadHCLFile(path)
	if body == nil {
		return nil, diags
	}

	file := &File{}

	content, contentDiags := body.Content(configFileSchema)
	diags = append(diags, contentDiags...)

	if attr, exists := content.Attributes["version"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &file.Version)
		diags = append(diags, valDiags...)
	}

	for _, block := range content.Blocks {
		switch block.Type {

		case "variable":
			cfg, cfgDiags := decodeVariableBlock(block, override)
			diags = append(diags, cfgDiags...)
			if cfg != nil {
				file.Variables = append(file.Variables, cfg)
			}

		case "output":
			cfg, cfgDiags := decodeOutputBlock(block, override)
			diags = append(diags, cfgDiags...)
			if cfg != nil {
				file.Outputs = append(file.Outputs, cfg)
			}

		case "scenario":
			cfg, cfgDiags := decodeScenarioBlock(block)
			diags = append(diags, cfgDiags...)
			if cfg != nil {
				file.Scenarios = append(file.Scenarios, cfg)
			}

		default:
			// Should never happen because the above cases should be exhaustive
			// for all block type names in our schema.
			continue

		}
	}

	return file, diags
}

// configFileSchema is the schema for the top-level of a config file. We use
// the low-level HCL API for this level so we can easily deal with each
// block type separately with its own decoding logic.
var configFileSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name:     "version",
			Required: true,
		},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type:       "variable",
			LabelNames: []string{"name"},
		},
		{
			Type:       "output",
			LabelNames: []string{"name"},
		},
		{
			Type: "scenario",
		},
	},
}
