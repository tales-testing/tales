package reporter

import (
	"time"

	"github.com/zclconf/go-cty/cty"
)

// Report struct
type Report struct {
	Scenarios []*Scenario `json:"scenarios"`
}

// Scenario struct
type Scenario struct {
	Name     string        `json:"name"`
	Tags     []string      `json:"tags,omitempty"`
	Duration time.Duration `json:"duration"`
	Cases    []*Case       `json:"cases"`
	Status   StatusType    `json:"status"`
}

// Case struct
type Case struct {
	Name     string        `json:"name"`
	Status   StatusType    `json:"status"`
	Raison   string        `json:"raison,omitempty"`
	Input    cty.Value     `json:"input,omitempty"`
	Output   cty.Value     `json:"output,omitempty"`
	Duration time.Duration `json:"duration"`
}

// FromError sets error status to Result
func (c *Case) FromError(err error) {
	c.Status = StatusFailed
	c.Raison = err.Error()
}

// NewCaseFromError return Result with error
func NewCaseFromError(err error) *Case {
	return &Case{
		Status: StatusFailed,
		Raison: err.Error(),
	}
}
