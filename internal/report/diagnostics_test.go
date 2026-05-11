package report

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func TestConsoleFailureOutputNoCTYAndMaskedSecrets(t *testing.T) {
	t.Parallel()

	result := &SuiteResult{
		Seed:      1234,
		Duration:  10 * time.Millisecond,
		Scenarios: []*ScenarioResult{sampleFailedScenario()},
	}

	buffer := bytes.Buffer{}

	if err := PrintConsoleWithOptions(&buffer, result, ConsoleOptions{Color: false, Progress: false}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	output := buffer.String()

	if strings.Contains(output, "cty.") {
		t.Fatalf("console output should not contain cty internals: %s", output)
	}

	if strings.Contains(output, "Bearer REAL_TOKEN") {
		t.Fatalf("authorization should be masked: %s", output)
	}

	if strings.Contains(output, "Passw0rd!") {
		t.Fatalf("password should be masked: %s", output)
	}

	if strings.Contains(output, "secret-access-token") {
		t.Fatalf("access token should be masked: %s", output)
	}

	if !strings.Contains(output, "***") {
		t.Fatalf("masked token marker should be present: %s", output)
	}
}

func TestJSONLFailureOutputMasksSensitiveFields(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed:      1234,
		Scenarios: []*ScenarioResult{sampleFailedScenario()},
	}

	if err := WriteJSONL(path, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	handle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer func() { _ = handle.Close() }()

	scanner := bufio.NewScanner(handle)

	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}

		if event["type"] != "step" || event["status"] != "fail" {
			continue
		}

		request := event["request"].(map[string]interface{})
		headers := request["headers"].(map[string]interface{})
		if headers["Authorization"] != "***" {
			t.Fatalf("authorization must be masked: %#v", headers)
		}

		requestJSON := request["json"].(map[string]interface{})
		if requestJSON["password"] != "***" {
			t.Fatalf("password must be masked: %#v", requestJSON)
		}

		response := event["response"].(map[string]interface{})
		responseJSON := response["json"].(map[string]interface{})
		if responseJSON["access_token"] != "***" {
			t.Fatalf("access_token must be masked: %#v", responseJSON)
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan jsonl: %v", err)
	}
}

func TestJUnitFailureOutputMasksSensitiveFields(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/report.xml"
	result := &SuiteResult{
		Seed:      1234,
		Scenarios: []*ScenarioResult{sampleFailedScenario()},
	}

	if err := WriteJUnit(path, result); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	text := string(content)
	if strings.Contains(text, "Bearer REAL_TOKEN") {
		t.Fatalf("authorization should be masked in junit: %s", text)
	}

	if strings.Contains(text, "Passw0rd!") {
		t.Fatalf("password should be masked in junit: %s", text)
	}

	if strings.Contains(text, "secret-access-token") {
		t.Fatalf("access token should be masked in junit: %s", text)
	}

	if !strings.Contains(text, "***") {
		t.Fatalf("masked marker should be present in junit: %s", text)
	}
}

func TestConsoleFailureOutputMasksBasicAuth(t *testing.T) {
	t.Parallel()

	result := &SuiteResult{
		Seed:      1234,
		Duration:  10 * time.Millisecond,
		Scenarios: []*ScenarioResult{sampleBasicAuthFailedScenario()},
	}

	buffer := bytes.Buffer{}
	if err := PrintConsoleWithOptions(&buffer, result, ConsoleOptions{Color: false, Progress: false}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	assertNoBasicAuthLeak(t, buffer.String())
	if !strings.Contains(buffer.String(), "Authorization: Basic ***") {
		t.Fatalf("expected masked basic authorization, got: %s", buffer.String())
	}
}

func TestJSONLFailureOutputMasksBasicAuth(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed:      1234,
		Scenarios: []*ScenarioResult{sampleBasicAuthFailedScenario()},
	}

	if err := WriteJSONL(path, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}

	assertNoBasicAuthLeak(t, string(content))
	if !strings.Contains(string(content), `"Authorization":"Basic ***"`) {
		t.Fatalf("expected masked basic authorization, got: %s", string(content))
	}
}

func TestJUnitFailureOutputMasksBasicAuth(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/report.xml"
	result := &SuiteResult{
		Seed:      1234,
		Scenarios: []*ScenarioResult{sampleBasicAuthFailedScenario()},
	}

	if err := WriteJUnit(path, result); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	assertNoBasicAuthLeak(t, string(content))
	if !strings.Contains(string(content), "Authorization: Basic ***") {
		t.Fatalf("expected masked basic authorization, got: %s", string(content))
	}
}

func TestConsoleFailurePrefixUsesKindAndScenarioFailureIsPrinted(t *testing.T) {
	t.Parallel()

	result := &SuiteResult{
		Seed:     1234,
		Duration: 10 * time.Millisecond,
		Scenarios: []*ScenarioResult{{
			File:     "e2e/fail/example.tales",
			Name:     "DAG failure",
			Status:   StatusFail,
			Duration: time.Millisecond,
			Failure: &ErrorDetail{
				Kind:    "dag",
				Path:    "topology",
				Message: "dependency cycle detected",
			},
		}},
	}

	buffer := bytes.Buffer{}

	if err := PrintConsoleWithOptions(&buffer, result, ConsoleOptions{Color: false, Progress: false}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "dag failed at topology") {
		t.Fatalf("expected kind-based failure prefix, got: %s", output)
	}

	if !strings.Contains(output, "dependency cycle detected") {
		t.Fatalf("expected scenario failure message, got: %s", output)
	}
}

