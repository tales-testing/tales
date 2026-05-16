package model

import "github.com/hashicorp/hcl/v2"

// SkipKind identifies how a skip rule is evaluated.
type SkipKind string

const (
	// SkipIf skips when the rule evaluates to true.
	SkipIf SkipKind = "skip_if"
	// SkipUnless skips when the rule does not evaluate to true.
	SkipUnless SkipKind = "skip_unless"
)

// SkipRule is one declarative skip directive attached to a scenario or step.
//
// Attribute semantics inside a single rule are combined with AND:
// the rule "matches" only when every populated attribute is satisfied.
// For SkipIf, a matching rule triggers a skip; for SkipUnless, a
// non-matching rule triggers a skip. Multiple rules attached to the same
// element are evaluated in order and the first one that triggers wins.
type SkipRule struct {
	Kind      SkipKind
	Condition Expression
	Reason    Expression
	OS        Expression
	Arch      Expression
	EnvSet    Expression
	Env       Expression
	Range     hcl.Range
}

// HasConditions reports whether the rule declares at least one
// non-reason attribute. A rule with only a reason is a parse error.
func (r SkipRule) HasConditions() bool {
	return !r.Condition.Empty() ||
		!r.OS.Empty() ||
		!r.Arch.Empty() ||
		!r.EnvSet.Empty() ||
		!r.Env.Empty()
}
