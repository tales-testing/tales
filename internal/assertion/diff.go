package assertion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/zclconf/go-cty/cty"
)

// unknownOperand stands in for a value that is not known yet. It is not
// valid JSON, so it cannot be confused with a string.
const unknownOperand = "(unknown)"

// Mismatch describes one assertion mismatch.
type Mismatch struct {
	Kind    string
	Path    string
	Want    interface{}
	Got     interface{}
	Message string
}

func (m *Mismatch) Error() string {
	if m.Message != "" {
		return fmt.Sprintf("%s at %s: %s", m.Kind, m.Path, m.Message)
	}

	return fmt.Sprintf("%s at %s: want=%s got=%s", m.Kind, m.Path, renderOperand(m.Want), renderOperand(m.Got))
}

// renderOperand renders one side of a mismatch as JSON. `%v` on a
// cty.Value prints the struct behind it (`{{{{} 83}} abc}`, where 83 is
// the kind of a string), not the value (issue #81). A string keeps its
// quotes, so leading and trailing whitespace stays visible. Object fields
// named like secrets are masked with the same rule the report applies to
// its want/got fields. A value JSON cannot represent (infinity, a marked
// value) falls back to its Go syntax.
func renderOperand(value interface{}) string {
	v, isCTY := value.(cty.Value)
	if !isCTY {
		if encoded, ok := encodeOperand(value); ok {
			return encoded
		}

		return fmt.Sprintf("%v", value)
	}

	if !v.IsKnown() {
		return unknownOperand
	}

	native, ok := ctyToJSON(v)
	if !ok {
		return v.GoString()
	}

	if encoded, ok := encodeOperand(native); ok {
		return encoded
	}

	return v.GoString()
}

// encodeOperand encodes value on a single line, secrets masked and
// without the HTML escaping of json.Marshal, which would print a URL's
// `&` as `\u0026`.
func encodeOperand(value interface{}) (string, bool) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(diagnostic.SanitizeUnknown(value)); err != nil {
		return "", false
	}

	return strings.TrimSuffix(buf.String(), "\n"), true
}

// ctyToJSON converts v into the encoding/json data model. Numbers become
// json.Number so they print with every digit, as ctyjson.Marshal does.
// ok is false when some part of v has no JSON form.
func ctyToJSON(v cty.Value) (interface{}, bool) {
	if v.IsMarked() || !v.IsKnown() {
		return nil, false
	}

	if v.IsNull() {
		return nil, true
	}

	ty := v.Type()

	switch {
	case ty == cty.String:
		return v.AsString(), true
	case ty == cty.Bool:
		return v.True(), true
	case ty == cty.Number:
		number := v.AsBigFloat()
		if number.IsInf() {
			return nil, false
		}

		return json.Number(number.Text('f', -1)), true
	case ty.IsObjectType() || ty.IsMapType():
		return ctyObjectToJSON(v)
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		return ctyArrayToJSON(v)
	default:
		return nil, false
	}
}

func ctyObjectToJSON(v cty.Value) (interface{}, bool) {
	out := make(map[string]interface{}, v.LengthInt())

	for it := v.ElementIterator(); it.Next(); {
		key, elem := it.Element()

		native, ok := ctyToJSON(elem)
		if !ok {
			return nil, false
		}

		out[key.AsString()] = native
	}

	return out, true
}

func ctyArrayToJSON(v cty.Value) (interface{}, bool) {
	out := make([]interface{}, 0, v.LengthInt())

	for it := v.ElementIterator(); it.Next(); {
		_, elem := it.Element()

		native, ok := ctyToJSON(elem)
		if !ok {
			return nil, false
		}

		out = append(out, native)
	}

	return out, true
}
