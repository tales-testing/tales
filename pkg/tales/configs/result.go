package configs

import "time"

// Result struct
type Result struct {
	Name     string
	Status   StatusType
	Raison   string
	Duration time.Duration
}

// FromErr sets error status to Result
func (r *Result) FromErr(err error) {
	r.Status = StatusFailed
	r.Raison = err.Error()
}

// ResultFailed return Result with error
func ResultFailed(err error) Result {
	return Result{
		Status: StatusFailed,
		Raison: err.Error(),
	}
}
