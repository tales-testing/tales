package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func createEvalContext() *hcl.EvalContext {
	variables := map[string]cty.Value{
		"config":  cty.ObjectVal(map[string]cty.Value{}),
		"keyword": cty.ObjectVal(map[string]cty.Value{}),
		"env":     cty.ObjectVal(map[string]cty.Value{}),
	}

	functions := map[string]function.Function{
		"abs":          stdlib.AbsoluteFunc,
		"ceil":         stdlib.CeilFunc,
		"chomp":        stdlib.ChompFunc,
		"chunklist":    stdlib.ChunklistFunc,
		"coalescelist": stdlib.CoalesceListFunc,
		"compact":      stdlib.CompactFunc,
		"concat":       stdlib.ConcatFunc,
		"contains":     stdlib.ContainsFunc,
		"csvdecode":    stdlib.CSVDecodeFunc,
		"distinct":     stdlib.DistinctFunc,
		"element":      stdlib.ElementFunc,
		"flatten":      stdlib.FlattenFunc,
		"floor":        stdlib.FloorFunc,
		"format":       stdlib.FormatFunc,
		"formatdate":   stdlib.FormatDateFunc,
		"formatlist":   stdlib.FormatListFunc,
		"indent":       stdlib.IndentFunc,
		"join":         stdlib.JoinFunc,
		"jsondecode":   stdlib.JSONDecodeFunc,
		"jsonencode":   stdlib.JSONEncodeFunc,
		"keys":         stdlib.KeysFunc,
		"lower":        stdlib.LowerFunc,
		"max":          stdlib.MaxFunc,
		"merge":        stdlib.MergeFunc,
		"min":          stdlib.MinFunc,
		"parseint":     stdlib.ParseIntFunc,
		"pow":          stdlib.PowFunc,
		"range":        stdlib.RangeFunc,
		"regex":        stdlib.RegexFunc,
		"regexall":     stdlib.RegexAllFunc,
		"signum":       stdlib.SignumFunc,
		"slice":        stdlib.SliceFunc,
		"sort":         stdlib.SortFunc,
		"split":        stdlib.SplitFunc,
		"strrev":       stdlib.ReverseFunc,
		"substr":       stdlib.SubstrFunc,
		"trim":         stdlib.TrimFunc,
		"trimprefix":   stdlib.TrimPrefixFunc,
		"trimspace":    stdlib.TrimSpaceFunc,
		"trimsuffix":   stdlib.TrimSuffixFunc,
		"upper":        stdlib.UpperFunc,
		"values":       stdlib.ValuesFunc,
		"json": function.New(&function.Spec{
			// Params represents required positional arguments, of which random
			// has none.
			Params: []function.Parameter{},
			// VarParam allows a "VarArgs" type input, in this case, of
			// strings.
			VarParam: &function.Parameter{Type: cty.DynamicPseudoType},
			// Type is used to determine the output type from the inputs. In
			// the case of Random it only accepts strings and only returns
			// strings.
			Type: function.StaticReturnType(cty.String),
			// Impl is the actual function. A "VarArgs" number of cty.String
			// will be passed in and a random one returned, also as a
			// cty.String.
			Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
				b, err := ctyjson.Marshal(args[0], args[0].Type())
				if err != nil {
					return cty.Value{}, err
				}

				return cty.StringVal(string(b)), nil
			},
		}),
	}

	return &hcl.EvalContext{
		Variables: variables,
		Functions: functions,
	}
}
