package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSkippedSuite() *SuiteResult {
	return &SuiteResult{
		Seed:     42,
		Duration: 10 * time.Millisecond,
		Scenarios: []*ScenarioResult{
			{
				File:       "e2e/pass/example.tales",
				Name:       "scenario skipped by rule",
				Status:     StatusSkip,
				SkipReason: "requires macOS",
				Duration:   1 * time.Millisecond,
			},
			{
				File:     "e2e/pass/other.tales",
				Name:     "scenario with step skipped",
				Status:   StatusPass,
				Duration: 2 * time.Millisecond,
				Steps: []*StepResult{
					{
						File:       "e2e/pass/other.tales",
						Scenario:   "scenario with step skipped",
						Name:       "skipped_step",
						Provider:   "http",
						Status:     StatusSkip,
						SkipReason: "ENABLE_DEBUG=1 required",
					},
				},
			},
		},
	}
}

func TestPrintConsoleSurfacesSkipReasons(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	if err := PrintConsoleWithOptions(buf, newSkippedSuite(), ConsoleOptions{Color: false, Progress: false}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "reason: requires macOS") {
		t.Fatalf("console output missing scenario skip reason:\n%s", out)
	}

	if !strings.Contains(out, "reason: ENABLE_DEBUG=1 required") {
		t.Fatalf("console output missing step skip reason:\n%s", out)
	}

	if !strings.Contains(out, "1 passed / 0 failed / 1 skipped") {
		t.Fatalf("console summary missing skipped scenario count:\n%s", out)
	}
}

func TestWriteJSONLEmitsSkipReason(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := WriteJSONL(path, newSkippedSuite()); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}

	defer func() { _ = file.Close() }()

	var (
		foundScenarioReason bool
		foundStepReason     bool
	)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if event["type"] == "scenario" && event["scenario"] == "scenario skipped by rule" {
			if event["skip_reason"] != "requires macOS" {
				t.Fatalf("scenario skip_reason = %v", event["skip_reason"])
			}

			foundScenarioReason = true
		}

		if event["type"] == "step" && event["step"] == "skipped_step" {
			if event["skip_reason"] != "ENABLE_DEBUG=1 required" {
				t.Fatalf("step skip_reason = %v", event["skip_reason"])
			}

			foundStepReason = true
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !foundScenarioReason {
		t.Fatalf("scenario skip_reason not emitted")
	}

	if !foundStepReason {
		t.Fatalf("step skip_reason not emitted")
	}
}

func TestWriteJUnitEmitsSkippedElement(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "junit.xml")
	if err := WriteJUnit(path, newSkippedSuite()); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	type testcase struct {
		Name    string `xml:"name,attr"`
		Skipped *struct {
			Message string `xml:"message,attr"`
		} `xml:"skipped"`
	}

	type testsuite struct {
		XMLName   xml.Name   `xml:"testsuite"`
		Skipped   int        `xml:"skipped,attr"`
		TestCases []testcase `xml:"testcase"`
	}

	var ts testsuite
	if err := xml.Unmarshal(data, &ts); err != nil {
		t.Fatalf("unmarshal junit: %v", err)
	}

	if ts.Skipped != 1 {
		t.Fatalf("testsuite skipped attr = %d want 1", ts.Skipped)
	}

	var skipped *testcase

	for i := range ts.TestCases {
		if ts.TestCases[i].Name == "scenario skipped by rule" {
			skipped = &ts.TestCases[i]
		}
	}

	if skipped == nil || skipped.Skipped == nil {
		t.Fatalf("expected <skipped> element on skipped testcase")
	}

	if skipped.Skipped.Message != "requires macOS" {
		t.Fatalf("skipped message = %q", skipped.Skipped.Message)
	}
}
