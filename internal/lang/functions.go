package lang

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

const (
	totpOptionPeriod    = "period"
	totpOptionDigits    = "digits"
	totpOptionAlgorithm = "algorithm"
	totpOptionTimestamp = "timestamp"
	paramSecret         = "secret"
	paramMessage        = "message"
)

const (
	matcherKey   = "__tales_matcher"
	paramName    = "name"
	paramValue   = "value"
	paramPattern = "pattern"
)

func matcherObject(name string, values map[string]cty.Value) cty.Value {
	payload := map[string]cty.Value{matcherKey: cty.StringVal(name)}
	for k, v := range values {
		payload[k] = v
	}

	return cty.ObjectVal(payload)
}

func envFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramName, Type: cty.String}},
		VarParam: &function.Parameter{
			Name: "default",
			Type: cty.String,
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			name := args[0].AsString()
			if value, ok := os.LookupEnv(name); ok {
				return cty.StringVal(value), nil
			}

			if len(args) > 1 {
				return cty.StringVal(args[1].AsString()), nil
			}

			return cty.StringVal(""), nil
		},
	})
}

func matcherNoArg(name string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject(name, map[string]cty.Value{}), nil
		},
	})
}

func matcherSingleArg(name string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject(name, map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

func optionalFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType, AllowNull: true}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("optional", map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

func requiredFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType, AllowNull: true}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("required", map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

func matchesFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramPattern, Type: cty.String}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			pattern := args[0].AsString()
			if _, err := regexp.Compile(pattern); err != nil {
				return cty.NilVal, fmt.Errorf("invalid regex %q: %w", pattern, err)
			}

			return matcherObject("matches", map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

func oneOfFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "values", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("one_of", map[string]cty.Value{paramValue: args[0]}), nil
		},
	})
}

func regexFindFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: paramValue, Type: cty.DynamicPseudoType},
			{Name: paramPattern, Type: cty.String},
		},
		VarParam: &function.Parameter{
			Name: "group",
			Type: cty.Number,
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if len(args) > 3 {
				return cty.NilVal, fmt.Errorf("regex_find accepts at most one capture group index")
			}

			groupIndex := 0

			if len(args) > 2 {
				parsedGroup, err := ctyNumberToInt(args[2])
				if err != nil {
					return cty.NilVal, err
				}

				groupIndex = parsedGroup
			}

			pattern := args[1].AsString()

			re, err := regexp.Compile(pattern)
			if err != nil {
				return cty.NilVal, fmt.Errorf("regex_find pattern is invalid: %w", err)
			}

			value := diagnostic.ScalarString(args[0])

			matches := re.FindStringSubmatch(value)
			if matches == nil {
				return cty.NilVal, fmt.Errorf("regex_find found no match")
			}

			if groupIndex < 0 || groupIndex >= len(matches) {
				return cty.NilVal, fmt.Errorf("regex_find group %d is out of range", groupIndex)
			}

			return cty.StringVal(matches[groupIndex]), nil
		},
	})
}

func urlEncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.StringVal(url.QueryEscape(diagnostic.ScalarString(args[0]))), nil
		},
	})
}

// base64urlEncodeFunc encodes the UTF-8 bytes of the input string using the
// RFC 4648 URL-safe alphabet without padding. This deliberately encodes the
// string itself, not a hex digest — for PKCE S256, use pkce_challenge instead
// of composing this with sha256_hex, since PKCE encodes the raw 32 hash
// bytes, not the 64-char hex string.
func base64urlEncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.StringVal(base64.RawURLEncoding.EncodeToString([]byte(args[0].AsString()))), nil
		},
	})
}

// jsonEncodeFunc serializes a cty.Value to a canonical JSON string with
// alphabetically-sorted object keys. The deterministic output makes it safe
// to use as the input of an HMAC or any other signature scheme.
func jsonEncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: paramValue, Type: cty.DynamicPseudoType, AllowNull: true}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			canonical, err := ctyToCanonical(args[0])
			if err != nil {
				return cty.NilVal, fmt.Errorf("jsonencode: %w", err)
			}

			out, err := json.Marshal(canonical)
			if err != nil {
				return cty.NilVal, fmt.Errorf("jsonencode: %w", err)
			}

			return cty.StringVal(string(out)), nil
		},
	})
}

