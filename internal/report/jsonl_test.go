package report

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWriteJSONLIncludesTeardownPhaseAndSkippedStatus(t *testing.T) {
	t.Parallel()
	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 1234,
		Scenarios: []*ScenarioResult{
			{
				File:     "e2e/pass/teardown_skip.tales",
				Name:     "Skipped teardown when capture is missing",
				Status:   StatusPass,
				Duration: 5 * time.Millisecond,
				Steps: []*StepResult{{
					File:     "e2e/pass/teardown_skip.tales",
					Scenario: "Skipped teardown when capture is missing",
					Name:     "health",
					Provider: "http",
					Status:   StatusPass,
				}},
				Teardown: []*StepResult{{
					File:     "e2e/pass/teardown_skip.tales",
					Scenario: "Skipped teardown when capture is missing",
					Name:     "delete_missing_user",
					Provider: "http",
					Status:   StatusSkip,
				}},
			},
		},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	h, err := os.Open(file)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer func() { _ = h.Close() }()

	scanner := bufio.NewScanner(h)
	found := false
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		if event["type"] == "step" && event["step"] == "delete_missing_user" {
			if event["phase"] != "teardown" {
				t.Fatalf("want teardown phase got %v", event["phase"])
			}
			if event["status"] != "skipped" {
				t.Fatalf("want skipped status got %v", event["status"])
			}
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !found {
		t.Fatalf("teardown skipped event not found")
	}
}

func TestWriteJSONLOmitsAttemptsForSingleAttemptSteps(t *testing.T) {
	t.Parallel()

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 1234,
		Scenarios: []*ScenarioResult{{
			File:   "test.tales",
			Name:   "single attempt",
			Status: StatusPass,
			Steps: []*StepResult{{
				File:     "test.tales",
				Scenario: "single attempt",
				Name:     "health",
				Provider: "http",
				Status:   StatusPass,
				Attempts: 1,
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	events := readJSONLEvents(t, file)
	for _, event := range events {
		if event["type"] != "step" {
			continue
		}
		if _, ok := event["attempts"]; ok {
			t.Fatalf("single-attempt step should not include attempts: %#v", event)
		}
	}
}

func TestWriteJSONLIncludesAttemptsForRetriedSteps(t *testing.T) {
	t.Parallel()

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 1234,
		Scenarios: []*ScenarioResult{{
			File:   "test.tales",
			Name:   "retry",
			Status: StatusPass,
			Steps: []*StepResult{{
				File:     "test.tales",
				Scenario: "retry",
				Name:     "eventual",
				Provider: "http",
				Status:   StatusPass,
				Attempts: 3,
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	events := readJSONLEvents(t, file)
	for _, event := range events {
		if event["type"] != "step" {
			continue
		}
		if event["attempts"] != float64(3) {
			t.Fatalf("retried step should include attempts=3: %#v", event)
		}

		return
	}

	t.Fatalf("step event not found")
}

func TestWriteJSONLHoistsMobileActionsAndPreservesUpstreamMask(t *testing.T) {
	t.Parallel()

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 1234,
		Scenarios: []*ScenarioResult{{
			File:   "ios.tales",
			Name:   "mobile",
			Status: StatusPass,
			Steps: []*StepResult{{
				File:     "ios.tales",
				Scenario: "mobile",
				Name:     "submit",
				Provider: "mobile",
				Status:   StatusPass,
				Request: map[string]interface{}{
					"actions": []any{
						map[string]any{"kind": "input_text", "id": "register.email", "value": "ios-user@example.com"},
						map[string]any{"kind": "input_text", "id": "register.password", "value": "***"},
						map[string]any{"kind": "input_text", "id": "register.token", "token": "Secret123"},
					},
				},
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	var step map[string]any

	for _, event := range readJSONLEvents(t, file) {
		if event["type"] == "step" && event["step"] == "submit" {
			step = event

			break
		}
	}

	if step == nil {
		t.Fatal("step event not found in jsonl")
	}

	actions, ok := step["actions"].([]any)
	if !ok {
		t.Fatalf("expected top-level actions array, got %T (%#v)", step["actions"], step["actions"])
	}

	if len(actions) != 3 {
		t.Fatalf("expected 3 hoisted actions, got %d (%#v)", len(actions), actions)
	}

	second, ok := actions[1].(map[string]any)
	if !ok {
		t.Fatalf("expected action entry to be an object, got %T", actions[1])
	}

	if second["value"] != "***" {
		t.Fatalf("expected upstream-masked password value preserved (***), got %#v", second["value"])
	}

	third, ok := actions[2].(map[string]any)
	if !ok {
		t.Fatalf("expected action entry to be an object, got %T", actions[2])
	}

	if third["token"] != "***" {
		t.Fatalf("expected SanitizeUnknown to mask the token field by JSON key, got %#v", third["token"])
	}

	for _, action := range actions {
		entry, ok := action.(map[string]any)
		if !ok {
			t.Fatalf("expected action entry object, got %T", action)
		}

		for _, raw := range entry {
			if raw == "Secret123" {
				t.Fatalf("raw secret survived report sanitization: %#v", entry)
			}
		}
	}
}

func readJSONLEvents(t *testing.T, path string) []map[string]interface{} {
	t.Helper()

	handle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer func() { _ = handle.Close() }()

	var events []map[string]interface{}
	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	return events
}
