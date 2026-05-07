package report

import (
	"fmt"
	"io"
	"strings"
)

// PrintConsole writes human-friendly report.
func PrintConsole(out io.Writer, result *SuiteResult) error {
	stats := &consoleStats{}
	for _, scenario := range result.Scenarios {
		if err := printScenario(out, result.Seed, scenario, stats); err != nil {
			return fmt.Errorf("print scenario %q: %w", scenario.Name, err)
		}
	}

	if _, err := fmt.Fprintf(
		out,
		"\nSummary: scenarios %d passed / %d failed, steps %d passed / %d failed, duration %s, seed %d\n",
		stats.passedScenarios,
		stats.failedScenarios,
		stats.passedSteps,
		stats.failedSteps,
		result.Duration,
		result.Seed,
	); err != nil {
		return fmt.Errorf("print summary: %w", err)
	}

	return nil
}

type consoleStats struct {
	passedScenarios int
	failedScenarios int
	passedSteps     int
	failedSteps     int
}

func printScenario(out io.Writer, seed int64, scenario *ScenarioResult, stats *consoleStats) error {
	if scenario.Status == StatusPass {
		stats.passedScenarios++
	} else {
		stats.failedScenarios++
	}

	if _, err := fmt.Fprintf(out, "Scenario %s (%s) %s in %s\n", scenario.Name, scenario.File, strings.ToUpper(string(scenario.Status)), scenario.Duration); err != nil {
		return fmt.Errorf("print scenario header: %w", err)
	}

	for _, step := range scenario.Steps {
		updateStepStats(stats, step.Status)

		if err := printStep(out, "step", 20, step); err != nil {
			return fmt.Errorf("print step %q: %w", step.Name, err)
		}
	}

	for _, step := range scenario.Teardown {
		if err := printStep(out, "teardown", 16, step); err != nil {
			return fmt.Errorf("print teardown step %q: %w", step.Name, err)
		}
	}

	if scenario.Failure != nil {
		if _, err := fmt.Fprintf(out, "  failure: %s\n", scenario.Failure.Message); err != nil {
			return fmt.Errorf("print scenario failure: %w", err)
		}

		if _, err := fmt.Fprintf(out, "  replay: tales test --seed %d --scenario %q %s\n", seed, scenario.Name, scenario.File); err != nil {
			return fmt.Errorf("print replay command: %w", err)
		}
	}

	return nil
}

func updateStepStats(stats *consoleStats, status Status) {
	switch status {
	case StatusPass:
		stats.passedSteps++
	case StatusFail:
		stats.failedSteps++
	case StatusSkip, StatusUnknown:
		return
	}
}

func printStep(out io.Writer, label string, width int, step *StepResult) error {
	if _, err := fmt.Fprintf(out, "  %s %-*s %-4s %s\n", label, width, step.Name, strings.ToUpper(string(step.Status)), step.Duration); err != nil {
		return fmt.Errorf("print line: %w", err)
	}

	if step.Failure == nil {
		return nil
	}

	errPrefix := "error"
	if label == "teardown" {
		errPrefix = "teardown error"
	}

	if _, err := fmt.Fprintf(out, "    %s: %s\n", errPrefix, step.Failure.Message); err != nil {
		return fmt.Errorf("print failure: %w", err)
	}

	if len(step.Request) > 0 {
		if _, err := fmt.Fprintf(out, "    request: %v\n", step.Request); err != nil {
			return fmt.Errorf("print request: %w", err)
		}
	}

	if len(step.Response) > 0 {
		if _, err := fmt.Fprintf(out, "    response: %v\n", step.Response); err != nil {
			return fmt.Errorf("print response: %w", err)
		}
	}

	return nil
}
