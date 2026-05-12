package visual

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/report"
)

func sampleSuite() *report.SuiteResult {
	return &report.SuiteResult{
		Seed:     1234,
		Duration: 2 * time.Second,
		Scenarios: []*report.ScenarioResult{
			{
				Name:     "register",
				File:     "e2e/ios/pass/register.tales",
				Status:   report.StatusPass,
				Duration: 1500 * time.Millisecond,
				Steps: []*report.StepResult{
					{
						Name:     "submit",
						Provider: "mobile",
						Phase:    "step",
						Status:   report.StatusPass,
						Duration: 700 * time.Millisecond,
						Actions: []*report.ActionResult{
							{
								Index:      0,
								Kind:       "tap",
								Label:      "Tap welcome.register",
								SelectorID: "welcome.register",
								Status:     report.StatusPass,
								Duration:   120 * time.Millisecond,
								Screenshot: "/abs/build/artifacts/mobile/register-aabbccdd/submit/step/attempt-1/actions/0000-tap-welcome.register/screenshot.png",
								Hierarchy:  "/abs/build/artifacts/mobile/register-aabbccdd/submit/step/attempt-1/actions/0000-tap-welcome.register/hierarchy.json",
							},
							{
								Index:      1,
								Kind:       "input_text",
								Label:      "Input text register.password ***",
								SelectorID: "register.password",
								Secure:     true,
								Value:      "***",
								Status:     report.StatusPass,
								Duration:   200 * time.Millisecond,
							},
						},
					},
				},
			},
		},
	}
}

func TestBuildMapsScenariosStepsActions(t *testing.T) {
	t.Parallel()

	suite := sampleSuite()
	got := Build(suite, "")

	if got.Seed != 1234 {
		t.Errorf("seed = %d, want 1234", got.Seed)
	}

	if got.Status != string(report.StatusPass) {
		t.Errorf("status = %q, want pass", got.Status)
	}

	if got.DurationMS != 2000 {
		t.Errorf("duration = %dms, want 2000", got.DurationMS)
	}

	if len(got.Scenarios) != 1 || len(got.Scenarios[0].Steps) != 1 || len(got.Scenarios[0].Steps[0].Actions) != 2 {
		t.Fatalf("unexpected shape: %+v", got)
	}

	secure := got.Scenarios[0].Steps[0].Actions[1]
	if secure.Value != "***" || !secure.Secure {
		t.Errorf("secure action lost mask: %+v", secure)
	}

	if strings.Contains(secure.Label, "hunter") {
		t.Errorf("secure label may have leaked raw value: %q", secure.Label)
	}
}

func TestBuildHandlesNilSuiteAndNilEntries(t *testing.T) {
	t.Parallel()

	empty := Build(nil, "")
	if empty.Status == "" || len(empty.Scenarios) != 0 {
		t.Fatalf("nil suite should produce empty scenarios with a status, got %+v", empty)
	}

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{
			nil,
			{
				Name: "x", Status: report.StatusPass,
				Steps: []*report.StepResult{nil, {Name: "ok", Status: report.StatusPass, Actions: []*report.ActionResult{nil}}},
			},
		},
	}

	got := Build(suite, "")
	if len(got.Scenarios) != 1 {
		t.Fatalf("expected 1 scenario after dropping nil, got %d", len(got.Scenarios))
	}

	if len(got.Scenarios[0].Steps) != 1 || len(got.Scenarios[0].Steps[0].Actions) != 0 {
		t.Fatalf("unexpected step/action shape after nil drop: %+v", got.Scenarios[0])
	}
}

func TestBuildConvertsArtifactPathsToRelativeWhenPossible(t *testing.T) {
	t.Parallel()

	abs, err := filepath.Abs(filepath.Join("build", "artifacts", "mobile", "register", "submit", "screenshot.png"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	htmlPath, err := filepath.Abs(filepath.Join("build", "reports", "visual.html"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{
			{
				Name: "x", Status: report.StatusPass,
				Steps: []*report.StepResult{{
					Name: "submit", Status: report.StatusPass,
					Actions: []*report.ActionResult{{Index: 0, Kind: "tap", Status: report.StatusPass, Screenshot: abs}},
				}},
			},
		},
	}

	got := Build(suite, htmlPath)
	screenshot := got.Scenarios[0].Steps[0].Actions[0].Screenshot

	if filepath.IsAbs(screenshot) {
		t.Errorf("expected relative path, got absolute %q", screenshot)
	}

	if !strings.Contains(screenshot, "artifacts/mobile/register/submit/screenshot.png") {
		t.Errorf("unexpected relative path: %q", screenshot)
	}
}

func TestBuildLeavesUnconvertiblePathsAsIs(t *testing.T) {
	t.Parallel()

	// htmlPath empty means there is no destination directory to compute
	// against; the original path must survive untouched.
	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{
			{
				Name: "x", Status: report.StatusPass,
				Steps: []*report.StepResult{{
					Name: "submit", Status: report.StatusPass,
					Actions: []*report.ActionResult{{Index: 0, Kind: "tap", Status: report.StatusPass, Screenshot: "/elsewhere/foo.png"}},
				}},
			},
		},
	}

	got := Build(suite, "")
	if got.Scenarios[0].Steps[0].Actions[0].Screenshot != "/elsewhere/foo.png" {
		t.Errorf("expected path to survive unchanged, got %q", got.Scenarios[0].Steps[0].Actions[0].Screenshot)
	}
}

func TestBuildPropagatesActionError(t *testing.T) {
	t.Parallel()

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{
			{
				Name: "x", Status: report.StatusFail,
				Steps: []*report.StepResult{{
					Name: "broken", Status: report.StatusFail,
					Actions: []*report.ActionResult{{
						Index: 0, Kind: "tap", Status: report.StatusFail,
						Error: &report.ErrorDetail{Kind: "action", Message: "element not visible"},
					}},
				}},
			},
		},
	}

	got := Build(suite, "")
	if got.Scenarios[0].Steps[0].Actions[0].Error != "element not visible" {
		t.Errorf("expected error message propagated, got %q", got.Scenarios[0].Steps[0].Actions[0].Error)
	}

	if got.Status != string(report.StatusFail) {
		t.Errorf("suite status should be fail when any scenario fails, got %q", got.Status)
	}
}
