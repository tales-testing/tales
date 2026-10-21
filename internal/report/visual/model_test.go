package visual

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/report"
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

func TestBuildPropagatesCaptureNote(t *testing.T) {
	t.Parallel()

	const note = "screenshot skipped: the browser did not answer in time (a navigation was probably in progress)"

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{{
			Name: "web", Status: report.StatusPass,
			Steps: []*report.StepResult{{
				Name: "bounce", Provider: "browser", Status: report.StatusPass,
				Actions: []*report.ActionResult{{
					Index: 0, Kind: "goto", Status: report.StatusPass,
					Hierarchy:   "/abs/build/artifacts/browser/web/bounce/step/attempt-1/actions/0000-goto-unnamed/dom.html",
					CaptureNote: note,
				}},
			}},
		}},
	}

	action := Build(suite, "").Scenarios[0].Steps[0].Actions[0]
	if action.CaptureNote != note {
		t.Errorf("CaptureNote = %q, want %q", action.CaptureNote, note)
	}

	if action.Screenshot != "" {
		t.Errorf("Screenshot = %q, want none", action.Screenshot)
	}
}

// TestBuildCarriesActionTimingPointAndStepArtifacts pins what the visual
// report needs from issue #83: when each action started, where a pointer
// action pointed, and the failed step's own files (console.jsonl,
// network.jsonl, state.json) as report-relative links.
func TestBuildCarriesActionTimingPointAndStepArtifacts(t *testing.T) {
	t.Parallel()

	htmlDir := t.TempDir()
	stepDir := filepath.Join(htmlDir, "artifacts", "browser", "web-1a2b3c4d", "submit", "step", "attempt-1", "step")

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{{
			Name: "web", Status: report.StatusFail,
			Steps: []*report.StepResult{{
				Name: "submit", Provider: "browser", Status: report.StatusFail,
				Artifacts: []report.Artifact{
					{Type: "screenshot", Path: filepath.Join(stepDir, "screenshot.png")},
					{Type: "console", Path: filepath.Join(stepDir, "console.jsonl")},
					{Type: "network", Path: filepath.Join(stepDir, "network.jsonl")},
					{Type: "state", Path: filepath.Join(stepDir, "state.json")},
				},
				Actions: []*report.ActionResult{
					{
						Index: 0, Kind: "click", Status: report.StatusFail,
						StartedAt: time.Date(2026, 10, 21, 9, 54, 34, 140_000_000, time.UTC),
						Duration:  time.Second,
						Point:     &report.ActionPoint{X: 37.5, Y: 90},
					},
					{Index: 1, Kind: "wait_visible", Status: report.StatusSkip},
				},
			}},
		}},
	}

	step := Build(suite, filepath.Join(htmlDir, "report.html")).Scenarios[0].Steps[0]

	click, skipped := step.Actions[0], step.Actions[1]

	if click.StartedAt != "2026-10-21T09:54:34.140Z" {
		t.Errorf("StartedAt = %q", click.StartedAt)
	}

	if click.Point == nil || *click.Point != (Point{X: 37.5, Y: 90}) {
		t.Errorf("Point = %+v, want (37.5, 90)", click.Point)
	}

	if skipped.StartedAt != "" || skipped.Point != nil {
		t.Errorf("a skipped action never ran: %+v", skipped)
	}

	if len(step.Artifacts) != 4 {
		t.Fatalf("step artifacts = %+v, want 4", step.Artifacts)
	}

	want := "artifacts/browser/web-1a2b3c4d/submit/step/attempt-1/step/console.jsonl"
	if step.Artifacts[1].Type != "console" || step.Artifacts[1].Path != want {
		t.Errorf("console artifact = %+v, want a path relative to the report: %s", step.Artifacts[1], want)
	}
}

func TestBuildOmitsStepArtifactsWhenNone(t *testing.T) {
	t.Parallel()

	step := Build(sampleSuite(), "").Scenarios[0].Steps[0]
	if step.Artifacts != nil {
		t.Errorf("Artifacts = %+v, want nil so the JSON omits the key", step.Artifacts)
	}
}
