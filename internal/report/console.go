package report

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/tales-testing/tales/internal/diagnostic"
)

const (
	failurePrefixDefault = "failure"
	ansiReset            = "\x1b[0m"
	ansiRed              = "\x1b[31m"
	ansiGreen            = "\x1b[32m"
	ansiYellow           = "\x1b[33m"
	ansiBlue             = "\x1b[34m"
	ansiCyan             = "\x1b[36m"
	ansiGray             = "\x1b[90m"
)

// ConsoleOptions controls CLI human output behavior.
type ConsoleOptions struct {
	Color    bool
	Progress bool
}

// DefaultConsoleOptions computes default console options from writer environment.
func DefaultConsoleOptions(out io.Writer) ConsoleOptions {
	enableColor := supportsTerminalFeatures(out)

	return ConsoleOptions{
		Color:    enableColor,
		Progress: enableColor,
	}
}

// PrintConsole writes human-friendly report.
func PrintConsole(out io.Writer, result *SuiteResult) error {
	return PrintConsoleWithOptions(out, result, DefaultConsoleOptions(out))
}

// PrintConsoleWithOptions writes human-friendly report with explicit options.
func PrintConsoleWithOptions(out io.Writer, result *SuiteResult, options ConsoleOptions) error {
	stats := newConsoleStats(result)
	painter := newColorPainter(options.Color)

	for _, scenario := range result.Scenarios {
		stats.currentScenario++

		if err := printScenario(out, result.Seed, scenario, stats, options, painter); err != nil {
			return fmt.Errorf("print scenario %q: %w", scenario.Name, err)
		}
	}

	summaryLabel := painter.paint(ansiBlue, "Summary")

	if _, err := fmt.Fprintf(
		out,
		"\n%s: scenarios %d passed / %d failed / %d skipped, steps %d passed / %d failed, skipped %d, duration %s, seed %d\n",
		summaryLabel,
		stats.passedScenarios,
		stats.failedScenarios,
		stats.skippedScenarios,
		stats.passedSteps,
		stats.failedSteps,
		stats.skippedSteps,
		result.Duration,
		result.Seed,
	); err != nil {
		return fmt.Errorf("print summary: %w", err)
	}

	return nil
}

type consoleStats struct {
	totalScenarios   int
	totalSteps       int
	currentScenario  int
	currentStep      int
	passedScenarios  int
	failedScenarios  int
	skippedScenarios int
	passedSteps      int
	failedSteps      int
	skippedSteps     int
}

func newConsoleStats(result *SuiteResult) *consoleStats {
	stats := &consoleStats{totalScenarios: len(result.Scenarios)}

	for _, scenario := range result.Scenarios {
		stats.totalSteps += len(scenario.Steps)
		stats.totalSteps += len(scenario.Teardown)
	}

	return stats
}

type colorPainter struct {
	enabled bool
}

func newColorPainter(enabled bool) colorPainter {
	return colorPainter{enabled: enabled}
}

func (c colorPainter) paint(colorCode, value string) string {
	if !c.enabled || value == "" {
		return value
	}

	return colorCode + value + ansiReset
}

func (c colorPainter) status(value Status) string {
	upper := strings.ToUpper(string(value))

	return c.colorizeStatus(value, upper)
}

func (c colorPainter) statusPadded(value Status, width int) string {
	plain := fmt.Sprintf("%-*s", width, strings.ToUpper(string(value)))

	return c.colorizeStatus(value, plain)
}

func (c colorPainter) colorizeStatus(value Status, rendered string) string {
	switch value {
	case StatusPass:
		return c.paint(ansiGreen, rendered)
	case StatusFail:
		return c.paint(ansiRed, rendered)
	case StatusSkip:
		return c.paint(ansiYellow, rendered)
	case StatusUnknown:
		return c.paint(ansiGray, rendered)
	default:
		return c.paint(ansiGray, rendered)
	}
}

