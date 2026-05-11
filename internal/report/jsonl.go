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
		}

		for _, step := range scenario.Teardown {
			if err := encodeStepEvent(encoder, result.Seed, "teardown", step); err != nil {
				return err
			}
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

	if step.Attempts > 0 {
		stepEvent["attempts"] = step.Attempts
	}

	if step.Failure != nil {
		stepEvent["error"] = sanitizeErrorDetail(step.Failure)
	}

	if len(step.Request) > 0 {
		stepEvent["request"] = diagnostic.SanitizeMap(step.Request)
	}

	if len(step.Response) > 0 {
		stepEvent["response"] = diagnostic.SanitizeMap(step.Response)
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
