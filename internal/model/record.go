package model

import "github.com/hashicorp/hcl/v2"

// ScenarioRecord describes the scenario-level `record { ... }` block. The
// runtime turns it into a screen-recording session that spans the whole
// scenario. V1 only supports the iOS mobile provider, but the model lives
// here so the runner can wire a generic hook without importing the provider.
//
// Output is required: it is a path relative to scenario.workdir and is
// validated by the workspace resolver at runtime so it cannot escape the
// per-scenario directory. The remaining fields are passthrough options for
// `xcrun simctl io <UDID> recordVideo` (codec, mask, display, force) plus an
// optional target name that overrides the auto-inferred mobile target.
type ScenarioRecord struct {
	Output  Expression
	Codec   Expression
	Mask    Expression
	Display Expression
	Target  Expression
	Force   Expression
	Range   hcl.Range
}
