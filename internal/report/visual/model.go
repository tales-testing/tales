// Package visual builds a renderable view-model of a SuiteResult tailored to
// the offline HTML visual report. It owns:
//   - the JSON payload shape consumed by the embedded JS (Report / Scenario
//     / Step / Action)
//   - the absolute → report-relative path conversion for screenshot and
//     hierarchy artifacts, so the HTML stays portable when shipped as a
//     single file with its build/artifacts/ tree alongside.
//
// Secure values are not re-masked here: report.ActionResult.Value already
// carries "***" when secure, and we copy it through unchanged.
package visual

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/tales-testing/tales/internal/report"
)

const visualReportTitle = "Tales Visual Report"

// suiteTeardownName labels the pseudo-scenario the suite-level teardown block
// is projected into. It matches the reserved scenario name the runtime stamps
// on those step results.
const suiteTeardownName = "tales:suite-teardown"

// Report is the top-level JSON payload embedded in the HTML data island.
type Report struct {
	Title       string     `json:"title"`
	GeneratedAt string     `json:"generated_at"`
	Seed        int64      `json:"seed"`
	DurationMS  int64      `json:"duration_ms"`
	Status      string     `json:"status"`
	Scenarios   []Scenario `json:"scenarios"`
}

// Scenario is one .tales scenario projected for the visual replay.
type Scenario struct {
	Name       string     `json:"name"`
	File       string     `json:"file"`
	Status     string     `json:"status"`
	DurationMS int64      `json:"duration_ms"`
	SkipReason string     `json:"skip_reason,omitempty"`
	Steps      []Step     `json:"steps"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
}

// Artifact is one scenario-wide file (e.g. a screen recording) produced by
// a provider's ScenarioHook implementation. Step-level artifacts continue
// to live on the Action / Step view.
type Artifact struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// Step is one step within a scenario. Provider is preserved so the timeline
// can label HTTP/keyword steps as "no screenshots available".
type Step struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Phase      string   `json:"phase,omitempty"`
	Status     string   `json:"status"`
	DurationMS int64    `json:"duration_ms"`
	SkipReason string   `json:"skip_reason,omitempty"`
	Actions    []Action `json:"actions"`
}

// Action is one entry in the visual timeline.
type Action struct {
	Index      int    `json:"index"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	SelectorID string `json:"selector_id,omitempty"`
	Secure     bool   `json:"secure,omitempty"`
	Value      string `json:"value,omitempty"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Screenshot string `json:"screenshot,omitempty"`
	Hierarchy  string `json:"hierarchy,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Build produces a renderable Report from a SuiteResult. htmlPath is used to
// turn artifact paths into paths relative to the destination HTML file when
// possible; on filepath.Rel failure (different volumes, missing input) the
// original path is preserved so the report still works locally.
func Build(result *report.SuiteResult, htmlPath string) Report {
	if result == nil {
		return Report{
			Title:       visualReportTitle,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Status:      string(report.StatusUnknown),
			Scenarios:   []Scenario{},
		}
	}

	htmlDir := ""
	if htmlPath != "" {
		htmlDir = filepath.Dir(htmlPath)
	}

	out := Report{
		Title:       visualReportTitle,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Seed:        result.Seed,
		DurationMS:  result.Duration.Milliseconds(),
		Status:      suiteStatus(result),
		Scenarios:   make([]Scenario, 0, len(result.Scenarios)),
	}

	for _, sc := range result.Scenarios {
		if sc == nil {
			continue
		}

		out.Scenarios = append(out.Scenarios, buildScenario(sc, htmlDir))
	}

	if teardown, ok := buildSuiteTeardown(result, htmlDir); ok {
		out.Scenarios = append(out.Scenarios, teardown)
	}

	return out
}

// buildSuiteTeardown projects the suite-level teardown block as a trailing
// pseudo-scenario, so the timeline shows it last without the template needing
// a new concept. Returns ok=false when the suite declares no teardown.
func buildSuiteTeardown(result *report.SuiteResult, htmlDir string) (Scenario, bool) {
	if len(result.Teardown) == 0 {
		return Scenario{}, false
	}

	status := report.StatusPass
	if len(result.TeardownFailures) > 0 {
		status = report.StatusFail
	}

	scenario := Scenario{
		Name:   suiteTeardownName,
		Status: string(status),
		Steps:  make([]Step, 0, len(result.Teardown)),
	}

	for _, st := range stepsInExecutionOrder(result.Teardown) {
		scenario.File = st.File
		scenario.Steps = append(scenario.Steps, buildStep(st, htmlDir))
	}

	return scenario, true
}

func buildScenario(sc *report.ScenarioResult, htmlDir string) Scenario {
	scenario := Scenario{
		Name:       sc.Name,
		File:       sc.File,
		Status:     string(sc.Status),
		DurationMS: sc.Duration.Milliseconds(),
		SkipReason: sc.SkipReason,
		Steps:      make([]Step, 0, len(sc.Steps)+len(sc.Teardown)),
		Artifacts:  buildScenarioArtifacts(sc.Artifacts, htmlDir),
	}

	// The runner sorts sc.Steps alphabetically for stable console / JSONL
	// output; the visual replay wants execution order instead so the user
	// scrolls through the timeline in the order the test actually ran. Sort
	// by StartedAt (populated by the runner before each step starts). Zero
	// StartedAt sinks to the bottom — never crashes when the field is unset.
	for _, st := range stepsInExecutionOrder(sc.Steps) {
		scenario.Steps = append(scenario.Steps, buildStep(st, htmlDir))
	}

	for _, st := range stepsInExecutionOrder(sc.Teardown) {
		scenario.Steps = append(scenario.Steps, buildStep(st, htmlDir))
	}

	return scenario
}

func stepsInExecutionOrder(in []*report.StepResult) []*report.StepResult {
	out := make([]*report.StepResult, 0, len(in))

	for _, st := range in {
		if st == nil {
			continue
		}

		out = append(out, st)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].StartedAt, out[j].StartedAt
		if a.IsZero() && b.IsZero() {
			return false
		}

		if a.IsZero() {
			return false
		}

		if b.IsZero() {
			return true
		}

		return a.Before(b)
	})

	return out
}

