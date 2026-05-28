package provider

import (
	"fmt"
	"strings"
)

// CaptureMode selects when a UI-driving provider (mobile, browser) captures
// screenshot and hierarchy / DOM artifacts. The default for the binary is
// CaptureFailures, which preserves the historical behavior (capture only on
// step failure).
type CaptureMode string

const (
	// CaptureNone disables every screenshot and hierarchy / DOM capture,
	// including the failure path.
	CaptureNone CaptureMode = "none"

	// CaptureFailures captures a single screenshot + hierarchy / DOM at the
	// step level when the step fails.
	CaptureFailures CaptureMode = "failures"

	// CaptureSteps captures one screenshot + hierarchy / DOM at the end of
	// each step (success or failure). The provider appends a synthetic
	// step_end ActionResult carrying the paths so the visual report can
	// render one tile per step.
	CaptureSteps CaptureMode = "steps"

	// CaptureActions captures a screenshot + hierarchy / DOM after every UI
	// action. On action failure a best-effort capture still happens so the
	// visual report can show the moment of failure.
	CaptureActions CaptureMode = "actions"
)

// ParseCaptureMode validates a user-supplied capture mode string and returns
// the canonical CaptureMode. Input is matched case-insensitively after
// trimming surrounding whitespace.
func ParseCaptureMode(s string) (CaptureMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(CaptureNone):
		return CaptureNone, nil
	case string(CaptureFailures):
		return CaptureFailures, nil
	case string(CaptureSteps):
		return CaptureSteps, nil
	case string(CaptureActions):
		return CaptureActions, nil
	}

	return "", fmt.Errorf("invalid capture mode %q (want one of: none, failures, steps, actions)", s)
}
