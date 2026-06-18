package parser

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// decodeScenarioRecord turns a parsed scenario-level record { ... } block
// into the model representation. The block is optional: a nil rb yields
// (nil, nil) and the scenario simply runs with no recording. When present,
// output is required because every supported recorder (V1: simctl) needs a
// destination path. The remaining attributes are forwarded as raw HCL
// expressions and validated at runtime by the provider implementing the
// ScenarioHook capability.
//
// scenarioBody is the parent scenario body used to locate the record block
// and surface its definition range in diagnostics; nil is tolerated (no
// range attached).
func decodeScenarioRecord(path string, rb *recordBlock, scenarioBody *hclsyntax.Body) (*model.ScenarioRecord, hcl.Diagnostics) {
	if rb == nil {
		return nil, nil
	}

	diags := make(hcl.Diagnostics, 0)

	rng := recordBlockRange(scenarioBody)

	if !exprIsSet(rb.Output) {
		subject := rng
		diags = append(diags, diagError(
			"Missing record output",
			"scenario record block must declare output = \"<path>\".",
			&subject,
		))
	}

	return &model.ScenarioRecord{
		Output:  optionalExpr(path, rb.Output),
		Codec:   optionalExpr(path, rb.Codec),
		Mask:    optionalExpr(path, rb.Mask),
		Display: optionalExpr(path, rb.Display),
		Target:  optionalExpr(path, rb.Target),
		Force:   optionalExpr(path, rb.Force),
		Range:   rng,
	}, diags
}

// recordBlockRange returns the source range of the first record child block
// inside a scenario body, or a zero range when the body is nil or carries no
// such block (gohcl still decoded one, so the caller treats this defensively).
func recordBlockRange(scenarioBody *hclsyntax.Body) hcl.Range {
	blocks := childBlocks(scenarioBody, "record")
	if len(blocks) == 0 {
		return hcl.Range{}
	}

	return blocks[0].DefRange()
}
