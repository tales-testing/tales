package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/hyperxlab/tales/internal/diagnostic"
)

const failurePrefixDefault = "failure"

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

	if scenario.Failure != nil && findFirstFailedStep(scenario) == nil {
		if _, err := fmt.Fprintf(out, "  scenario failure:\\n"); err != nil {
			return fmt.Errorf("print scenario failure title: %w", err)
		}

		if err := printFailure(out, scenario.Failure); err != nil {
			return err
		}
	}

	if scenario.Failure != nil {
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
	if _, err := fmt.Fprintf(out, "  %s %-*s [%s] %-7s %s\n", label, width, step.Name, step.Provider, strings.ToUpper(string(step.Status)), step.Duration); err != nil {
		return fmt.Errorf("print line: %w", err)
	}

	if step.Failure == nil {
		return nil
	}

	if err := printFailure(out, step.Failure); err != nil {
		return err
	}

	if len(step.Request) > 0 {
		if err := printRequest(out, step.Request); err != nil {
			return err
		}
	}

	if len(step.Response) > 0 {
		if err := printResponse(out, step.StatusCode, step.Response); err != nil {
			return err
		}
	}

	return nil
}

func printFailure(out io.Writer, failure *ErrorDetail) error {
	prefix := failurePrefix(failure)
	if failure.Path != "" {
		prefix = fmt.Sprintf("%s at %s", prefix, failure.Path)
	}

	if _, err := fmt.Fprintf(out, "    %s\n", prefix); err != nil {
		return fmt.Errorf("print failure prefix: %w", err)
	}

	if failure.Message != "" {
		if _, err := fmt.Fprintf(out, "      message: %s\n", failure.Message); err != nil {
			return fmt.Errorf("print failure message: %w", err)
		}
	}

	if failure.Want != nil {
		if _, err := fmt.Fprintf(out, "      want: %s\n", diagnostic.ScalarString(failure.Want)); err != nil {
			return fmt.Errorf("print failure want: %w", err)
		}
	}

	if failure.Got != nil {
		if _, err := fmt.Fprintf(out, "      got:  %s\n", diagnostic.ScalarString(failure.Got)); err != nil {
			return fmt.Errorf("print failure got: %w", err)
		}
	}

	return nil
}

func failurePrefix(failure *ErrorDetail) string {
	if failure == nil {
		return failurePrefixDefault
	}

	if failure.Kind == "" {
		return failurePrefixDefault
	}

	if failure.Kind == "assertion" {
		return "assertion failed"
	}

	return failure.Kind + " failed"
}

func printRequest(out io.Writer, request map[string]interface{}) error {
	sanitized := diagnostic.SanitizeMap(request)

	request = sanitized

	method := stringField(request, "method")
	url := stringField(request, "url")

	if method != "" || url != "" {
		if _, err := fmt.Fprintf(out, "    Request:\n      %s %s\n", method, url); err != nil {
			return fmt.Errorf("print request line: %w", err)
		}
	} else if _, err := fmt.Fprintf(out, "    Request:\n"); err != nil {
		return fmt.Errorf("print request header: %w", err)
	}

	if err := printHeaders(out, request, "headers"); err != nil {
		return err
	}

	if jsonValue, ok := request["json"]; ok && jsonValue != nil {
		if _, err := fmt.Fprintf(out, "      JSON:\n%s\n", indentMultiline(diagnostic.PrettyJSON(jsonValue), "        ")); err != nil {
			return fmt.Errorf("print request json: %w", err)
		}
	}

	body := stringField(request, "body")
	if body != "" {
		if _, err := fmt.Fprintf(out, "      Body:\n%s\n", indentMultiline(body, "        ")); err != nil {
			return fmt.Errorf("print request body: %w", err)
		}
	}

	return nil
}

func printResponse(out io.Writer, statusCode int, response map[string]interface{}) error {
	sanitized := diagnostic.SanitizeMap(response)

	response = sanitized

	status := intField(response, "status")
	if status == 0 {
		status = statusCode
	}

	if _, err := fmt.Fprintf(out, "    Response:\n      status: %d\n", status); err != nil {
		return fmt.Errorf("print response status: %w", err)
	}

	if err := printHeaders(out, response, "headers"); err != nil {
		return err
	}

	if jsonValue, ok := response["json"]; ok && jsonValue != nil {
		if _, err := fmt.Fprintf(out, "      JSON:\n%s\n", indentMultiline(diagnostic.PrettyJSON(jsonValue), "        ")); err != nil {
			return fmt.Errorf("print response json: %w", err)
		}
	}

	body := stringField(response, "body")
	if body != "" {
		if _, err := fmt.Fprintf(out, "      Body:\n%s\n", indentMultiline(body, "        ")); err != nil {
			return fmt.Errorf("print response body: %w", err)
		}
	}

	return nil
}

func printHeaders(out io.Writer, values map[string]interface{}, key string) error {
	rawHeaders, ok := values[key]
	if !ok || rawHeaders == nil {
		return nil
	}

	headers := diagnostic.MaskHeaders(rawHeaders)
	if len(headers) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(out, "      Headers:\n"); err != nil {
		return fmt.Errorf("print headers title: %w", err)
	}

	for _, headerKey := range diagnostic.SortedHeaderKeys(headers) {
		if _, err := fmt.Fprintf(out, "        %s: %s\n", headerKey, headers[headerKey]); err != nil {
			return fmt.Errorf("print header %s: %w", headerKey, err)
		}
	}

	return nil
}

func stringField(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}

	if rendered, ok := value.(string); ok {
		return rendered
	}

	return diagnostic.ScalarString(value)
}

func intField(values map[string]interface{}, key string) int {
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func indentMultiline(value, indent string) string {
	if value == "" {
		return indent
	}

	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}

	return strings.Join(lines, "\n")
}