func TestConsoleProgressLine(t *testing.T) {
	t.Parallel()

	scenario := sampleFailedScenario()
	scenario.Status = StatusPass
	scenario.Failure = nil
	scenario.Steps[0].Status = StatusPass
	scenario.Steps[0].Failure = nil

	result := &SuiteResult{
		Seed:      1234,
		Duration:  10 * time.Millisecond,
		Scenarios: []*ScenarioResult{scenario},
	}

	buffer := bytes.Buffer{}
	if err := PrintConsoleWithOptions(&buffer, result, ConsoleOptions{Color: false, Progress: true}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "[ scenario: 1/1, step: 1/1, skip: 0, success: 1, failure: 0 ]") {
		t.Fatalf("progress line not found: %s", output)
	}
}

func TestJUnitFailureMessageUsesKindForNonAssertion(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/report.xml"
	result := &SuiteResult{
		Seed:      1234,
		Scenarios: []*ScenarioResult{sampleCaptureFailedScenario()},
	}

	if err := WriteJUnit(path, result); err != nil {
		t.Fatalf("write junit: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, `message="capture failed at token"`) {
		t.Fatalf("expected capture-based junit message, got: %s", text)
	}
}

func sampleFailedScenario() *ScenarioResult {
	step := &StepResult{
		File:       "e2e/fail/teardown_failure.tales",
		Scenario:   "Teardown runs after failure",
		Name:       "intentional_failure",
		Provider:   "http",
		Phase:      "step",
		Status:     StatusFail,
		Duration:   time.Millisecond,
		StatusCode: 404,
		Request: map[string]interface{}{
			"method": "POST",
			"url":    "http://localhost:1337/auth",
			"headers": map[string]interface{}{
				"Accept":        "application/json",
				"Authorization": "Bearer REAL_TOKEN",
			},
			"json": map[string]interface{}{
				"email":    "user@example.com",
				"password": "Passw0rd!",
			},
		},
		Response: map[string]interface{}{
			"status": 404,
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
			"json": map[string]interface{}{
				"access_token": "secret-access-token",
				"error":        "not found",
			},
			"body": `{"access_token":"secret-access-token","error":"not found"}`,
		},
		Failure: &ErrorDetail{
			Kind:    "assertion",
			Path:    "status",
			Want:    cty.NumberIntVal(200),
			Got:     cty.NumberIntVal(404),
			Message: "assertion failed at status",
		},
	}

	return &ScenarioResult{
		File:     "e2e/fail/teardown_failure.tales",
		Name:     "Teardown runs after failure",
		Status:   StatusFail,
		Duration: 2 * time.Millisecond,
		Steps:    []*StepResult{step},
		Failure:  step.Failure,
	}
}

func sampleBasicAuthFailedScenario() *ScenarioResult {
	rawAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:wrong-secret"))
	step := &StepResult{
		File:       "e2e/fail/basic_auth_failure.tales",
		Scenario:   "HTTP basic auth failure",
		Name:       "basic_auth_invalid",
		Provider:   "http",
		Phase:      "step",
		Status:     StatusFail,
		Duration:   time.Millisecond,
		StatusCode: 401,
		Request: map[string]interface{}{
			"method": "GET",
			"url":    "http://localhost:1337/basic-auth",
			"headers": map[string]interface{}{
				"Authorization": rawAuthorization,
			},
			"json": map[string]interface{}{
				"password": "wrong-secret",
			},
		},
		Response: map[string]interface{}{
			"status": 401,
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
			"body": `{"error":"unauthorized"}`,
		},
		Failure: &ErrorDetail{
			Kind:    "assertion",
			Path:    "status",
			Want:    cty.NumberIntVal(200),
			Got:     cty.NumberIntVal(401),
			Message: "assertion failed at status",
		},
	}

	return &ScenarioResult{
		File:     "e2e/fail/basic_auth_failure.tales",
		Name:     "HTTP basic auth failure",
		Status:   StatusFail,
		Duration: 2 * time.Millisecond,
		Steps:    []*StepResult{step},
		Failure:  step.Failure,
	}
}

func assertNoBasicAuthLeak(t *testing.T, output string) {
	t.Helper()

	for _, forbidden := range []string{
		"wrong-secret",
		"admin:wrong-secret",
		base64.StdEncoding.EncodeToString([]byte("admin:wrong-secret")),
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("basic auth secret leaked (%q): %s", forbidden, output)
		}
	}
}

func sampleCaptureFailedScenario() *ScenarioResult {
	step := &StepResult{
		File:       "e2e/fail/capture_failure.tales",
		Scenario:   "Capture failure",
		Name:       "capture_token",
		Provider:   "http",
		Phase:      "step",
		Status:     StatusFail,
		Duration:   time.Millisecond,
		StatusCode: 200,
		Failure: &ErrorDetail{
			Kind: "capture",
			Path: "token",
		},
	}

	return &ScenarioResult{
		File:     "e2e/fail/capture_failure.tales",
		Name:     "Capture failure",
		Status:   StatusFail,
		Duration: 2 * time.Millisecond,
		Steps:    []*StepResult{step},
		Failure:  step.Failure,
	}
}
