package transport

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// buildTransportFixture constructs the minimal tales.transport.v1.EchoService
// descriptor used by both transport client tests, plus the matching Types
// registry the codec / protojson resolver needs. Echo takes EchoRequest{text}
// and returns EchoResponse{text}; the test server simply echoes the value.
func buildTransportFixture(t *testing.T) (*protoregistry.Files, *protoregistry.Types, protoreflect.MessageDescriptor, protoreflect.MessageDescriptor) {
	t.Helper()

	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()

	stringField := func(name string, number int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(number),
			Type:     stringType,
			JsonName: proto.String(name),
		}
	}

	echoReq := &descriptorpb.DescriptorProto{
		Name:  proto.String("EchoRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{stringField("text", 1)},
	}
	echoResp := &descriptorpb.DescriptorProto{
		Name:  proto.String("EchoResponse"),
		Field: []*descriptorpb.FieldDescriptorProto{stringField("text", 1)},
	}

	service := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("EchoService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{
				Name:       proto.String("Echo"),
				InputType:  proto.String(".tales.transport.v1.EchoRequest"),
				OutputType: proto.String(".tales.transport.v1.EchoResponse"),
			},
		},
	}

	fd := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/transport/v1/echo.proto"),
		Package:     proto.String("tales.transport.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{echoReq, echoResp},
		Service:     []*descriptorpb.ServiceDescriptorProto{service},
	}

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("protodesc.NewFiles: %v", err)
	}

	reqDesc, err := files.FindDescriptorByName("tales.transport.v1.EchoRequest")
	if err != nil {
		t.Fatalf("find EchoRequest: %v", err)
	}

	respDesc, err := files.FindDescriptorByName("tales.transport.v1.EchoResponse")
	if err != nil {
		t.Fatalf("find EchoResponse: %v", err)
	}

	types := &protoregistry.Types{}
	for _, md := range []protoreflect.MessageDescriptor{reqDesc.(protoreflect.MessageDescriptor), respDesc.(protoreflect.MessageDescriptor)} {
		if err := types.RegisterMessage(dynamicpb.NewMessageType(md)); err != nil {
			t.Fatalf("register message: %v", err)
		}
	}

	return files, types, reqDesc.(protoreflect.MessageDescriptor), respDesc.(protoreflect.MessageDescriptor)
}
