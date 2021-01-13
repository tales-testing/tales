package reporter

// StatusType type
type StatusType string

// StatusType enums
const (
	StatusPassed      StatusType = "PASSED"
	StatusFailed      StatusType = "FAILED"
	StatusNotExecuted StatusType = "NOT EXECUTED"
	StatusSkipped     StatusType = "SKIPPED"
)
