package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hyperxlab/tales/pkg/tales/addrs"
)

// Scenario represents a "scenario" block in a file.
type Scenario struct {
	Name    string
	Summary string
	Cases   []ScenarioCase

	DeclRange hcl.Range
	TypeRange hcl.Range
}

func (s *Scenario) moduleUniqueKey() string {
	return s.Addr().String()
}

// Addr returns a resource address for the receiver that is relative to the
// resource's containing module.
func (s *Scenario) Addr() addrs.Scenario {
	return addrs.Scenario{
		Name: s.Name,
	}
}

// ScenarioCase interface
type ScenarioCase interface {
	Execute()
}

// ScenarioCaseBase represents a "case" block in Scenario
type ScenarioCaseBase struct {
	Type   string
	Name   string
	Config hcl.Body

	DeclRange hcl.Range
	TypeRange hcl.Range
}

func decodeScenarioCaseBlock(block *hcl.Block) (ScenarioCase, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	switch block.Labels[0] {
	case "http":
		c, httpDiags := decodeScenarioHTTPCaseBlock(block)
		diags = append(diags, httpDiags...)

		return c, diags
	default:
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid case type",
			Detail:   fmt.Sprintf("The case type name %q is invalid.", block.Labels[0]),
			Subject:  &block.LabelRanges[0],
		})

		return nil, diags
	}
}

func decodeScenarioBlock(block *hcl.Block) (*Scenario, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	s := &Scenario{
		DeclRange: block.DefRange,
		//TypeRange: block.LabelRanges[0],
	}

	content, moreDiags := block.Body.Content(scenarioBlockSchema)
	diags = append(diags, moreDiags...)

	if attr, exists := content.Attributes["name"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &s.Name)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["summary"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &s.Summary)
		diags = append(diags, valDiags...)
	}

	for _, block := range content.Blocks {
		switch block.Type {
		case "case":
			scenarioCase, caseDiags := decodeScenarioCaseBlock(block)
			diags = append(diags, caseDiags...)

			s.Cases = append(s.Cases, scenarioCase)

		default:
			// Any other block types are ones we've reserved for future use,
			// so they get a generic message.
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Reserved block type name in scenario block",
				Detail:   fmt.Sprintf("The block type name %q is reserved for use by Tales in a future version.", block.Type),
				Subject:  &block.TypeRange,
			})
		}
	}

	return s, diags
}

var scenarioBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name: "name",
		},
		{
			Name: "summary",
		},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "case", LabelNames: []string{"type", "name"}},
	},
}
