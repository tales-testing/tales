package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func suiteWithTeardown(status Status) *SuiteResult {
	result := &SuiteResult{
		Seed: 7,
		Scenarios: []*ScenarioResult{{
			File:   "e2e/pass/blog.tales",
			Name:   "Blog flow",
			Status: StatusPass,
			Steps:  []*StepResult{{Name: "create", Provider: "http", Phase: "step", Status: StatusPass}},
		}},
		Teardown: []*StepResult{{
			File:     "e2e/pass/_suite.tales",
			Scenario: "tales:suite-teardown",
			Name:     "purge",
			Provider: "sql",
			Phase:    suiteTeardownLabel,
			Status:   status,
		}},
	}

	if status == StatusFail {
		failure := &ErrorDetail{Kind: "provider", Message: "purge failed"}
		result.Teardown[0].Failure = failure
		result.TeardownFailures = []*ErrorDetail{failure}
	}

	return result
}

func TestConsolePrintsSuiteTeardown(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if err := PrintConsoleWithOptions(&buf, suiteWithTeardown(StatusPass), ConsoleOptions{}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Suite teardown (e2e/pass/_suite.tales)") {
		t.Fatalf("console output missing the suite teardown header:\n%s", out)
	}

	if !strings.Contains(out, "teardown purge") {
		t.Fatalf("console output missing the suite teardown step:\n%s", out)
	}
}

// Suites without a teardown block must render exactly as before.
func TestConsoleOmitsSuiteTeardownWhenAbsent(t *testing.T) {
	t.Parallel()

	result := suiteWithTeardown(StatusPass)
	result.Teardown = nil

	var buf bytes.Buffer

	if err := PrintConsoleWithOptions(&buf, result, ConsoleOptions{}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	if strings.Contains(buf.String(), "Suite teardown") {
		t.Fatalf("console printed a suite teardown section for a suite without one:\n%s", buf.String())
	}
}

// A cleanup that did not complete leaves the environment dirty for the next
// run, so it must fail the command even when every scenario passed.
func TestFailedReportsSuiteTeardownFailure(t *testing.T) {
	t.Parallel()

	if !suiteWithTeardown(StatusFail).Failed() {
		t.Fatal("a failing suite teardown must fail the suite")
	}

	if suiteWithTeardown(StatusPass).Failed() {
		t.Fatal("a passing suite teardown must not fail the suite")
	}
}

func TestJSONLEmitsSuiteTeardownLast(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.jsonl")
	if err := WriteJSONL(path, suiteWithTeardown(StatusPass)); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")

	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("decode last event: %v", err)
	}

	if last[jsonlKeyPhase] != suiteTeardownLabel {
		t.Fatalf("last event phase = %v, want %q", last[jsonlKeyPhase], suiteTeardownLabel)
	}

	if last[jsonlKeyStep] != "purge" {
		t.Fatalf("last event step = %v, want purge", last[jsonlKeyStep])
	}
}

// Without a synthetic testcase, a run whose only failure is the cleanup would
// produce a fully green JUnit file next to a non-zero exit code.
func TestJUnitReportsSuiteTeardownFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.xml")
	if err := WriteJUnit(path, suiteWithTeardown(StatusFail)); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	out := string(raw)
	if !strings.Contains(out, `failures="1"`) {
		t.Fatalf("junit should report the teardown failure:\n%s", out)
	}

	if !strings.Contains(out, `name="`+suiteTeardownLabel+`"`) {
		t.Fatalf("junit missing the suite teardown testcase:\n%s", out)
	}

	if !strings.Contains(out, "purge failed") {
		t.Fatalf("junit missing the teardown failure message:\n%s", out)
	}
}

func TestJUnitOmitsSuiteTeardownCaseWhenAbsent(t *testing.T) {
	t.Parallel()

	result := suiteWithTeardown(StatusPass)
	result.Teardown = nil

	path := filepath.Join(t.TempDir(), "report.xml")
	if err := WriteJUnit(path, result); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	if strings.Contains(string(raw), suiteTeardownLabel) {
		t.Fatalf("junit emitted a suite teardown case for a suite without one:\n%s", raw)
	}
}
