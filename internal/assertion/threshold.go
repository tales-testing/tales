package assertion

import (
	"fmt"
	"time"

	"github.com/zclconf/go-cty/cty"
)

const msgValueNotNumber = "value is not a number"

// thresholdValue converts a number or duration-string cty.Value into a
// canonical float64. Durations are normalised to milliseconds so that
// numeric metrics in _ms fields compare directly against duration
// thresholds. ok=false when the value is not a recognized numeric or
// duration form.
func thresholdValue(v cty.Value) (float64, bool) {
	if v.IsNull() {
		return 0, false
	}

	switch v.Type() {
	case cty.Number:
		f, _ := v.AsBigFloat().Float64()

		return f, true
	case cty.String:
		d, err := time.ParseDuration(v.AsString())
		if err != nil {
			return 0, false
		}

		return float64(d) / float64(time.Millisecond), true
	}

	return 0, false
}

// formatThreshold returns a human-readable rendering of a threshold for
// error messages (preserves the original duration string when given).
func formatThreshold(v cty.Value) string {
	if v.IsNull() {
		return "null"
	}

	switch v.Type() {
	case cty.Number:
		f, _ := v.AsBigFloat().Float64()

		return formatFloat(f)
	case cty.String:
		return v.AsString()
	}

	return v.GoString()
}

func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}

	return fmt.Sprintf("%g", f)
}

func comparisonError(path, op string, actual cty.Value, threshold cty.Value) error {
	actualFloat, ok := thresholdValue(actual)
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueNotNumber}
	}

	return &Mismatch{
		Kind:    kindAssertion,
		Path:    path,
		Message: fmt.Sprintf("value must be %s %s, got %s", op, formatThreshold(threshold), formatFloat(actualFloat)),
	}
}

func matchLt(args map[string]cty.Value, actual cty.Value, path string) error {
	return matchThreshold(args, actual, path, "lt", "<", func(a, t float64) bool { return a < t })
}

func matchLte(args map[string]cty.Value, actual cty.Value, path string) error {
	return matchThreshold(args, actual, path, "lte", "<=", func(a, t float64) bool { return a <= t })
}

func matchGt(args map[string]cty.Value, actual cty.Value, path string) error {
	return matchThreshold(args, actual, path, "gt", ">", func(a, t float64) bool { return a > t })
}

func matchGte(args map[string]cty.Value, actual cty.Value, path string) error {
	return matchThreshold(args, actual, path, "gte", ">=", func(a, t float64) bool { return a >= t })
}

func matchThreshold(
	args map[string]cty.Value,
	actual cty.Value,
	path, name, op string,
	cmp func(actual, threshold float64) bool,
) error {
	thresholdVal, ok := args["value"]
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: fmt.Sprintf("%s matcher is missing value", name)}
	}

	threshold, ok := thresholdValue(thresholdVal)
	if !ok {
		return &Mismatch{
			Kind:    kindAssertion,
			Path:    path,
			Message: fmt.Sprintf("%s threshold must be number or duration, got %s", name, formatThreshold(thresholdVal)),
		}
	}

	actualFloat, ok := thresholdValue(actual)
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueNotNumber}
	}

	if cmp(actualFloat, threshold) {
		return nil
	}

	return comparisonError(path, op, actual, thresholdVal)
}

func matchBetween(args map[string]cty.Value, actual cty.Value, path string) error {
	minVal, hasMin := args["min"]
	maxVal, hasMax := args["max"]

	if !hasMin || !hasMax {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "between matcher requires min and max"}
	}

	lo, loOK := thresholdValue(minVal)
	if !loOK {
		return &Mismatch{
			Kind:    kindAssertion,
			Path:    path,
			Message: fmt.Sprintf("between min must be number or duration, got %s", formatThreshold(minVal)),
		}
	}

	hi, hiOK := thresholdValue(maxVal)
	if !hiOK {
		return &Mismatch{
			Kind:    kindAssertion,
			Path:    path,
			Message: fmt.Sprintf("between max must be number or duration, got %s", formatThreshold(maxVal)),
		}
	}

	if lo > hi {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: "between min must be <= max"}
	}

	actualFloat, ok := thresholdValue(actual)
	if !ok {
		return &Mismatch{Kind: kindAssertion, Path: path, Message: msgValueNotNumber}
	}

	if actualFloat >= lo && actualFloat <= hi {
		return nil
	}

	return &Mismatch{
		Kind:    kindAssertion,
		Path:    path,
		Message: fmt.Sprintf("value must be between %s and %s, got %s", formatThreshold(minVal), formatThreshold(maxVal), formatFloat(actualFloat)),
	}
}