func printScenario(out io.Writer, seed int64, scenario *ScenarioResult, stats *consoleStats, options ConsoleOptions, painter colorPainter) error {
	updateScenarioStats(stats, scenario.Status)

	if err := printScenarioHeader(out, scenario, painter); err != nil {
		return err
	}

	for _, step := range scenario.Steps {
		stats.currentStep++
		updateStepStats(stats, step.Status)

		if options.Progress {
			if err := printProgress(out, stats, painter); err != nil {
				return err
			}
		}

		if err := printStep(out, "step", 20, step, painter); err != nil {
			return fmt.Errorf("print step %q: %w", step.Name, err)
		}
	}

	for _, step := range scenario.Teardown {
		stats.currentStep++
		updateStepStats(stats, step.Status)

		if options.Progress {
			if err := printProgress(out, stats, painter); err != nil {
				return err
			}
		}

		if err := printStep(out, "teardown", 16, step, painter); err != nil {
			return fmt.Errorf("print teardown step %q: %w", step.Name, err)
		}
	}

	if scenario.Failure != nil && findFirstFailedStep(scenario) == nil {
		if _, err := fmt.Fprintf(out, "  scenario failure:\n"); err != nil {
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

func printProgress(out io.Writer, stats *consoleStats, painter colorPainter) error {
	text := fmt.Sprintf(
		"[ scenario: %d/%d, step: %d/%d, skip: %d, success: %d, failure: %d ]",
		stats.currentScenario,
		stats.totalScenarios,
		stats.currentStep,
		stats.totalSteps,
		stats.skippedSteps,
		stats.passedSteps,
		stats.failedSteps,
	)

	line := painter.paint(ansiGray, text)
	if _, err := fmt.Fprintf(out, "  %s\n", line); err != nil {
		return fmt.Errorf("print progress: %w", err)
	}

	return nil
}

func updateStepStats(stats *consoleStats, status Status) {
	switch status {
	case StatusPass:
		stats.passedSteps++
	case StatusFail:
		stats.failedSteps++
	case StatusSkip:
		stats.skippedSteps++
	case StatusUnknown:
		return
	}
}

func updateScenarioStats(stats *consoleStats, status Status) {
	switch status {
	case StatusPass:
		stats.passedScenarios++
	case StatusSkip:
		stats.skippedScenarios++
	case StatusFail, StatusUnknown:
		stats.failedScenarios++
	}
}

func printScenarioHeader(out io.Writer, scenario *ScenarioResult, painter colorPainter) error {
	scenarioLabel := painter.paint(ansiCyan, "Scenario")
	statusLabel := painter.status(scenario.Status)

	if _, err := fmt.Fprintf(out, "%s %s (%s) %s in %s\n", scenarioLabel, scenario.Name, scenario.File, statusLabel, scenario.Duration); err != nil {
		return fmt.Errorf("print scenario header: %w", err)
	}

	if scenario.Status == StatusSkip && scenario.SkipReason != "" {
		if _, err := fmt.Fprintf(out, "  reason: %s\n", scenario.SkipReason); err != nil {
			return fmt.Errorf("print scenario skip reason: %w", err)
		}
	}

	return nil
}

func printStep(out io.Writer, label string, width int, step *StepResult, painter colorPainter) error {
	statusLabel := painter.statusPadded(step.Status, 7)

	attempts := ""
	if step.Attempts > 1 {
		attempts = fmt.Sprintf(" attempts=%d", step.Attempts)
	}

	if _, err := fmt.Fprintf(out, "  %s %-*s [%s] %s %s%s\n", label, width, step.Name, step.Provider, statusLabel, step.Duration, attempts); err != nil {
		return fmt.Errorf("print line: %w", err)
	}

	if step.Status == StatusSkip && step.SkipReason != "" {
		if _, err := fmt.Fprintf(out, "    reason: %s\n", step.SkipReason); err != nil {
			return fmt.Errorf("print step skip reason: %w", err)
		}
	}

	if step.Failure == nil {
		return nil
	}

	if err := printFailure(out, step.Failure); err != nil {
		return err
	}

	if err := printActionSummaries(out, step.Request); err != nil {
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

	if len(step.Artifacts) > 0 {
		if err := printArtifacts(out, step.Artifacts); err != nil {
			return err
		}
	}

	return nil
}

func printActionSummaries(out io.Writer, request map[string]interface{}) error {
	if len(request) == 0 {
		return nil
	}

	actions, ok := request["actions"].([]map[string]any)
	if !ok || len(actions) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(out, "    Actions:"); err != nil {
		return fmt.Errorf("print actions header: %w", err)
	}

	for _, action := range actions {
		if _, err := fmt.Fprintf(out, "      %s\n", renderActionSummary(action)); err != nil {
			return fmt.Errorf("print action summary: %w", err)
		}
	}

	return nil
}

func renderActionSummary(action map[string]any) string {
	kind := stringField(action, "kind")
	id := stringField(action, "id")

	rendered := kind
	if id != "" {
		rendered += " id=" + id
	}

	if value, ok := action["value"]; ok && value != nil {
		rendered += " value=" + diagnostic.ScalarString(value)
	}

	return rendered
}

func printArtifacts(out io.Writer, artifacts []Artifact) error {
	if _, err := fmt.Fprintln(out, "    artifacts:"); err != nil {
		return fmt.Errorf("print artifacts header: %w", err)
	}

	for _, a := range artifacts {
		label := a.Type
		if label == "" {
			label = artifactFallbackLabel
		}

		if _, err := fmt.Fprintf(out, "      %s: %s\n", label, a.Path); err != nil {
			return fmt.Errorf("print artifact %s: %w", a.Type, err)
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

	if bodyValue, ok := request["body"]; ok && bodyValue != nil {
		if err := printRequestBody(out, bodyValue); err != nil {
			return err
		}
	}

	return nil
}

func printRequestBody(out io.Writer, bodyValue interface{}) error {
	bodyMap, ok := bodyValue.(map[string]interface{})
	if !ok {
		body := diagnostic.ScalarString(bodyValue)
		if body == "" || body == "null" {
			return nil
		}

		if _, err := fmt.Fprintf(out, "      Body:\n%s\n", indentMultiline(body, "        ")); err != nil {
			return fmt.Errorf("print request body: %w", err)
		}

		return nil
	}

	if jsonValue, ok := bodyMap["json"]; ok && jsonValue != nil {
		if _, err := fmt.Fprintf(out, "      JSON:\n%s\n", indentMultiline(diagnostic.PrettyJSON(jsonValue), "        ")); err != nil {
			return fmt.Errorf("print request json: %w", err)
		}
	}

	if formValue, ok := bodyMap["form"]; ok && formValue != nil {
		if _, err := fmt.Fprintf(out, "      Form:\n%s\n", indentMultiline(diagnostic.PrettyJSON(formValue), "        ")); err != nil {
			return fmt.Errorf("print request form: %w", err)
		}
	}

	raw := stringField(bodyMap, "raw")
	if raw != "" {
		if _, err := fmt.Fprintf(out, "      Body:\n%s\n", indentMultiline(raw, "        ")); err != nil {
			return fmt.Errorf("print request raw body: %w", err)
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
		for _, value := range headers[headerKey] {
			if _, err := fmt.Fprintf(out, "        %s: %s\n", headerKey, value); err != nil {
				return fmt.Errorf("print header %s: %w", headerKey, err)
			}
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

func supportsTerminalFeatures(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}

	if !isatty.IsTerminal(file.Fd()) && !isatty.IsCygwinTerminal(file.Fd()) {
		return false
	}

	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return false
	}

	return true
}
