package report

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hyperxlab/tales/internal/diagnostic"
)

type testsuiteXML struct {
	XMLName   xml.Name      `xml:"testsuite"`
	Name      string        `xml:"name,attr"`
	Tests     int           `xml:"tests,attr"`
	Failures  int           `xml:"failures,attr"`
	Time      string        `xml:"time,attr"`
	TestCases []testcaseXML `xml:"testcase"`
}

type testcaseXML struct {
	Name      string      `xml:"name,attr"`
	ClassName string      `xml:"classname,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *failureXML `xml:"failure,omitempty"`
}

type failureXML struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// WriteJUnit writes junit xml report with one testcase per scenario.
func WriteJUnit(path string, result *SuiteResult) error {
	x := testsuiteXML{Name: "tales", Tests: len(result.Scenarios), Time: seconds(result.Duration)}

	for _, scenario := range result.Scenarios {
		tc := testcaseXML{Name: scenario.Name, ClassName: scenario.File, Time: seconds(scenario.Duration)}
		if scenario.Status == StatusFail {
			x.Failures++

			message, body := buildJUnitFailure(result.Seed, scenario)
			tc.Failure = &failureXML{Message: message, Body: body}
		}

		x.TestCases = append(x.TestCases, tc)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create junit file %q: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	enc := xml.NewEncoder(file)
	enc.Indent("", "  ")

	if err := enc.Encode(x); err != nil {
		return fmt.Errorf("encode junit xml: %w", err)
	}

	return nil
}

func buildJUnitFailure(seed int64, scenario *ScenarioResult) (string, string) {
	message := "scenario failed"
	if scenario.Failure != nil {
		message = failurePrefix(scenario.Failure)

		if scenario.Failure.Path != "" {
			message = fmt.Sprintf("%s at %s", message, scenario.Failure.Path)
		}

		if scenario.Failure.Message != "" {
			message = scenario.Failure.Message
		}
	}

	failedStep := findFirstFailedStep(scenario)

	body := strings.Builder{}
	_, _ = fmt.Fprintf(&body, "scenario: %s\n", scenario.Name)
	_, _ = fmt.Fprintf(&body, "file: %s\n", scenario.File)
	_, _ = fmt.Fprintf(&body, "seed: %d\n", seed)

	if failedStep != nil {
		_, _ = fmt.Fprintf(&body, "step: %s\n", failedStep.Name)
		_, _ = fmt.Fprintf(&body, "provider: %s\n", failedStep.Provider)
	}

	if scenario.Failure != nil {
		_, _ = fmt.Fprintf(&body, "kind: %s\n", scenario.Failure.Kind)

		if scenario.Failure.Path != "" {
			_, _ = fmt.Fprintf(&body, "path: %s\n", scenario.Failure.Path)
		}

		if scenario.Failure.Want != nil {
			_, _ = fmt.Fprintf(&body, "want: %s\n", diagnostic.ScalarString(scenario.Failure.Want))
		}

		if scenario.Failure.Got != nil {
			_, _ = fmt.Fprintf(&body, "got: %s\n", diagnostic.ScalarString(scenario.Failure.Got))
		}

		if scenario.Failure.Message != "" {
			_, _ = fmt.Fprintf(&body, "message: %s\n", scenario.Failure.Message)
		}
	}

	if failedStep != nil {
		body.WriteString(renderRequestForJUnit(failedStep.Request))
		body.WriteString(renderResponseForJUnit(failedStep.Response, failedStep.StatusCode))
	}

	_, _ = fmt.Fprintf(&body, "replay: tales test --seed %d --scenario %q %s\n", seed, scenario.Name, scenario.File)

	return message, body.String()
}

func findFirstFailedStep(scenario *ScenarioResult) *StepResult {
	for _, step := range scenario.Steps {
		if step.Status == StatusFail {
			return step
		}
	}

	for _, step := range scenario.Teardown {
		if step.Status == StatusFail {
			return step
		}
	}

	return nil
}

func renderRequestForJUnit(request map[string]interface{}) string {
	if len(request) == 0 {
		return ""
	}

	sanitized := diagnostic.SanitizeMap(request)
	builder := strings.Builder{}
	builder.WriteString("request:\n")
	_, _ = fmt.Fprintf(&builder, "  method: %s\n", valueAsString(sanitized["method"]))
	_, _ = fmt.Fprintf(&builder, "  url: %s\n", valueAsString(sanitized["url"]))
	builder.WriteString(renderHeadersForJUnit(sanitized, "headers"))

	if value, ok := sanitized["body"]; ok && value != nil {
		builder.WriteString(renderRequestBodyForJUnit(value))
	}

	return builder.String()
}

func renderRequestBodyForJUnit(value interface{}) string {
	bodyMap, ok := value.(map[string]interface{})
	if !ok {
		body := valueAsString(value)
		if body == "" {
			return ""
		}

		builder := strings.Builder{}
		builder.WriteString("  body:\n")
		builder.WriteString(indentMultiline(body, "    "))
		builder.WriteString("\n")

		return builder.String()
	}

	builder := strings.Builder{}
	if jsonValue, ok := bodyMap["json"]; ok && jsonValue != nil {
		builder.WriteString("  json:\n")
		builder.WriteString(indentMultiline(diagnostic.PrettyJSON(jsonValue), "    "))
		builder.WriteString("\n")
	}

	if formValue, ok := bodyMap["form"]; ok && formValue != nil {
		builder.WriteString("  form:\n")
		builder.WriteString(indentMultiline(diagnostic.PrettyJSON(formValue), "    "))
		builder.WriteString("\n")
	}

	if rawValue, ok := bodyMap["raw"]; ok && rawValue != nil && valueAsString(rawValue) != "" {
		builder.WriteString("  body:\n")
		builder.WriteString(indentMultiline(valueAsString(rawValue), "    "))
		builder.WriteString("\n")
	}

	return builder.String()
}

func renderResponseForJUnit(response map[string]interface{}, statusCode int) string {
	if len(response) == 0 {
		return ""
	}

	sanitized := diagnostic.SanitizeMap(response)

	status := intField(sanitized, "status")
	if status == 0 {
		status = statusCode
	}

	builder := strings.Builder{}
	builder.WriteString("response:\n")
	_, _ = fmt.Fprintf(&builder, "  status: %d\n", status)
	builder.WriteString(renderHeadersForJUnit(sanitized, "headers"))

	if value, ok := sanitized["json"]; ok && value != nil {
		builder.WriteString("  json:\n")
		builder.WriteString(indentMultiline(diagnostic.PrettyJSON(value), "    "))
		builder.WriteString("\n")
	}

	if value, ok := sanitized["body"]; ok && value != nil && valueAsString(value) != "" {
		builder.WriteString("  body:\n")
		builder.WriteString(indentMultiline(valueAsString(value), "    "))
		builder.WriteString("\n")
	}

	return builder.String()
}

func renderHeadersForJUnit(values map[string]interface{}, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}

	headers := diagnostic.MaskHeaders(raw)
	if len(headers) == 0 {
		return ""
	}

	builder := strings.Builder{}
	builder.WriteString("  headers:\n")

	for _, headerName := range diagnostic.SortedHeaderKeys(headers) {
		_, _ = fmt.Fprintf(&builder, "    %s: %s\n", headerName, headers[headerName])
	}

	return builder.String()
}

func valueAsString(value interface{}) string {
	if value == nil {
		return ""
	}

	if text, ok := value.(string); ok {
		return text
	}

	return diagnostic.ScalarString(value)
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
