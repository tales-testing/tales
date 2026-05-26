package lang

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

const matcherKey = "__tales_matcher"

func matcherObject(name string, values map[string]cty.Value) cty.Value {
	payload := map[string]cty.Value{matcherKey: cty.StringVal(name)}
	for k, v := range values {
		payload[k] = v
	}

	return cty.ObjectVal(payload)
}

func envFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "name", Type: cty.String}},
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
		Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject(name, map[string]cty.Value{"value": args[0]}), nil
		},
	})
}

func optionalFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType, AllowNull: true}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("optional", map[string]cty.Value{"value": args[0]}), nil
		},
	})
}

func requiredFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType, AllowNull: true}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("required", map[string]cty.Value{"value": args[0]}), nil
		},
	})
}

func matchesFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "pattern", Type: cty.String}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			pattern := args[0].AsString()
			if _, err := regexp.Compile(pattern); err != nil {
				return cty.NilVal, fmt.Errorf("invalid regex %q: %w", pattern, err)
			}

			return matcherObject("matches", map[string]cty.Value{"value": args[0]}), nil
		},
	})
}

func oneOfFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "values", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return matcherObject("one_of", map[string]cty.Value{"value": args[0]}), nil
		},
	})
}

func regexFindFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "value", Type: cty.DynamicPseudoType},
			{Name: "pattern", Type: cty.String},
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
		Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			return cty.StringVal(url.QueryEscape(diagnostic.ScalarString(args[0]))), nil
		},
	})
}

// jsonEncodeFunc serializes a cty.Value to a canonical JSON string with
// alphabetically-sorted object keys. The deterministic output makes it safe
// to use as the input of an HMAC or any other signature scheme.
func jsonEncodeFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "value", Type: cty.DynamicPseudoType, AllowNull: true}},
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

// hmacSHA256HexFunc computes HMAC-SHA256(secret, message) and returns the
// digest as lowercase hex. Errors never embed the secret or message.
func hmacSHA256HexFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "secret", Type: cty.String},
			{Name: "message", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			mac := hmac.New(sha256.New, []byte(args[0].AsString()))
			if _, err := mac.Write([]byte(args[1].AsString())); err != nil {
				return cty.NilVal, fmt.Errorf("hmac_sha256_hex: write failed")
			}

			return cty.StringVal(hex.EncodeToString(mac.Sum(nil))), nil
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

func baseFunctions() map[string]function.Function {
	return map[string]function.Function{
		"env":             envFunc(),
		"jsonencode":      jsonEncodeFunc(),
		"now_unix":        nowUnixFunc(),
		"now_rfc3339":     nowRFC3339Func(),
		"hmac_sha256_hex": hmacSHA256HexFunc(),
		"regex_find":      regexFindFunc(),
		"url_encode":      urlEncodeFunc(),
		"contains":        matcherSingleArg("contains"),
		"matches":         matchesFunc(),
		"exists":          matcherNoArg("exists"),
		"not_exists":      matcherNoArg("not_exists"),
		"is_string":       matcherNoArg("is_string"),
		"is_number":       matcherNoArg("is_number"),
		"is_bool":         matcherNoArg("is_bool"),
		"is_array":        matcherNoArg("is_array"),
		"is_object":       matcherNoArg("is_object"),
		"one_of":          oneOfFunc(),
		"can":             matcherSingleArg("can"),
		"optional":        optionalFunc(),
		"required":        requiredFunc(),
		"any":             matcherNoArg("any"),
	}
}
