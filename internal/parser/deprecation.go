package parser

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// deprecationKind distinguishes a deprecated block type from a deprecated
// attribute name.
type deprecationKind int

const (
	deprecatedBlock deprecationKind = iota
	deprecatedAttribute
)

// Block-type names that are shared across the parser. caseBlock and
// responseBlock are the deprecated aliases for stepBlockType / "expect".
const (
	stepBlockType     = "step"
	caseBlockType     = "case"
	responseBlockType = "response"
)

// deprecation declares a single deprecated DSL element. Deprecations are a
// Go-side flag (never expressed in HCL): the parser walks every loaded
// .tales file and emits a warning diagnostic for each occurrence so users can
// migrate before the element is removed in a future release.
//
// A rule matches by Name (block type or attribute name). When Within is set,
// it only matches when the chain of enclosing block types ends with that
// sequence (suffix match), which lets the same name be deprecated in one
// context without touching another. A nil Within matches anywhere.
type deprecation struct {
	kind    deprecationKind
	name    string
	within  []string
	summary string
	detail  string
}

// defaultDeprecations is the registry of deprecated DSL elements. Add an entry
// here to start warning on a block or attribute; the element keeps working
// until it is actually removed.
var defaultDeprecations = []deprecation{
	{
		kind:    deprecatedBlock,
		name:    caseBlockType,
		summary: `Deprecated block "case"`,
		detail:  `The "case" block is a deprecated alias for "step" and will be removed in a future release. Rename it to "step".`,
	},
	{
		kind:    deprecatedBlock,
		name:    responseBlockType,
		summary: `Deprecated block "response"`,
		detail:  `The "response" block is a deprecated alias for "expect" and will be removed in a future release. Rename it to "expect".`,
	},
}

// collectDeprecations walks a parsed .tales body and returns one warning
// diagnostic per deprecated block/attribute occurrence, using the default
// registry. Diagnostics are sorted by source position so output is stable.
func collectDeprecations(body hcl.Body) hcl.Diagnostics {
	return collectDeprecationsWith(body, defaultDeprecations)
}

// collectDeprecationsWith is the registry-injectable core of
// collectDeprecations, kept separate so tests can exercise the matching and
// scoping logic with synthetic rules.
func collectDeprecationsWith(body hcl.Body, rules []deprecation) hcl.Diagnostics {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		// Non-native bodies (e.g. JSON) cannot expose declaration ranges the
		// way we rely on; .tales files are always HCL native syntax, so this
		// path is only reached by exotic inputs and is intentionally silent.
		return nil
	}

	var diags hcl.Diagnostics

	walkDeprecations(syntaxBody, nil, rules, &diags)

	sort.SliceStable(diags, func(i, j int) bool {
		return lessByPosition(diags[i].Subject, diags[j].Subject)
	})

	return diags
}

// walkDeprecations recursively visits every attribute and block, matching each
// against the registry and recording a warning for every hit. parents is the
// chain of enclosing block types from the root down to the current body.
func walkDeprecations(body *hclsyntax.Body, parents []string, rules []deprecation, diags *hcl.Diagnostics) {
	for name, attr := range body.Attributes {
		if rule := matchDeprecation(rules, deprecatedAttribute, name, parents); rule != nil {
			nameRange := attr.NameRange
			*diags = append(*diags, deprecationDiag(rule, &nameRange))
		}
	}

	for _, block := range body.Blocks {
		if rule := matchDeprecation(rules, deprecatedBlock, block.Type, parents); rule != nil {
			defRange := block.DefRange()
			*diags = append(*diags, deprecationDiag(rule, &defRange))
		}

		walkDeprecations(block.Body, append(parents, block.Type), rules, diags)
	}
}

// matchDeprecation returns the first rule of the given kind whose name matches
// and whose Within suffix (if any) is satisfied by parents.
func matchDeprecation(rules []deprecation, kind deprecationKind, name string, parents []string) *deprecation {
	for i := range rules {
		rule := &rules[i]
		if rule.kind != kind || rule.name != name {
			continue
		}

		if matchesWithin(rule.within, parents) {
			return rule
		}
	}

	return nil
}

// matchesWithin reports whether parents ends with the within sequence. An empty
// within matches any context.
func matchesWithin(within, parents []string) bool {
	if len(within) == 0 {
		return true
	}

	if len(within) > len(parents) {
		return false
	}

	offset := len(parents) - len(within)
	for i, want := range within {
		if parents[offset+i] != want {
			return false
		}
	}

	return true
}

func deprecationDiag(rule *deprecation, subject *hcl.Range) *hcl.Diagnostic {
	return &hcl.Diagnostic{
		Severity: hcl.DiagWarning,
		Summary:  rule.summary,
		Detail:   rule.detail,
		Subject:  subject,
	}
}

// lessByPosition orders diagnostics by file, then line, then column. nil
// subjects sort last so positioned warnings stay grouped.
func lessByPosition(a, b *hcl.Range) bool {
	switch {
	case a == nil && b == nil:
		return false
	case a == nil:
		return false
	case b == nil:
		return true
	}

	if a.Filename != b.Filename {
		return a.Filename < b.Filename
	}

	if a.Start.Line != b.Start.Line {
		return a.Start.Line < b.Start.Line
	}

	return a.Start.Column < b.Start.Column
}

// FormatDeprecation renders a single deprecation warning as a one-line,
// file:line-prefixed string. Exported so CLI commands can present warnings
// consistently.
func FormatDeprecation(diag *hcl.Diagnostic) string {
	location := ""
	if diag.Subject != nil {
		location = fmt.Sprintf("%s:%d: ", diag.Subject.Filename, diag.Subject.Start.Line)
	}

	return fmt.Sprintf("%s%s", location, diag.Detail)
}

// Warnings returns the warning-severity diagnostics from a diagnostics set,
// preserving order. It lets callers surface deprecation warnings without
// re-implementing the severity filter.
func Warnings(diags hcl.Diagnostics) hcl.Diagnostics {
	warnings := make(hcl.Diagnostics, 0, len(diags))

	for _, diag := range diags {
		if diag.Severity == hcl.DiagWarning {
			warnings = append(warnings, diag)
		}
	}

	return warnings
}
