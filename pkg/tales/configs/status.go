package configs

// StatusType type
type StatusType string

// StatusType enums
const (
	StatusPassed      StatusType = "PASSED"
	StatusFailed      StatusType = "FAILED"
	StatusNotExecuted StatusType = "NOT EXECUTED"
)
