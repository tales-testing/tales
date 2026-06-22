// Package codec converts request and response messages between cty values (the
// Tales expression engine's runtime representation) and dynamic Protobuf
// messages built from descriptors loaded at runtime. No code generation is
// involved; everything goes through dynamicpb plus protojson with the user's
// descriptor registry as the type resolver.
package codec

import (
	"encoding/json"
	"fmt"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// EncodeMessage builds a dynamicpb.Message from a cty value carrying the
// step's request payload. The message descriptor describes the wire shape
// (input message of the resolved method). The types registry provides
// lookups for any imported types (including google.protobuf.Timestamp and
// friends when they appear in the schema); pass descriptor.BuildTypes(files)
// to obtain it from the descriptor.Files loaded for the step's target.
//
// The cty value is round-tripped through canonical JSON: ctyjson.Marshal
// produces a deterministic byte sequence with sorted object keys, then
// protojson.Unmarshal populates the dynamic message. Unknown fields are
// rejected (DiscardUnknown: false) so a typo in the test fixture surfaces
// as a clear failure instead of being silently dropped.
//
// The returned JSON bytes are the canonical request representation; the
// caller writes them to the request.json artifact and never echoes the raw
// cty value (which can include secrets via expression evaluation).
func EncodeMessage(input protoreflect.MessageDescriptor, value cty.Value, types *protoregistry.Types) (*dynamicpb.Message, []byte, error) {
	if input == nil {
		return nil, nil, fmt.Errorf("input message descriptor is nil")
	}

	msg := dynamicpb.NewMessage(input)

	jsonBytes, err := ctyToJSON(value)
	if err != nil {
		return nil, nil, fmt.Errorf("convert request to JSON: %w", err)
	}

	opts := protojson.UnmarshalOptions{
		Resolver:       types,
		DiscardUnknown: false,
	}

	if err := opts.Unmarshal(jsonBytes, msg); err != nil {
		return nil, nil, fmt.Errorf("encode message %s: %w", input.FullName(), err)
	}

	return msg, jsonBytes, nil
}

// ctyToJSON serializes a cty value as canonical JSON. A nil / null / empty
// cty value becomes "{}" so the caller can hand an "empty message" to
// protojson without special-casing. Unknown values fail clearly because they
// would silently lose information.
func ctyToJSON(value cty.Value) ([]byte, error) {
	if value == cty.NilVal {
		return []byte("{}"), nil
	}

	if !value.IsKnown() {
		return nil, fmt.Errorf("request message is not fully known at execution time")
	}

	if value.IsNull() {
		return []byte("{}"), nil
	}

	typ := value.Type()
	if !typ.IsObjectType() && !typ.IsMapType() {
		return nil, fmt.Errorf("request message must be an object, got %s", typ.FriendlyName())
	}

	encoded, err := ctyjson.Marshal(value, typ)
	if err != nil {
		return nil, fmt.Errorf("marshal cty to JSON: %w", err)
	}

	// ctyjson may emit a JSON value with non-deterministic key ordering on
	// some cty versions; round-trip through encoding/json to enforce
	// deterministic, sorted-key output so the artifact is reproducible.
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return nil, fmt.Errorf("normalize JSON: %w", err)
	}

	canonical, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON: %w", err)
	}

	return canonical, nil
}
