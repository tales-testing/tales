package report

import (
	"encoding/json"
	"os"
)

// WriteJSONL writes compact newline-delimited events.
func WriteJSONL(path string, result *SuiteResult) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	encoder := json.NewEncoder(file)

	for _, scenario := range result.Scenarios {
		event := map[string]interface{}{
			"type":        "scenario",
			"status":      scenario.Status,
			"file":        scenario.File,
			"scenario":    scenario.Name,
			"duration_ms": scenario.Duration.Milliseconds(),
			"seed":        result.Seed,
		}
		if scenario.Failure != nil {
			event["error"] = scenario.Failure
		}
		if err := encoder.Encode(event); err != nil {
			return err
		}
		for _, step := range scenario.Steps {
			stepEvent := map[string]interface{}{
				"type":        "step",
				"phase":       "step",
				"status":      step.Status,
				"file":        step.File,
				"scenario":    step.Scenario,
				"step":        step.Name,
				"provider":    step.Provider,
				"duration_ms": step.Duration.Milliseconds(),
				"seed":        result.Seed,
			}
			if step.StatusCode > 0 {
				stepEvent["status_code"] = step.StatusCode
			}
			if step.Failure != nil {
				stepEvent["error"] = step.Failure
			}
			if err := encoder.Encode(stepEvent); err != nil {
				return err
			}
		}
		for _, step := range scenario.Teardown {
			stepEvent := map[string]interface{}{
				"type":        "step",
				"phase":       "teardown",
				"status":      step.Status,
				"file":        step.File,
				"scenario":    step.Scenario,
				"step":        step.Name,
				"provider":    step.Provider,
				"duration_ms": step.Duration.Milliseconds(),
				"seed":        result.Seed,
			}
			if step.StatusCode > 0 {
				stepEvent["status_code"] = step.StatusCode
			}
			if step.Failure != nil {
				stepEvent["error"] = step.Failure
			}
			if err := encoder.Encode(stepEvent); err != nil {
				return err
			}
		}
	}
	return nil
}
