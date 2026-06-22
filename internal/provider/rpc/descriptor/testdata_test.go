package descriptor

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildTestFileSet returns a serialized FileDescriptorSet containing a single
// proto3 file tales/test/v1/test.proto with TestService and three methods:
// Echo (unary), Fail (unary returning a structured failure), and WatchStream
// (server-streaming) used to exercise the streaming-rejection branch in
// Resolve. The set is self-contained so it loads cleanly through buildFiles
// without external imports.
func buildTestFileSet(t *testing.T) []byte {
	t.Helper()

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{buildTestFileDescriptor()}}

	bytes, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal test file set: %v", err)
	}

	return bytes
}

func buildTestFileDescriptor() *descriptorpb.FileDescriptorProto {
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	labelOptional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	streamFalse := proto.Bool(false)
	streamTrue := proto.Bool(true)

	stringField := func(name string, number int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(number),
			Label:    labelOptional,
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
	failReq := &descriptorpb.DescriptorProto{
		Name:  proto.String("FailRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{stringField("reason", 1)},
	}
	failResp := &descriptorpb.DescriptorProto{
		Name: proto.String("FailResponse"),
	}

	service := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("TestService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{
				Name:            proto.String("Echo"),
				InputType:       proto.String(".tales.test.v1.EchoRequest"),
				OutputType:      proto.String(".tales.test.v1.EchoResponse"),
				ClientStreaming: streamFalse,
				ServerStreaming: streamFalse,
			},
			{
				Name:            proto.String("Fail"),
				InputType:       proto.String(".tales.test.v1.FailRequest"),
				OutputType:      proto.String(".tales.test.v1.FailResponse"),
				ClientStreaming: streamFalse,
				ServerStreaming: streamFalse,
			},
			{
				Name:            proto.String("WatchStream"),
				InputType:       proto.String(".tales.test.v1.EchoRequest"),
				OutputType:      proto.String(".tales.test.v1.EchoResponse"),
				ClientStreaming: streamFalse,
				ServerStreaming: streamTrue,
			},
		},
	}

	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/test/v1/test.proto"),
		Package:     proto.String("tales.test.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{echoReq, echoResp, failReq, failResp},
		Service:     []*descriptorpb.ServiceDescriptorProto{service},
	}
}
