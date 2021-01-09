package lang

import (
	"github.com/hyperxlab/tales/pkg/tales/lang/funcs"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// Functions returns the set of functions that should be used to when evaluating
// expressions in the receiving scope.
func (s *Scope) Functions() map[string]function.Function {
	s.funcsLock.Lock()

	if s.funcs == nil {
		s.funcs = map[string]function.Function{
			"abs":          stdlib.AbsoluteFunc,
			"base64decode": funcs.Base64DecodeFunc,
			"base64encode": funcs.Base64EncodeFunc,
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
			"md5":          funcs.Md5Func,
			"merge":        stdlib.MergeFunc,
			"min":          stdlib.MinFunc,
			"parseint":     stdlib.ParseIntFunc,
			"pow":          stdlib.PowFunc,
			"range":        stdlib.RangeFunc,
			"regex":        stdlib.RegexFunc,
			"regexall":     stdlib.RegexAllFunc,
			"sha1":         funcs.Sha1Func,
			"sha256":       funcs.Sha256Func,
			"sha512":       funcs.Sha512Func,
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
		}
	}

	s.funcsLock.Unlock()

	return s.funcs
}
