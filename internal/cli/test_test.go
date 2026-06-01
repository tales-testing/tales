package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
	mobileprovider "github.com/tales-testing/tales/internal/provider/mobile"
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

func TestPrintPreflight_TimeoutDisabledWarning(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	suite := &model.Suite{
		Files:     []string{"a.tales", "b.tales"},
		Scenarios: []*model.Scenario{{Name: "one"}, {Name: "two"}, {Name: "three"}},
	}

	printPreflight(&buf, suite, 0)

	out := buf.String()
	for _, want := range []string{"loaded 3 scenarios", "from 2 files", "timeout=disabled", "--timeout="} {
		if !strings.Contains(out, want) {
			t.Errorf("preflight output %q missing %q", out, want)
		}
	}
}

func TestPrintPreflight_TimeoutSetShowsDuration(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	suite := &model.Suite{
		Files:     []string{"only.tales"},
		Scenarios: []*model.Scenario{{Name: "alone"}},
	}

	printPreflight(&buf, suite, 5*time.Minute)

	out := buf.String()
	if !strings.Contains(out, "1 scenario from 1 file") {
		t.Errorf("singular form must drop the 's': %q", out)
	}

	if !strings.Contains(out, "timeout=5m0s") {
		t.Errorf("expected timeout=5m0s in %q", out)
	}

	if strings.Contains(out, "disabled") {
		t.Errorf("must not show 'disabled' when --timeout > 0: %q", out)
	}
}

func TestPrintDeprecationWarnings_ListsEachWithLocation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	diags := hcl.Diagnostics{
		{Severity: hcl.DiagError, Summary: "ignored error"},
		{
			Severity: hcl.DiagWarning,
			Detail:   `The "case" block is a deprecated alias for "step".`,
			Subject:  &hcl.Range{Filename: "demo.tales", Start: hcl.Pos{Line: 4}},
		},
	}

	printDeprecationWarnings(&buf, diags)

	out := buf.String()
	for _, want := range []string{"1 deprecation warning:", "demo.tales:4:", `deprecated alias for "step"`} {
		if !strings.Contains(out, want) {
			t.Errorf("warning output %q missing %q", out, want)
		}
	}

	if strings.Contains(out, "ignored error") {
		t.Errorf("errors must not be printed as deprecation warnings: %q", out)
	}
}

func TestPrintDeprecationWarnings_QuietWhenNone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	printDeprecationWarnings(&buf, hcl.Diagnostics{{Severity: hcl.DiagError, Summary: "boom"}})

	if buf.Len() != 0 {
		t.Errorf("no warnings means no output, got %q", buf.String())
	}
}
