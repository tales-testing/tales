package codec

import (
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider/rpc/descriptor"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestEncodeMessage_RoundTripScalars(t *testing.T) {
	t.Parallel()

	registry, types, input := loadEchoInput(t)

	value := cty.ObjectVal(map[string]cty.Value{
		"text": cty.StringVal("hello"),
	})

	msg, jsonBytes, err := EncodeMessage(input, value, types)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	if got := string(jsonBytes); got != `{"text":"hello"}` {
		t.Errorf("jsonBytes = %s, want %q", got, `{"text":"hello"}`)
	}

	// Round-trip back to cty.
	_, decoded, err := DecodeMessage(msg, types)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}

	if !decoded.Type().IsObjectType() {
		t.Fatalf("decoded type = %s, want object", decoded.Type().FriendlyName())
	}

	if got := decoded.GetAttr("text").AsString(); got != "hello" {
		t.Errorf("decoded.text = %q, want %q", got, "hello")
	}

	_ = registry
}

func TestEncodeMessage_EmptyValueEncodesAsEmptyMessage(t *testing.T) {
	t.Parallel()

	_, types, input := loadEchoInput(t)

	for _, tc := range []struct {
		name  string
		value cty.Value
	}{
		{"nil", cty.NilVal},
		{"null", cty.NullVal(cty.EmptyObject)},
		{"empty", cty.EmptyObjectVal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg, jsonBytes, err := EncodeMessage(input, tc.value, types)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}

			if !proto.Equal(msg, msg.New().Interface()) {
				// New() returns an empty message of the same type.
				t.Errorf("msg is not empty: %v", msg)
			}

			if string(jsonBytes) != "{}" {
				t.Errorf("jsonBytes = %s, want \"{}\"", string(jsonBytes))
			}
		})
	}
}

func TestEncodeMessage_RejectsNonObject(t *testing.T) {
	t.Parallel()

	_, types, input := loadEchoInput(t)

	_, _, err := EncodeMessage(input, cty.StringVal("not an object"), types)
	if err == nil {
		t.Fatal("expected error for non-object request")
	}

	if !strings.Contains(err.Error(), "request message must be an object") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestEncodeMessage_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	_, types, input := loadEchoInput(t)

	value := cty.ObjectVal(map[string]cty.Value{
		"text":         cty.StringVal("hi"),
		"unknown_attr": cty.StringVal("boom"),
	})

	_, _, err := EncodeMessage(input, value, types)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}

	if !strings.Contains(err.Error(), "unknown_attr") {
		t.Errorf("error = %q, want it to mention the unknown field", err.Error())
	}
}

func TestEncodeMessage_RejectsNilDescriptor(t *testing.T) {
	t.Parallel()

	_, _, err := EncodeMessage(nil, cty.EmptyObjectVal, nil)
	if err == nil {
		t.Fatal("expected error for nil descriptor")
	}
}

func TestDecodeMessage_NilMessage(t *testing.T) {
	t.Parallel()

	jsonBytes, value, err := DecodeMessage(nil, nil)
	if err != nil {
		t.Fatalf("DecodeMessage(nil): %v", err)
	}

	if string(jsonBytes) != "{}" {
		t.Errorf("jsonBytes = %s, want \"{}\"", string(jsonBytes))
	}

	if !value.RawEquals(cty.EmptyObjectVal) {
		t.Errorf("value = %#v, want EmptyObjectVal", value)
	}
}

func TestEncodeDecodeMessage_Repeated(t *testing.T) {
	t.Parallel()

	registry, types := loadRepeatedFixture(t)

	desc, err := registry.FindDescriptorByName("tales.codec.v1.ListReq")
	if err != nil {
		t.Fatalf("find ListReq: %v", err)
	}

	input, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("ListReq is not a message descriptor")
	}

	value := cty.ObjectVal(map[string]cty.Value{
		"tags": cty.ListVal([]cty.Value{cty.StringVal("rpc"), cty.StringVal("test")}),
	})

	msg, _, err := EncodeMessage(input, value, types)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	_, decoded, err := DecodeMessage(msg, types)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}

	tags := decoded.GetAttr("tags").AsValueSlice()
	if len(tags) != 2 || tags[0].AsString() != "rpc" || tags[1].AsString() != "test" {
		t.Errorf("tags = %v, want [rpc test]", tags)
	}
}

// loadEchoInput returns the test registry + types + the EchoRequest input
// descriptor reused from the descriptor package's testdata builder.
func loadEchoInput(t *testing.T) (*protoregistry.Files, *protoregistry.Types, protoreflect.MessageDescriptor) {
	t.Helper()

	registry := loadDescriptorFromHelper(t)

	types, err := descriptor.BuildTypes(registry)
	if err != nil {
		t.Fatalf("BuildTypes: %v", err)
	}

	desc, err := registry.FindDescriptorByName("tales.test.v1.EchoRequest")
	if err != nil {
		t.Fatalf("find EchoRequest: %v", err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("EchoRequest is not a message descriptor")
	}

	return registry, types, md
}

// loadDescriptorFromHelper builds a *protoregistry.Files containing the same
// tales.test.v1 fixture used by the descriptor package's own tests.
func loadDescriptorFromHelper(t *testing.T) *protoregistry.Files {
	t.Helper()

	set := buildSelfContainedSet(t)

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("protodesc.NewFiles: %v", err)
	}

	return files
}

// loadRepeatedFixture builds a tiny proto with one ListReq{ repeated string
// tags = 1 } message so the repeated-field test stays isolated from the
// generic tales.test.v1 fixture.
func loadRepeatedFixture(t *testing.T) (*protoregistry.Files, *protoregistry.Types) {
	t.Helper()

	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	labelRepeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	listReq := &descriptorpb.DescriptorProto{
		Name: proto.String("ListReq"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("tags"),
				Number:   proto.Int32(1),
				Label:    labelRepeated,
				Type:     stringType,
				JsonName: proto.String("tags"),
			},
		},
	}

	fd := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/codec/v1/list.proto"),
		Package:     proto.String("tales.codec.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{listReq},
	}

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("protodesc.NewFiles: %v", err)
	}

	types, err := descriptor.BuildTypes(files)
	if err != nil {
		t.Fatalf("BuildTypes: %v", err)
	}

	return files, types
}

// buildSelfContainedSet mirrors descriptor.buildTestFileSet (kept duplicated
// here because Go test helpers are not exported across packages) so the codec
// tests can build their own input descriptor without taking a dep on the
// descriptor package's internal test file.
func buildSelfContainedSet(t *testing.T) *descriptorpb.FileDescriptorSet {
	t.Helper()

	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()

	echoReq := &descriptorpb.DescriptorProto{
		Name: proto.String("EchoRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("text"), Number: proto.Int32(1), Type: stringType, JsonName: proto.String("text")},
		},
	}
	echoResp := &descriptorpb.DescriptorProto{
		Name: proto.String("EchoResponse"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("text"), Number: proto.Int32(1), Type: stringType, JsonName: proto.String("text")},
		},
	}

	fd := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/test/v1/test.proto"),
		Package:     proto.String("tales.test.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{echoReq, echoResp},
	}

	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}}
}
