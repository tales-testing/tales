package sql

import (
	"fmt"
	"math/big"
	"time"
	"unicode/utf8"

	"github.com/zclconf/go-cty/cty"
)

// ConvertArgs lowers a list of cty values into Go values usable as
// database/sql parameters. Scalars only: cty lists and objects are rejected
// in V1 because the standard driver protocol can not bind composite values
// without provider-specific helpers.
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

	switch value.Type() {
	case cty.String:
		return value.AsString(), nil
	case cty.Bool:
		return value.True(), nil
	case cty.Number:
		return numberToDriverValue(value), nil
	default:
		return nil, fmt.Errorf("%s", value.Type().FriendlyName())
	}
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
