package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/internal/model"
)

// skipAttrCondition is the set of attribute names that count as a
// skip condition (i.e. populate at least one field other than reason).
var skipAttrCondition = map[string]struct{}{
	"condition": {},
	"os":        {},
	"arch":      {},
	"env_set":   {},
	"env":       {},
}

// allowedSkipAttrs lists every attribute accepted inside a skip block.
var allowedSkipAttrs = map[string]struct{}{
	"condition": {},
	"reason":    {},
	"os":        {},
	"arch":      {},
	"env_set":   {},
	"env":       {},
}

// decodeSkipRules turns the raw HCL skip_if / skip_unless blocks into
// model.SkipRule entries, validating that each rule declares at least
// one actionable attribute and rejects unknown attributes.
func decodeSkipRules(path string, skipIf, skipUnless []skipBlock) ([]model.SkipRule, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0, len(skipIf)+len(skipUnless))
	rules := make([]model.SkipRule, 0, len(skipIf)+len(skipUnless))

	for _, raw := range skipIf {
		rule, ruleDiags := decodeSkipRule(path, raw, model.SkipIf)

		diags = append(diags, ruleDiags...)
		rules = append(rules, rule)
	}

	for _, raw := range skipUnless {
		rule, ruleDiags := decodeSkipRule(path, raw, model.SkipUnless)

		diags = append(diags, ruleDiags...)
		rules = append(rules, rule)
	}

	return rules, diags
}

func decodeSkipRule(path string, raw skipBlock, kind model.SkipKind) (model.SkipRule, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0, 2)

	attrs, attrDiags := raw.Body.JustAttributes()
	diags = append(diags, attrDiags...)

	rule := model.SkipRule{Kind: kind}
	hasCondition := false

	var firstRange *hcl.Range

	for name, attr := range attrs {
		attrRange := attr.Range
		if firstRange == nil {
			firstRange = &attrRange
		}

		if _, ok := allowedSkipAttrs[name]; !ok {
			diags = append(diags, diagError(
				"Unknown "+string(kind)+" attribute",
				fmt.Sprintf("attribute %q is not supported. Use one of condition, reason, os, arch, env_set, or env.", name),
				&attrRange,
			))

			continue
		}

		expression := attrExpr(path, attr)

		switch name {
		case "condition":
			rule.Condition = expression
		case "reason":
			rule.Reason = expression
		case "os":
			rule.OS = expression
		case "arch":
			rule.Arch = expression
		case "env_set":
			rule.EnvSet = expression
		case "env":
			rule.Env = expression
		}

		if _, ok := skipAttrCondition[name]; ok {
			hasCondition = true
		}
	}

	if firstRange != nil {
		rule.Range = *firstRange
	}

	if !hasCondition {
		diags = append(diags, diagError(
			"Empty "+string(kind)+" block",
			string(kind)+" must declare at least one of condition, os, arch, env_set, or env.",
			firstRange,
		))
	}

	return rule, diags
}
