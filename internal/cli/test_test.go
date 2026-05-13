package cli

import (
	"strings"
	"testing"

	mobileprovider "github.com/hyperxlab/tales/internal/provider/mobile"
)

func TestResolveCaptureMode_DefaultsWithoutFlags(t *testing.T) {
	t.Parallel()

	got, err := resolveCaptureMode("", "")
	if err != nil {
		t.Fatalf("resolveCaptureMode: %v", err)
	}

	if got != mobileprovider.CaptureFailures {
		t.Errorf("no flags → default capture mode should be failures, got %q", got)
	}
}

func TestResolveCaptureMode_DefaultsToActionsWhenReportHTMLSet(t *testing.T) {
	t.Parallel()

	got, err := resolveCaptureMode("", "build/reports/visual.html")
	if err != nil {
		t.Fatalf("resolveCaptureMode: %v", err)
	}

	if got != mobileprovider.CaptureActions {
		t.Errorf("with --report-html the default capture mode should be actions, got %q", got)
	}
}

func TestResolveCaptureMode_ExplicitOverridesDefault(t *testing.T) {
	t.Parallel()

	got, err := resolveCaptureMode("steps", "build/reports/visual.html")
	if err != nil {
		t.Fatalf("resolveCaptureMode: %v", err)
	}

	if got != mobileprovider.CaptureSteps {
		t.Errorf("explicit --capture-screenshots=steps must win over the report-html default; got %q", got)
	}
}

func TestResolveCaptureMode_InvalidValueReturnsError(t *testing.T) {
	t.Parallel()

	_, err := resolveCaptureMode("loud", "")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}

	for _, want := range []string{"none", "failures", "steps", "actions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message %q should list valid mode %q", err.Error(), want)
		}
	}
}
