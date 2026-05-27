package report

import "time"

// ActiveScenario describes one in-flight scenario at the moment a heartbeat
// snapshot is taken. The runtime computes these and hands them to the sink;
// the type lives here because runtime imports report (not the other way),
// so it is the natural shared home.
type ActiveScenario struct {
	Name    string
	Elapsed time.Duration
}

// Status describes suite/scenario/step state.
type Status string

const (
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusSkip    Status = "skipped"
	StatusUnknown Status = "unknown"
)

// SuiteResult is aggregate test execution result.
type SuiteResult struct {
	Seed      int64
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
	Scenarios []*ScenarioResult
	// StalledScenarios holds the names of scenarios that were still
	// in-flight when the runner's parent context hit its deadline. It is
	// populated only when --timeout (or any other context.WithTimeout
	// wrapper above the runner) fires before the suite finishes, and is
	// used by the CLI to surface the culprits in the cancel message.
	StalledScenarios []string
}

// ScenarioResult contains one scenario execution.
type ScenarioResult struct {
	File             string
	Name             string
	Tags             []string
	Status           Status
	Duration         time.Duration
	Steps            []*StepResult
	Teardown         []*StepResult
	Failure          *ErrorDetail
	TeardownFailures []*ErrorDetail
	// SkipReason is set when Status == StatusSkip. Populated by scenario-
	// level skip_if / skip_unless rules in the runtime; empty otherwise.
	SkipReason string
}

// StepResult contains one step execution.
type StepResult struct {
	File       string
	Scenario   string
	Name       string
	Provider   string
	Phase      string
	Status     Status
	StartedAt  time.Time
	Duration   time.Duration
	Request    map[string]interface{}
	Response   map[string]interface{}
	StatusCode int
	Attempts   int
	Failure    *ErrorDetail
	Artifacts  []Artifact
	Actions    []*ActionResult
	// SkipReason is set when Status == StatusSkip. Populated by step-
	// level skip_if / skip_unless rules in the runtime, by the dependency
	// cascade, or by teardown when its `when` predicate is false; empty
	// otherwise.
	SkipReason string
}

// ActionResult describes one UI action executed within a step (mobile today,
// web later). The visual HTML report, the optional JSONL action events, and
// future provider-agnostic consumers all read from this typed shape.
//
// Secure actions MUST carry Value == "***" — masking happens at the single
// site where the result is constructed, never at render time.
type ActionResult struct {
	Index      int           `json:"index"`
	Kind       string        `json:"kind"`
	Label      string        `json:"label"`
	SelectorID string        `json:"selector_id,omitempty"`
	Secure     bool          `json:"secure,omitempty"`
	Value      string        `json:"value,omitempty"`
	Status     Status        `json:"status"`
	Duration   time.Duration `json:"-"`
	Screenshot string        `json:"screenshot,omitempty"`
	Hierarchy  string        `json:"hierarchy,omitempty"`
	Error      *ErrorDetail  `json:"error,omitempty"`
	StartedAt  time.Time     `json:"started_at,omitzero"`
}

// Artifact references one file produced by a step (mobile screenshots,
// hierarchy dumps, ...). Path is the value produced by the provider — usually
// relative to the working directory when the default artifacts base is used,
// but can be absolute when callers override the base directory (for example,
// tests using a temp dir).
type Artifact struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

const artifactFallbackLabel = "artifact"

// ErrorDetail is compact machine+human readable failure details.
type ErrorDetail struct {
	Kind    string      `json:"kind"`
	Path    string      `json:"path,omitempty"`
	Want    interface{} `json:"want,omitempty"`
	Got     interface{} `json:"got,omitempty"`
	Message string      `json:"message"`
}

// Failed returns true when suite has at least one failed scenario.
func (r *SuiteResult) Failed() bool {
	for _, scenario := range r.Scenarios {
		if scenario.Status == StatusFail {
			return true
		}
	}

	return false
}
