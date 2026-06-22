package codec

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// DecodeMessage converts a dynamic Protobuf message into the canonical JSON
// representation plus the matching cty value the assertion engine consumes.
//
// V1 caveat: protojson encodes int64 / uint64 / fixed64 / sfixed64 / sint64
// fields as JSON strings (per the proto3 JSON spec, to preserve precision in
// JavaScript). The resulting cty value therefore carries those numerics as
// cty.String; tests must assert with `"42"`, not `42`, for those types. Other
// scalar fields (int32, double, bool, ...) round-trip as JSON numbers and
// match natively. Documented at the provider docs page.
//
// EmitUnpopulated is false by default so response assertions only see fields
// the server actually set — closer to user expectations and consistent with
// the HTTP provider's response.json shape.
func DecodeMessage(msg *dynamicpb.Message, types *protoregistry.Types) ([]byte, cty.Value, error) {
	if msg == nil {
		return []byte("{}"), cty.EmptyObjectVal, nil
	}

	opts := protojson.MarshalOptions{
		Resolver:        types,
		EmitUnpopulated: false,
		UseProtoNames:   false,
	}

	jsonBytes, err := opts.Marshal(msg)
	if err != nil {
		return nil, cty.NilVal, fmt.Errorf("decode message %s: %w", msg.Descriptor().FullName(), err)
	}

	if len(jsonBytes) == 0 {
		return []byte("{}"), cty.EmptyObjectVal, nil
	}

	inferredType, err := ctyjson.ImpliedType(jsonBytes)
	if err != nil {
		return jsonBytes, cty.NilVal, fmt.Errorf("infer response cty type: %w", err)
	}

	value, err := ctyjson.Unmarshal(jsonBytes, inferredType)
	if err != nil {
		return jsonBytes, cty.NilVal, fmt.Errorf("unmarshal response into cty: %w", err)
	}

	return jsonBytes, value, nil
}
