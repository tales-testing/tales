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

func TestWriteJSONLEmitsActionEventsAfterStep(t *testing.T) {
	t.Parallel()

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 9876,
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
				Actions: []*ActionResult{
					{
						Index:      0,
						Kind:       "tap",
						Label:      "Tap welcome.register",
						SelectorID: "welcome.register",
						Status:     StatusPass,
						Duration:   120 * time.Millisecond,
						Screenshot: "build/artifacts/mobile/x/submit/step/attempt-1/actions/0000-tap-welcome.register/screenshot.png",
					},
					{
						Index:      1,
						Kind:       "input_text",
						Label:      "Input text register.password ***",
						SelectorID: "register.password",
						Secure:     true,
						Value:      "***",
						Status:     StatusPass,
						Duration:   200 * time.Millisecond,
					},
				},
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	events := readJSONLEvents(t, file)

	// Find step event and the two action events in order.
	var stepIdx int = -1
	var actionEvents []map[string]any

	for i, e := range events {
		if e["type"] == "step" && e["step"] == "submit" {
			stepIdx = i
		}

		if e["type"] == "action" && e["step"] == "submit" {
			actionEvents = append(actionEvents, e)
		}
	}

	if stepIdx == -1 {
		t.Fatal("step event missing")
	}

	if len(actionEvents) != 2 {
		t.Fatalf("expected 2 action events, got %d", len(actionEvents))
	}

	// Action events must come AFTER the step event.
	for i, e := range events {
		if i <= stepIdx {
			continue
		}

		if e["type"] == "action" {
			break
		}
	}

	first := actionEvents[0]
	if first["index"].(float64) != 0 || first["kind"] != "tap" {
		t.Errorf("first action event payload unexpected: %+v", first)
	}

	if first["screenshot"] == nil || first["screenshot"] == "" {
		t.Errorf("expected first action to carry screenshot path, got %v", first["screenshot"])
	}

	second := actionEvents[1]
	if second["secure"] != true {
		t.Errorf("expected secure=true on masked action, got %v", second["secure"])
	}

	if second["value"] != "***" {
		t.Errorf("expected masked value ***, got %v", second["value"])
	}

	// Ensure raw secret never leaks anywhere in the file.
	for _, event := range events {
		for _, v := range event {
			if v == "hunter2" {
				t.Fatalf("raw secret leaked into JSONL output: %#v", event)
			}
		}
	}
}

func TestWriteJSONLEmitsNoActionEventsWhenSliceEmpty(t *testing.T) {
	t.Parallel()

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Seed: 1,
		Scenarios: []*ScenarioResult{{
			File: "x.tales", Name: "x", Status: StatusPass,
			Steps: []*StepResult{{
				File: "x.tales", Scenario: "x", Name: "s", Provider: "http", Status: StatusPass,
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	for _, e := range readJSONLEvents(t, file) {
		if e["type"] == "action" {
			t.Fatalf("expected no action events when Actions slice is empty, got %+v", e)
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

func TestWriteJSONLActionEventCarriesCaptureNote(t *testing.T) {
	t.Parallel()

	const note = "screenshot skipped: the page kept navigating while it was captured"

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Scenarios: []*ScenarioResult{{
			File:   "web.tales",
			Name:   "web",
			Status: StatusPass,
			Steps: []*StepResult{{
				File:     "web.tales",
				Scenario: "web",
				Name:     "bounce",
				Provider: "browser",
				Status:   StatusPass,
				Actions: []*ActionResult{
					{Index: 0, Kind: "goto", Label: "goto /bounce", Status: StatusPass, CaptureNote: note},
					{Index: 1, Kind: "wait_visible", Label: "wait_visible #email", Status: StatusPass},
				},
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	var actions []map[string]any

	for _, e := range readJSONLEvents(t, file) {
		if e["type"] == "action" {
			actions = append(actions, e)
		}
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 action events, got %d", len(actions))
	}

	if actions[0]["capture_note"] != note {
		t.Errorf("capture_note = %v, want %q", actions[0]["capture_note"], note)
	}

	if _, ok := actions[1]["capture_note"]; ok {
		t.Errorf("an action with a complete capture must not carry capture_note: %v", actions[1])
	}
}

// TestWriteJSONLActionEventCarriesTimingAndPoint pins the action timeline of
// issue #83: an action event says when it started and ended, in the layout
// of the browser's console.jsonl / network.jsonl, and a pointer action says
// where it pointed.
func TestWriteJSONLActionEventCarriesTimingAndPoint(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 10, 21, 11, 54, 34, 120_000_000, time.FixedZone("CEST", 2*60*60))

	file := t.TempDir() + "/events.jsonl"
	result := &SuiteResult{
		Scenarios: []*ScenarioResult{{
			File:   "web.tales",
			Name:   "web",
			Status: StatusFail,
			Steps: []*StepResult{{
				File:     "web.tales",
				Scenario: "web",
				Name:     "submit",
				Provider: "browser",
				Status:   StatusFail,
				Actions: []*ActionResult{
					{Index: 0, Kind: "goto", Label: "goto /form", Status: StatusPass, StartedAt: started, Duration: 18 * time.Millisecond},
					{Index: 1, Kind: "click", Label: "click #submit", Status: StatusFail, StartedAt: started.Add(20 * time.Millisecond), Duration: 1002 * time.Millisecond, Point: &ActionPoint{X: 37.5, Y: 90}},
					{Index: 2, Kind: "wait_visible", Label: "wait_visible #done", Status: StatusSkip},
				},
			}},
		}},
	}

	if err := WriteJSONL(file, result); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	var actions []map[string]any

	for _, e := range readJSONLEvents(t, file) {
		if e["type"] == "action" {
			actions = append(actions, e)
		}
	}

	if len(actions) != 3 {
		t.Fatalf("expected 3 action events, got %d", len(actions))
	}

	goTo, click, skipped := actions[0], actions[1], actions[2]

	if goTo["started_at"] != "2026-10-21T09:54:34.120Z" || goTo["ended_at"] != "2026-10-21T09:54:34.138Z" {
		t.Errorf("goto timing = %v .. %v", goTo["started_at"], goTo["ended_at"])
	}

	if _, ok := goTo["point"]; ok {
		t.Errorf("goto is not a pointer action, got point %v", goTo["point"])
	}

	if click["started_at"] != "2026-10-21T09:54:34.140Z" || click["ended_at"] != "2026-10-21T09:54:35.142Z" {
		t.Errorf("click timing = %v .. %v", click["started_at"], click["ended_at"])
	}

	point, ok := click["point"].(map[string]any)
	if !ok || point["x"] != 37.5 || point["y"] != float64(90) {
		t.Errorf("click point = %v, want {x: 37.5, y: 90}", click["point"])
	}

	for _, key := range []string{"started_at", "ended_at", "point"} {
		if _, ok := skipped[key]; ok {
			t.Errorf("a skipped action never ran and must not carry %s: %v", key, skipped)
		}
	}
}
