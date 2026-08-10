package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// teardownBlockType is the block name shared by the scenario-level and the
// suite-level teardown blocks.
const teardownBlockType = "teardown"

// decodeSuiteTeardown decodes the top-level `teardown { step ... }` block of
// one file. It mirrors the scenario-level teardown decoding, including the
// hclsyntax walk that recovers the textual order of interleaved step / case
// blocks (gohcl decodes them into separate slices and loses it).
//
// syntaxBody is the file body, so childBlocks only sees top-level teardown
// blocks: scenario-level ones live inside a scenario block and are never
// visited here.
func decodeSuiteTeardown(path string, blocks []teardownDef, syntaxBody *hclsyntax.Body) ([]*model.Step, hcl.Diagnostics) {
	if len(blocks) == 0 {
		return nil, nil
	}

	diags := make(hcl.Diagnostics, 0)
	steps := make([]*model.Step, 0)
	sourceBlocks := childBlocks(syntaxBody, teardownBlockType)

	for i, td := range blocks {
		decoded, tDiags := decodeSteps(path, append(td.Steps, td.Cases...))
		diags = append(diags, tDiags...)

		decoded = reorderStepsBySource(decoded, sourceOrder(blockBodyAt(sourceBlocks, i)))
		steps = append(steps, decoded...)
	}

	return steps, diags
}

// mergeSuiteTeardown installs the suite-level teardown carried by one file
// into the merged suite. Only one file may declare it: concatenating across
// files would make the cleanup order depend on the alphabetical order of the
// filenames, and silently dropping one would lose cleanup work. Both are worse
// than asking the author to pick a single home for it.
func mergeSuiteTeardown(dst, src *model.Suite, diags *hcl.Diagnostics) {
	if len(src.Teardown) == 0 {
		return
	}

	if dst.TeardownFile != "" {
		*diags = append(*diags, diagError(
			"Duplicate suite teardown",
			fmt.Sprintf("A suite-level teardown block is already defined in %q; only one file may declare it", dst.TeardownFile),
			nil,
		))

		return
	}

	dst.Teardown = src.Teardown
	dst.TeardownFile = src.TeardownFile
}

// validateSuiteTeardown enforces the same contracts the scenario-level
// teardown gets: unique step names and well-formed step-local vars.
//
// Step ordering is deliberately not validated, for the same reason it is not
// validated on scenario teardown: cleanup routinely guards optional references
// with when = can(...), so referencing something that may never exist is a
// legitimate pattern here rather than an error.
func validateSuiteTeardown(suite *model.Suite, diags *hcl.Diagnostics) {
	if len(suite.Teardown) == 0 {
		return
	}

	names := map[string]struct{}{}

	for _, step := range suite.Teardown {
		if _, exists := names[step.Name]; exists {
			*diags = append(*diags, diagError("Duplicate step", fmt.Sprintf("Suite teardown has duplicate step %q", step.Name), nil))
		}

		names[step.Name] = struct{}{}
	}

	validateStepVarsIn(suite.Teardown, "Suite teardown", diags)
}
