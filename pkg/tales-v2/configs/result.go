package configs

import "time"

// StatusType enum type
type StatusType string

// StatusType enums
const (
	StatusPassed StatusType = "PASSED"
	StatusFailed StatusType = "FAILED"
)

// Result represents a result of scenario case
type Result struct {
	Name     string
	Status   StatusType
	Raison   string
	Duration time.Duration
}

// FromErr modify status of Result and set error message
func (r *Result) FromErr(err error) {
	r.Status = StatusFailed
	r.Raison = err.Error()
}
