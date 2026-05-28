package lang

import (
	"fmt"
	"time"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

const (
	paramMin = "min"
	paramMax = "max"
)

// thresholdFunc returns a matcher factory for the numeric comparators
// lt / lte / gt / gte. The single argument can be a number or a Go
// duration string ("100ms", "2.5s", "1m"); invalid arguments fail at
// HCL evaluation time rather than at match time.
func thresholdFunc(name string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if err := validateThresholdArg(name, args[0]); err != nil {
				return cty.NilVal, err
			}

			return matcherObject(name, map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

// betweenFunc returns the factory for between(min, max). Both bounds
// must be numeric or both must be parseable durations. min <= max is
// validated when both bounds are concrete.
func betweenFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: paramMin, Type: cty.DynamicPseudoType},
			{Name: paramMax, Type: cty.DynamicPseudoType},
		},
		Type: function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if err := validateThresholdArg("between", args[0]); err != nil {
				return cty.NilVal, err
			}

			if err := validateThresholdArg("between", args[1]); err != nil {
				return cty.NilVal, err
			}

			lo, loOK := thresholdAsFloat(args[0])
			hi, hiOK := thresholdAsFloat(args[1])

			if loOK && hiOK && lo > hi {
				return cty.NilVal, fmt.Errorf("between min must be <= max, got min=%v max=%v", args[0].GoString(), args[1].GoString())
			}

			return matcherObject("between", map[string]cty.Value{
				paramMin: args[0],
				paramMax: args[1],
			}), nil
		},
	})
}

func validateThresholdArg(name string, v cty.Value) error {
	if v.IsNull() {
		return fmt.Errorf("%s threshold must be number or duration, got null", name)
	}

	switch v.Type() {
	case cty.Number:
		return nil
	case cty.String:
		raw := v.AsString()
		if _, err := time.ParseDuration(raw); err != nil {
			return fmt.Errorf("%s threshold must be number or duration, got %q", name, raw)
		}

		return nil
	default:
		return fmt.Errorf("%s threshold must be number or duration, got %s", name, v.Type().FriendlyName())
	}
}

// thresholdAsFloat converts a numeric or duration-string threshold to a
// float64. Durations are returned in milliseconds. ok=false when the
// value is not a recognized threshold form.
func thresholdAsFloat(v cty.Value) (float64, bool) {
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
