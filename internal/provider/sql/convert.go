package sql

import (
	"fmt"
	"math/big"
	"time"
	"unicode/utf8"

	"github.com/zclconf/go-cty/cty"
)

// ListArg marks an argument written as a nested list in .tales
// (args = [[1, 2, 3]]). The provider expands the placeholder bound to it into
// a run of placeholders (IN ($1,$2,$3) / IN (?,?,?)) and flattens the values
// before binding them, because no driver protocol binds a composite value to a
// single placeholder. Elements are already lowered to database/sql parameter
// values.
type ListArg []any

// ConvertArgs lowers a list of cty values into Go values usable as
// database/sql parameters. Scalars bind directly; a nested list becomes a
// ListArg, which the provider later expands into a placeholder run. Objects
// and maps are still rejected: no composite binding exists for them.
func ConvertArgs(args []cty.Value) ([]any, error) {
	out := make([]any, 0, len(args))

	for i, arg := range args {
		converted, err := ConvertArg(arg)
		if err != nil {
			return nil, fmt.Errorf("unsupported SQL arg type at args[%d]: %w", i, err)
		}

		out = append(out, converted)
	}

	return out, nil
}

// ConvertArg lowers a single cty value into a database/sql parameter value.
// Null values map to Go nil so drivers bind a SQL NULL.
func ConvertArg(value cty.Value) (any, error) {
	if !value.IsKnown() {
		return nil, fmt.Errorf("value is unknown")
	}

	if value.IsNull() {
		return nil, nil
	}

	switch {
	case value.Type() == cty.String:
		return value.AsString(), nil
	case value.Type() == cty.Bool:
		return value.True(), nil
	case value.Type() == cty.Number:
		return numberToDriverValue(value), nil
	case value.Type().IsSetType():
		// A cty set has no source order, so the expanded placeholder run
		// would not be reproducible. Refuse rather than emit a
		// non-deterministic statement.
		return nil, fmt.Errorf("sets are not supported as SQL args; use a list")
	case value.Type().IsTupleType(), value.Type().IsListType():
		return convertListArg(value)
	default:
		return nil, fmt.Errorf("%s", value.Type().FriendlyName())
	}
}

// convertListArg lowers a cty tuple / list into a ListArg. Nesting is
// rejected: a placeholder can expand into a flat run of values only.
func convertListArg(value cty.Value) (ListArg, error) {
	elements := value.AsValueSlice()
	out := make(ListArg, 0, len(elements))

	for i, element := range elements {
		converted, err := ConvertArg(element)
		if err != nil {
			return nil, fmt.Errorf("at index %d: %w", i, err)
		}

		if _, nested := converted.(ListArg); nested {
			return nil, fmt.Errorf("nested list arguments are not supported at index %d", i)
		}

		out = append(out, converted)
	}

	return out, nil
}

// numberToDriverValue converts a cty.Number to int64 when it represents a
// whole number that fits in int64; otherwise it falls back to float64. This
// preserves big integer IDs (bigint primary keys) that would otherwise be
// silently truncated by a naive float conversion.
func numberToDriverValue(value cty.Value) any {
	bf := value.AsBigFloat()

	if bf.IsInt() {
		i, acc := bf.Int64()
		if acc == big.Exact {
			return i
		}
	}

	f, _ := bf.Float64()

	return f
}

// ConvertRowValue normalizes a value returned by database/sql.Rows.Scan into
// a cty.Value the rest of Tales can match against.
func ConvertRowValue(value any) (cty.Value, error) {
	if value == nil {
		return cty.NullVal(cty.DynamicPseudoType), nil
	}

	if v, ok := convertRowScalar(value); ok {
		return v, nil
	}

	switch v := value.(type) {
	case []byte:
		if !utf8.Valid(v) {
			return cty.NilVal, fmt.Errorf("non-UTF-8 bytes returned by driver; cannot convert to string")
		}

		return cty.StringVal(string(v)), nil
	case time.Time:
		return cty.StringVal(v.Format(time.RFC3339Nano)), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported column type %T", value)
	}
}

// convertRowScalar handles the boolean / numeric / string fast path. The
// returned ok flag is false for values that need richer handling (byte slices
// and time.Time).
func convertRowScalar(value any) (cty.Value, bool) {
	switch v := value.(type) {
	case bool:
		return cty.BoolVal(v), true
	case int:
		return cty.NumberIntVal(int64(v)), true
	case int8:
		return cty.NumberIntVal(int64(v)), true
	case int16:
		return cty.NumberIntVal(int64(v)), true
	case int32:
		return cty.NumberIntVal(int64(v)), true
	case int64:
		return cty.NumberIntVal(v), true
	case uint8:
		return cty.NumberIntVal(int64(v)), true
	case uint16:
		return cty.NumberIntVal(int64(v)), true
	case uint32:
		return cty.NumberIntVal(int64(v)), true
	case uint, uint64:
		return convertUnsignedNumber(v), true
	case float32:
		return cty.NumberFloatVal(float64(v)), true
	case float64:
		return cty.NumberFloatVal(v), true
	case string:
		return cty.StringVal(v), true
	default:
		return cty.NilVal, false
	}
}

// convertUnsignedNumber maps uint / uint64 values to cty numbers using
// big.NewFloat so values larger than math.MaxInt64 still round-trip safely
// (golangci-lint flags the naive int64 conversion as G115).
func convertUnsignedNumber(value any) cty.Value {
	var u uint64

	switch v := value.(type) {
	case uint:
		u = uint64(v)
	case uint64:
		u = v
	default:
		return cty.NullVal(cty.Number)
	}

	bf := new(big.Float).SetUint64(u)

	return cty.NumberVal(bf)
}
