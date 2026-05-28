package browser

import (
	"fmt"
	"sort"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func readAttr(value cty.Value, name string) (cty.Value, error) {
	if value.IsNull() || !value.IsKnown() {
		return cty.NilVal, fmt.Errorf("not an object")
	}

	switch {
	case value.Type().IsObjectType():
		if !value.Type().HasAttribute(name) {
			return cty.NilVal, fmt.Errorf("missing %q", name)
		}

		return value.GetAttr(name), nil
	case value.Type().IsMapType():
		key := cty.StringVal(name)

		has := value.HasIndex(key)
		if !has.IsKnown() || has.IsNull() || !has.True() {
			return cty.NilVal, fmt.Errorf("missing %q", name)
		}

		return value.Index(key), nil
	default:
		return cty.NilVal, fmt.Errorf("not an object")
	}
}

func listKeys(value cty.Value) ([]string, error) {
	if value.IsNull() || !value.IsKnown() {
		return nil, fmt.Errorf("not an object")
	}

	keys := make([]string, 0)

	switch {
	case value.Type().IsObjectType():
		for name := range value.Type().AttributeTypes() {
			keys = append(keys, name)
		}
	case value.Type().IsMapType():
		for it := value.ElementIterator(); it.Next(); {
			k, _ := it.Element()
			if k.Type() == cty.String && !k.IsNull() {
				keys = append(keys, k.AsString())
			}
		}
	default:
		return nil, fmt.Errorf("not an object")
	}

	sort.Strings(keys)

	return keys, nil
}

func readOptionalAttr(value cty.Value, name string) (cty.Value, bool) {
	attr, err := readAttr(value, name)
	if err != nil {
		return cty.NilVal, false
	}

	return attr, true
}

func readOptionalString(value cty.Value, name string) (string, bool) {
	attr, ok := readOptionalAttr(value, name)
	if !ok {
		return "", false
	}

	if attr.IsNull() || attr.Type() != cty.String {
		return "", false
	}

	return attr.AsString(), true
}

func readOptionalInt(value cty.Value, name string) (int, bool, error) {
	attr, ok := readOptionalAttr(value, name)
	if !ok || attr.IsNull() {
		return 0, false, nil
	}

	if attr.Type() != cty.Number {
		return 0, false, fmt.Errorf("%q must be a number", name)
	}

	n, acc := attr.AsBigFloat().Int64()
	if acc != 0 {
		return 0, false, fmt.Errorf("%q must be an integer", name)
	}

	return int(n), true, nil
}

func readOptionalBool(value cty.Value, name string) (bool, bool, error) {
	attr, ok := readOptionalAttr(value, name)
	if !ok || attr.IsNull() {
		return false, false, nil
	}

	if attr.Type() != cty.Bool {
		return false, false, fmt.Errorf("%q must be a bool", name)
	}

	return attr.True(), true, nil
}

func readOptionalDuration(value cty.Value, name string) (time.Duration, bool, error) {
	raw, ok := readOptionalString(value, name)
	if !ok {
		return 0, false, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%q is not a duration: %w", name, err)
	}

	return d, true, nil
}

func readOptionalStringList(value cty.Value, name string) ([]string, bool, error) {
	attr, ok := readOptionalAttr(value, name)
	if !ok || attr.IsNull() {
		return nil, false, nil
	}

	t := attr.Type()
	if !t.IsListType() && !t.IsTupleType() {
		return nil, false, fmt.Errorf("%q must be a list of strings", name)
	}

	out := make([]string, 0, attr.LengthInt())

	for it := attr.ElementIterator(); it.Next(); {
		_, v := it.Element()
		if v.Type() != cty.String || v.IsNull() {
			return nil, false, fmt.Errorf("%q must be a list of strings", name)
		}

		out = append(out, v.AsString())
	}

	return out, true, nil
}