// ctyToCanonical converts a cty.Value into a Go value that json.Marshal
// emits deterministically: object keys are sorted (encoding/json sorts
// map[string]any keys alphabetically), sets are sorted by encoded form,
// and numbers preserve precision via json.Number.
func ctyToCanonical(v cty.Value) (any, error) {
	if v.IsNull() {
		return nil, nil
	}

	if !v.IsKnown() {
		return nil, fmt.Errorf("cannot encode unknown value")
	}

	ty := v.Type()
	switch {
	case ty == cty.String:
		return v.AsString(), nil
	case ty == cty.Bool:
		return v.True(), nil
	case ty == cty.Number:
		return json.Number(v.AsBigFloat().Text('f', -1)), nil
	case ty.IsObjectType(), ty.IsMapType():
		return ctyMapToCanonical(v)
	case ty.IsListType(), ty.IsTupleType():
		return ctyListToCanonical(v)
	case ty.IsSetType():
		return ctySetToCanonical(v)
	}

	return nil, fmt.Errorf("cannot encode value of type %s", ty.FriendlyName())
}

func ctyMapToCanonical(v cty.Value) (any, error) {
	out := map[string]any{}

	for it := v.ElementIterator(); it.Next(); {
		k, val := it.Element()

		child, err := ctyToCanonical(val)
		if err != nil {
			return nil, err
		}

		out[k.AsString()] = child
	}

	return out, nil
}

func ctyListToCanonical(v cty.Value) (any, error) {
	out := make([]any, 0, v.LengthInt())

	for it := v.ElementIterator(); it.Next(); {
		_, val := it.Element()

		child, err := ctyToCanonical(val)
		if err != nil {
			return nil, err
		}

		out = append(out, child)
	}

	return out, nil
}

func ctySetToCanonical(v cty.Value) (any, error) {
	items := make([]any, 0, v.LengthInt())

	for it := v.ElementIterator(); it.Next(); {
		_, val := it.Element()

		child, err := ctyToCanonical(val)
		if err != nil {
			return nil, err
		}

		items = append(items, child)
	}

	encoded := make([]string, len(items))

	for i, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("encode set element: %w", err)
		}

		encoded[i] = string(b)
	}

	indexes := make([]int, len(items))
	for i := range indexes {
		indexes[i] = i
	}

	sort.SliceStable(indexes, func(i, j int) bool {
		return encoded[indexes[i]] < encoded[indexes[j]]
	})

	sorted := make([]any, len(items))
	for i, idx := range indexes {
		sorted[i] = items[idx]
	}

	return sorted, nil
}

// nowUnixFunc returns the current Unix timestamp in seconds. Non-deterministic
// by design: callers that need a stable value across multiple expressions must
// capture it once in a step-local var.
func nowUnixFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.Number),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.NumberIntVal(time.Now().Unix()), nil
		},
	})
}

// nowRFC3339Func returns the current UTC time formatted as RFC3339. UTC is
// explicit to avoid timezone drift between environments.
func nowRFC3339Func() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.StringVal(time.Now().UTC().Format(time.RFC3339)), nil
		},
	})
}

func ctyNumberToInt(value cty.Value) (int, error) {
	if value.Type() != cty.Number {
		return 0, fmt.Errorf("number value expected")
	}

	parsed, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		return 0, fmt.Errorf("integer value expected")
	}

	return int(parsed), nil
}

func ctyNumberToInt64(value cty.Value) (int64, error) {
	if value.Type() != cty.Number {
		return 0, fmt.Errorf("number value expected")
	}

	parsed, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		return 0, fmt.Errorf("integer value expected")
	}

	return parsed, nil
}

