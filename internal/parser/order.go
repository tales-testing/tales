package parser

import (
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// stepKey identifies a step block by its two labels (provider and name).
type stepKey struct {
	provider string
	name     string
}

// sourceStep is one step/case block located in textual source order.
type sourceStep struct {
	key  stepKey
	line int
}

// childBlocks returns body's child blocks of the given type, in source order.
// It returns nil for a nil body so callers can chain it safely.
func childBlocks(body *hclsyntax.Body, blockType string) []*hclsyntax.Block {
	if body == nil {
		return nil
	}

	blocks := make([]*hclsyntax.Block, 0, len(body.Blocks))

	for _, block := range body.Blocks {
		if block.Type == blockType {
			blocks = append(blocks, block)
		}
	}

	return blocks
}

// blockBodyAt returns the body of blocks[i], or nil when i is out of range.
func blockBodyAt(blocks []*hclsyntax.Block, i int) *hclsyntax.Body {
	if i < 0 || i >= len(blocks) {
		return nil
	}

	return blocks[i].Body
}

// sourceOrder walks a scenario/keyword/teardown body and returns its step and
// case child blocks in textual source order, with their definition lines.
// gohcl decodes step and case blocks into separate slices, losing the order
// in which they appear when interleaved; this recovers it.
func sourceOrder(body *hclsyntax.Body) []sourceStep {
	if body == nil {
		return nil
	}

	order := make([]sourceStep, 0, len(body.Blocks))

	for _, block := range body.Blocks {
		if block.Type != "step" && block.Type != "case" {
			continue
		}

		if len(block.Labels) != 2 {
			continue
		}

		order = append(order, sourceStep{
			key:  stepKey{provider: block.Labels[0], name: block.Labels[1]},
			line: block.DefRange().Start.Line,
		})
	}

	return order
}

// reorderStepsBySource returns steps reordered to match textual source order,
// setting each Step.Line from its source block. When the decoded set and the
// source set disagree (count mismatch, an unmatched block, or duplicate
// (provider, name) keys — all already reported elsewhere through gohcl or
// suite validation) it returns steps unchanged rather than risk dropping or
// duplicating a step.
func reorderStepsBySource(steps []*model.Step, order []sourceStep) []*model.Step {
	if len(order) != len(steps) {
		return steps
	}

	byKey := make(map[stepKey]*model.Step, len(steps))

	for _, step := range steps {
		key := stepKey{provider: step.Provider, name: step.Name}
		if _, dup := byKey[key]; dup {
			return steps
		}

		byKey[key] = step
	}

	reordered := make([]*model.Step, 0, len(steps))

	for _, src := range order {
		step, ok := byKey[src.key]
		if !ok {
			return steps
		}

		if src.line > 0 {
			step.Line = src.line
		}

		reordered = append(reordered, step)
	}

	return reordered
}