func buildStep(st *report.StepResult, htmlDir string) Step {
	step := Step{
		Name:       st.Name,
		Provider:   st.Provider,
		Phase:      st.Phase,
		Status:     string(st.Status),
		DurationMS: st.Duration.Milliseconds(),
		SkipReason: st.SkipReason,
		Actions:    make([]Action, 0, len(st.Actions)),
	}

	for _, a := range st.Actions {
		if a == nil {
			continue
		}

		step.Actions = append(step.Actions, buildAction(a, htmlDir))
	}

	return step
}

func buildAction(a *report.ActionResult, htmlDir string) Action {
	out := Action{
		Index:      a.Index,
		Kind:       a.Kind,
		Label:      a.Label,
		SelectorID: a.SelectorID,
		Secure:     a.Secure,
		Value:      a.Value,
		Status:     string(a.Status),
		DurationMS: a.Duration.Milliseconds(),
		Screenshot: relativeArtifactPath(a.Screenshot, htmlDir),
		Hierarchy:  relativeArtifactPath(a.Hierarchy, htmlDir),
	}

	if a.Error != nil {
		out.Error = a.Error.Message
	}

	return out
}

// buildScenarioArtifacts copies the scenario-level artifacts produced by
// ScenarioHook implementations into the visual payload, rewriting paths to
// be relative to the destination HTML directory so the report stays
// portable when shipped alongside its build/artifacts/ tree.
func buildScenarioArtifacts(in []report.Artifact, htmlDir string) []Artifact {
	if len(in) == 0 {
		return nil
	}

	out := make([]Artifact, 0, len(in))

	for _, a := range in {
		out = append(out, Artifact{
			Type: a.Type,
			Path: relativeArtifactPath(a.Path, htmlDir),
		})
	}

	return out
}

// relativeArtifactPath returns the path relative to htmlDir when possible.
// Empty input is returned as-is. If filepath.Rel fails (e.g. cross-volume on
// Windows, or htmlDir is empty), the original path is returned so the report
// still works when opened locally.
func relativeArtifactPath(path, htmlDir string) string {
	if path == "" || htmlDir == "" {
		return path
	}

	rel, err := filepath.Rel(htmlDir, path)
	if err != nil {
		return path
	}

	return filepath.ToSlash(rel)
}

// suiteStatus returns the aggregate status as a string. Marks fail when any
// scenario failed, otherwise pass. Defensive against nil entries in the
// scenario slice (defensive enough to match buildScenario's nil drop).
func suiteStatus(result *report.SuiteResult) string {
	if len(result.TeardownFailures) > 0 {
		return string(report.StatusFail)
	}

	for _, sc := range result.Scenarios {
		if sc == nil {
			continue
		}

		if sc.Status == report.StatusFail {
			return string(report.StatusFail)
		}
	}

	return string(report.StatusPass)
}
