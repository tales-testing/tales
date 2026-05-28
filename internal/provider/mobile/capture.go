package mobile

import (
	"fmt"

	"github.com/tales-testing/tales/internal/provider"
)

// Status string constants that mirror report.Status values. They live here
// to avoid pulling the report package into the provider layer (which would
// introduce a downward dependency). Consumers (the runtime) translate these
// back into report.Status when copying ActionResults into StepResult.Actions.
const (
	actionStatusPass = "pass"
	actionStatusFail = "fail"
	actionStatusSkip = "skipped"
)

// CaptureMode is the shared capture-mode enum re-exported for backward
// compatibility with the mobile provider's public API.
type CaptureMode = provider.CaptureMode

// Capture mode constants — aliases of the shared provider package values.
const (
	CaptureNone     = provider.CaptureNone
	CaptureFailures = provider.CaptureFailures
	CaptureSteps    = provider.CaptureSteps
	CaptureActions  = provider.CaptureActions
)

// ParseCaptureMode delegates to the shared parser.
func ParseCaptureMode(s string) (CaptureMode, error) {
	mode, err := provider.ParseCaptureMode(s)
	if err != nil {
		return "", fmt.Errorf("mobile: %w", err)
	}

	return mode, nil
}
