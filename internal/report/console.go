package report

import (
	"fmt"
	"io"
	"strings"
)

// PrintConsole writes human-friendly report.
func PrintConsole(out io.Writer, result *SuiteResult) error {
	passedScenarios := 0
	failedScenarios := 0
	passedSteps := 0
	failedSteps := 0

	for _, scenario := range result.Scenarios {
		if scenario.Status == StatusPass {
			passedScenarios++
		} else {
			failedScenarios++
		}

		if _, err := fmt.Fprintf(out, "Scenario %s (%s) %s in %s\n", scenario.Name, scenario.File, strings.ToUpper(string(scenario.Status)), scenario.Duration); err != nil {
			return err
		}
		for _, step := range scenario.Steps {
			switch step.Status {
			case StatusPass:
				passedSteps++
			case StatusFail:
				failedSteps++
			}
			if _, err := fmt.Fprintf(out, "  step %-20s %-4s %s\n", step.Name, strings.ToUpper(string(step.Status)), step.Duration); err != nil {
				return err
			}
			if step.Failure != nil {
				if _, err := fmt.Fprintf(out, "    error: %s\n", step.Failure.Message); err != nil {
					return err
				}
				if len(step.Request) > 0 {
					if _, err := fmt.Fprintf(out, "    request: %v\n", step.Request); err != nil {
						return err
					}
				}
				if len(step.Response) > 0 {
					if _, err := fmt.Fprintf(out, "    response: %v\n", step.Response); err != nil {
						return err
					}
				}
			}
		}
		for _, step := range scenario.Teardown {
			if _, err := fmt.Fprintf(out, "  teardown %-16s %-4s %s\n", step.Name, strings.ToUpper(string(step.Status)), step.Duration); err != nil {
				return err
			}
			if step.Failure != nil {
				if _, err := fmt.Fprintf(out, "    teardown error: %s\n", step.Failure.Message); err != nil {
					return err
				}
			}
		}
		if scenario.Failure != nil {
			if _, err := fmt.Fprintf(out, "  failure: %s\n", scenario.Failure.Message); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "  replay: tales test --seed %d --scenario %q %s\n", result.Seed, scenario.Name, scenario.File); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintf(
		out,
		"\nSummary: scenarios %d passed / %d failed, steps %d passed / %d failed, duration %s, seed %d\n",
		passedScenarios,
		failedScenarios,
		passedSteps,
		failedSteps,
		result.Duration,
		result.Seed,
	)
	return err
}