// totpFunc registers the totp(secret, options?) expression. The options object
// is decoded into a TOTPOptions struct; all validation lives inside
// GenerateTOTP so the rules stay in one place. Errors never echo the secret.
func totpFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: paramSecret, Type: cty.String},
		},
		VarParam: &function.Parameter{
			Name:             "options",
			Type:             cty.DynamicPseudoType,
			AllowDynamicType: true,
			AllowNull:        true,
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if len(args) > 2 {
				return cty.NilVal, fmt.Errorf("totp: too many arguments")
			}

			opts, err := decodeTOTPOptionsArg(args)
			if err != nil {
				return cty.NilVal, err
			}

			code, err := GenerateTOTP(args[0].AsString(), opts)
			if err != nil {
				return cty.NilVal, err
			}

			return cty.StringVal(code), nil
		},
	})
}

func decodeTOTPOptionsArg(args []cty.Value) (TOTPOptions, error) {
	if len(args) < 2 || args[1].IsNull() {
		return TOTPOptions{}, nil
	}

	optsVal := args[1]
	if !optsVal.Type().IsObjectType() {
		return TOTPOptions{}, fmt.Errorf("totp: options must be an object")
	}

	allowed := map[string]struct{}{
		totpOptionPeriod:    {},
		totpOptionDigits:    {},
		totpOptionAlgorithm: {},
		totpOptionTimestamp: {},
	}

	opts := TOTPOptions{}

	for name, attr := range optsVal.AsValueMap() {
		if _, ok := allowed[name]; !ok {
			return TOTPOptions{}, fmt.Errorf("totp: unknown option %q", name)
		}

		if attr.IsNull() {
			continue
		}

		if err := assignTOTPOption(&opts, name, attr); err != nil {
			return TOTPOptions{}, err
		}
	}

	return opts, nil
}

func assignTOTPOption(opts *TOTPOptions, name string, attr cty.Value) error {
	switch name {
	case totpOptionPeriod:
		period, err := ctyNumberToInt64(attr)
		if err != nil {
			return fmt.Errorf("totp: option %q must be an integer", name)
		}

		opts.Period = period
	case totpOptionDigits:
		digits, err := ctyNumberToInt(attr)
		if err != nil {
			return fmt.Errorf("totp: option %q must be an integer", name)
		}

		opts.Digits = digits
	case totpOptionAlgorithm:
		if attr.Type() != cty.String {
			return fmt.Errorf("totp: option %q must be a string", name)
		}

		opts.Algorithm = attr.AsString()
	case totpOptionTimestamp:
		timestamp, err := ctyNumberToInt64(attr)
		if err != nil {
			return fmt.Errorf("totp: option %q must be an integer", name)
		}

		// Use a pointer so an explicit timestamp=0 (a legal Unix epoch
		// value, documented as allowed) is not silently replaced with the
		// wall clock inside GenerateTOTP.
		opts.Timestamp = &timestamp
	}

	return nil
}

func baseFunctions() map[string]function.Function {
	out := map[string]function.Function{
		"env":              envFunc(),
		"jsonencode":       jsonEncodeFunc(),
		"now_unix":         nowUnixFunc(),
		"now_rfc3339":      nowRFC3339Func(),
		"totp":             totpFunc(),
		"regex_find":       regexFindFunc(),
		"url_encode":       urlEncodeFunc(),
		"base64url_encode": base64urlEncodeFunc(),
		"pkce_challenge":   pkceChallengeFunc(),
		"contains":         matcherSingleArg("contains"),
		"matches":          matchesFunc(),
		"exists":           matcherNoArg("exists"),
		"not_exists":       matcherNoArg("not_exists"),
		"is_string":        matcherNoArg("is_string"),
		"is_number":        matcherNoArg("is_number"),
		"is_bool":          matcherNoArg("is_bool"),
		"is_array":         matcherNoArg("is_array"),
		"is_object":        matcherNoArg("is_object"),
		"one_of":           oneOfFunc(),
		"can":              matcherSingleArg("can"),
		"optional":         optionalFunc(),
		"required":         requiredFunc(),
		"any":              matcherNoArg("any"),
	}

	for name, fn := range hashFunctions() {
		out[name] = fn
	}

	for name, fn := range hmacFunctions() {
		out[name] = fn
	}

	return out
}
