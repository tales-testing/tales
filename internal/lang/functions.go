package lang

import (
	"fmt"
	"net/url"
	"os"
	"regexp"

	"github.com/hyperxlab/tales/internal/diagnostic"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
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
		"env":        envFunc(),
		"jsonencode": stdlib.JSONEncodeFunc,
		"regex_find": regexFindFunc(),
		"url_encode": urlEncodeFunc(),
		"contains":   matcherSingleArg("contains"),
		"matches":    matchesFunc(),
		"exists":     matcherNoArg("exists"),
		"not_exists": matcherNoArg("not_exists"),
		"is_string":  matcherNoArg("is_string"),
		"is_number":  matcherNoArg("is_number"),
		"is_bool":    matcherNoArg("is_bool"),
		"is_array":   matcherNoArg("is_array"),
		"is_object":  matcherNoArg("is_object"),
		"one_of":     oneOfFunc(),
		"can":        matcherSingleArg("can"),
	}
}
