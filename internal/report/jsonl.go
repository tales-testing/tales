package report

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hyperxlab/tales/internal/diagnostic"
)

// WriteJSONL writes compact newline-delimited events.
func WriteJSONL(path string, result *SuiteResult) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create jsonl file %q: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	for _, scenario := range result.Scenarios {
		if err := encodeScenarioEvent(encoder, result.Seed, scenario); err != nil {
			return err
		}

		for _, step := range scenario.Steps {
			if err := encodeStepEvent(encoder, result.Seed, "step", step); err != nil {
				return err
			}

			if err := encodeActionEvents(encoder, result.Seed, "step", step); err != nil {
				return err
			}
		}

		for _, step := range scenario.Teardown {
			if err := encodeStepEvent(encoder, result.Seed, "teardown", step); err != nil {
				return err
			}

			if err := encodeActionEvents(encoder, result.Seed, "teardown", step); err != nil {
				return err
			}
		}
	}

	return nil
}

// encodeActionEvents emits one "action" event per UI action attached to a
// step. It is a no-op when the step has no Actions, so output stays
// byte-identical to the pre-visual-report format for non-mobile suites and
// for mobile suites running in --capture-screenshots failures (where
// Actions are still recorded as a typed slice for the HTML report).
//
// Secure values are not re-masked here: the slice carries "***" already.
func encodeActionEvents(encoder *json.Encoder, seed int64, phase string, step *StepResult) error {
	for _, action := range step.Actions {
		if action == nil {
			continue
		}

		event := map[string]interface{}{
			"type":        "action",
			"phase":       phase,
			"scenario":    step.Scenario,
			"step":        step.Name,
			"provider":    step.Provider,
			"index":       action.Index,
			"kind":        action.Kind,
			"label":       action.Label,
			"status":      action.Status,
			"duration_ms": action.Duration.Milliseconds(),
			"seed":        seed,
		}

		if action.SelectorID != "" {
			event["selector_id"] = action.SelectorID
		}

		if action.Secure {
			event["secure"] = true
		}

		if action.Value != "" {
			event["value"] = action.Value
		}

		if action.Screenshot != "" {
			event["screenshot"] = action.Screenshot
		}

		if action.Hierarchy != "" {
			event["hierarchy"] = action.Hierarchy
		}

		if action.Error != nil {
			event["error"] = sanitizeErrorDetail(action.Error)
		}

		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("encode action event %s/%d: %w", step.Name, action.Index, err)
		}
	}

	return nil
}

func encodeScenarioEvent(encoder *json.Encoder, seed int64, scenario *ScenarioResult) error {
	event := map[string]interface{}{
		"type":        "scenario",
		"status":      scenario.Status,
		"file":        scenario.File,
		"scenario":    scenario.Name,
		"duration_ms": scenario.Duration.Milliseconds(),
		"seed":        seed,
	}
	if scenario.Failure != nil {
		event["error"] = sanitizeErrorDetail(scenario.Failure)
	}

	if err := encoder.Encode(event); err != nil {
		return fmt.Errorf("encode scenario event %q: %w", scenario.Name, err)
	}

	return nil
}

func encodeStepEvent(encoder *json.Encoder, seed int64, phase string, step *StepResult) error {
	stepEvent := map[string]interface{}{
		"type":        "step",
		"phase":       phase,
		"status":      step.Status,
		"file":        step.File,
		"scenario":    step.Scenario,
		"step":        step.Name,
		"provider":    step.Provider,
		"duration_ms": step.Duration.Milliseconds(),
		"seed":        seed,
	}
	if step.StatusCode > 0 {
		stepEvent["status_code"] = step.StatusCode
	}

	if step.Attempts > 1 {
		stepEvent["attempts"] = step.Attempts
	}

	if step.Failure != nil {
		stepEvent["error"] = sanitizeErrorDetail(step.Failure)
	}

	if len(step.Request) > 0 {
		stepEvent["request"] = diagnostic.SanitizeMap(step.Request)
		if actions, ok := step.Request["actions"]; ok {
			stepEvent["actions"] = diagnostic.SanitizeUnknown(actions)
		}
	}

	if len(step.Response) > 0 {
		stepEvent["response"] = diagnostic.SanitizeMap(step.Response)
	}

	if len(step.Artifacts) > 0 {
		stepEvent["artifacts"] = step.Artifacts
	}

	if err := encoder.Encode(stepEvent); err != nil {
		return fmt.Errorf("encode %s step event %q: %w", phase, step.Name, err)
	}

	return nil
}

func sanitizeErrorDetail(detail *ErrorDetail) map[string]interface{} {
	if detail == nil {
		return nil
	}

	return map[string]interface{}{
		"kind":    detail.Kind,
		"path":    detail.Path,
		"want":    diagnostic.Normalize(detail.Want),
		"got":     diagnostic.Normalize(detail.Got),
		"message": detail.Message,
	}
}
