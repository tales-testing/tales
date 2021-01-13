package reporter

import (
	"encoding/json"
	"io"
	"os"
)

func init() {
	Register("json", &JSONReporter{
		out: os.Stdout,
	})
}

// JSONReporter struct
type JSONReporter struct {
	out       io.Writer
	scenarios []*Scenario
	last      *Scenario
}

// Start implements Reporter
func (r *JSONReporter) Start() error {
	return nil
}

// ReportScenario implements Reporter
func (r *JSONReporter) ReportScenario(s *Scenario) error {
	r.scenarios = append(r.scenarios, s)
	r.last = s

	return nil
}

// ReportCase implements Reporter
func (r *JSONReporter) ReportCase(c *Case) error {
	r.last.Cases = append(r.last.Cases, c)

	r.last.Duration += c.Duration

	if c.Status == StatusFailed {
		r.last.Status = StatusFailed
	}

	return nil
}

// Stop implements Reporter
func (r *JSONReporter) Stop() error {
	report := &Report{
		Scenarios: r.scenarios,
	}

	for _, s := range r.scenarios {
		s.Status = StatusPassed

		for _, c := range s.Cases {
			if c.Status == StatusFailed {
				s.Status = StatusFailed
			}
		}
	}

	return json.NewEncoder(r.out).Encode(report)
}
