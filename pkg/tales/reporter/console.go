package reporter

import (
	"fmt"
	"os"
	"time"

	"github.com/euskadi31/go-einfo/colors"
	"github.com/euskadi31/go-einfo/terminal"
)

var colorsMap = map[StatusType]string{
	StatusFailed:      colors.Red,
	StatusPassed:      colors.Green,
	StatusNotExecuted: colors.Yellow,
	StatusSkipped:     colors.LightGray,
}

func colorString(f *os.File, color string, value string) string {
	if !terminal.IsColor(f) {
		return value
	}

	return fmt.Sprintf("%s%s%s", color, value, colors.ResetAll)
}

func init() {
	Register("console", &ConsoleReporter{
		out: os.Stdout,
	})
}

// ConsoleReporter struct
type ConsoleReporter struct {
	out       *os.File
	scenarios []*Scenario
	last      *Scenario
	start     time.Time
}

// Start implements Reporter
func (r *ConsoleReporter) Start() error {
	r.start = time.Now()

	return nil
}

// ReportScenario implements Reporter
func (r *ConsoleReporter) ReportScenario(s *Scenario) error {
	r.scenarios = append(r.scenarios, s)
	r.last = s

	if _, err := fmt.Fprintf(r.out, "%s %s%s\n", colorString(r.out, colors.DarkGray, "Scenario"), s.Name, colorString(r.out, colors.DarkGray, "...")); err != nil {
		return err
	}

	return nil
}

// ReportCase implements Reporter
func (r *ConsoleReporter) ReportCase(c *Case) error {
	r.last.Cases = append(r.last.Cases, c)

	if c.Status == StatusFailed {
	}

	if _, err := fmt.Fprintf(r.out, "\t%s %s %s %s %s\n", colorString(r.out, colors.DarkGray, "Case"), c.Name, colorString(r.out, colorsMap[c.Status], string(c.Status)), colorString(r.out, colors.DarkGray, "in"), c.Duration); err != nil {
		return err
	}

	r.last.Duration += c.Duration

	if c.Status == StatusFailed {
		r.last.Status = StatusFailed

		if _, err := fmt.Fprintf(r.out, "\t\tError: %s\n", c.Raison); err != nil {
			return err
		}
	}

	return nil
}

// Stop implements Reporter
func (r *ConsoleReporter) Stop() error {
	scenarioPassed := 0
	scenarioCount := len(r.scenarios)
	caseCount := 0
	casePassed := 0
	duration := time.Since(r.start)

	for _, s := range r.scenarios {
		caseCount += len(s.Cases)

		s.Status = StatusPassed

		for _, c := range s.Cases {
			if c.Status == StatusPassed {
				casePassed++
			} else if c.Status == StatusFailed {
				s.Status = StatusFailed
			}
		}

		if s.Status == StatusPassed {
			scenarioPassed++
		}
	}

	if _, err := fmt.Fprintf(r.out, "\nScenarios:\t%s, %d total\n", colorString(r.out, colors.Green, fmt.Sprintf("%d passed", scenarioPassed)), scenarioCount); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(r.out, "Cases:\t\t%s, %d total\n", colorString(r.out, colors.Green, fmt.Sprintf("%d passed", casePassed)), caseCount); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(r.out, "Time:\t\t%s\n", duration); err != nil {
		return err
	}

	return nil
}
