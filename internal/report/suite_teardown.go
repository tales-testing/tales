package report

import (
	"fmt"
	"io"
)

// suiteTeardownLabel is the phase name used across the console, JSONL and
// JUnit renderings of the suite-level teardown block.
const suiteTeardownLabel = "suite_teardown"

// printSuiteTeardown renders the suite-level teardown below the last scenario.
// It prints nothing when the suite declares none, so the output of every
// existing suite stays byte-identical.
func printSuiteTeardown(out io.Writer, result *SuiteResult, stats *consoleStats, options ConsoleOptions, painter colorPainter) error {
	if len(result.Teardown) == 0 {
		return nil
	}

	header := painter.paint(ansiCyan, "Suite teardown")

	if _, err := fmt.Fprintf(out, "%s (%s)\n", header, suiteTeardownFile(result)); err != nil {
		return fmt.Errorf("print suite teardown header: %w", err)
	}

	for _, step := range result.Teardown {
		stats.currentStep++
		updateStepStats(stats, step.Status)

		if options.Progress {
			if err := printProgress(out, stats, painter); err != nil {
				return err
			}
		}

		if err := printStep(out, "teardown", 16, step, painter); err != nil {
			return fmt.Errorf("print suite teardown step %q: %w", step.Name, err)
		}
	}

	return nil
}

// suiteTeardownFile returns the .tales file that declared the block, taken
// from the first step since a single file owns the whole block.
func suiteTeardownFile(result *SuiteResult) string {
	if len(result.Teardown) == 0 || result.Teardown[0].File == "" {
		return "suite"
	}

	return result.Teardown[0].File
}
