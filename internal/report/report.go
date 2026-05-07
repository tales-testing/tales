package report

import "time"

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
}

// StepResult contains one step execution.
type StepResult struct {
	File       string
	Scenario   string
	Name       string
	Provider   string
	Phase      string
	Status     Status
	Duration   time.Duration
	Request    map[string]string
	Response   map[string]string
	StatusCode int
	Failure    *ErrorDetail
}

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
