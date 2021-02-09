package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/euskadi31/go-einfo/colors"
	"github.com/euskadi31/go-einfo/terminal"
	ctyjson "github.com/zclconf/go-cty/cty/json"
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

func (r *ConsoleReporter) prefixString(prefix string, content string) string {
	lines := strings.Split(content, "\n")

	for i := 0; i < len(lines); i++ {
		lines[i] = prefix + lines[i]
	}

	return strings.Join(lines, "\n")
}

func (r *ConsoleReporter) printCaseData(c *Case) {
	if !c.Input.IsNull() {
		b, err := ctyjson.Marshal(c.Input, c.Input.Type())
		if err != nil {
			fmt.Fprint(r.out, err)
		}

		var data interface{}

		if err := json.Unmarshal(b, &data); err != nil {
			fmt.Fprint(r.out, err)
		}

		b, err = json.MarshalIndent(data, "", "    ")
		if err != nil {
			fmt.Fprint(r.out, err)
		}

		fmt.Fprint(r.out, "\t\tRequest body:\n")
		fmt.Fprint(r.out, r.prefixString("\t\t", string(b))+"\n")
	}

	if !c.Output.IsNull() {
		b, err := ctyjson.Marshal(c.Output, c.Output.Type())
		if err != nil {
			fmt.Fprint(r.out, err)
		}

		var data interface{}

		if err := json.Unmarshal(b, &data); err != nil {
			fmt.Fprint(r.out, err)
		}

		b, err = json.MarshalIndent(data, "", "    ")
		if err != nil {
			fmt.Fprint(r.out, err)
		}

		fmt.Fprint(r.out, "\t\tResponse body:\n")
		fmt.Fprint(r.out, r.prefixString("\t\t", string(b))+"\n")
	}
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

		r.printCaseData(c)
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
