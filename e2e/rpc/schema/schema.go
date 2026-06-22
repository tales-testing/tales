// Package schema builds the FileDescriptorSet for the Tales rpc e2e
// fixture. It is the single source of truth shared between the genbin tool
// (which serializes it to descriptor.bin for Tales to load at runtime) and
// the mockserver (which uses it directly to dispatch dynamic requests).
// proto/tales/rpc/v1/test.proto is the human-readable reference; this
// builder mirrors it byte-for-byte.
package schema

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// FullPackage is the proto package of the test service. Exposed so callers
// can build the canonical /pkg.Service/Method path without retyping it.
const FullPackage = "tales.rpc.v1"

// FullService is the canonical fully-qualified name of the TestService.
const FullService = FullPackage + ".TestService"

// BuildFileDescriptorSet returns the file set with one entry, matching
// proto/tales/rpc/v1/test.proto exactly.
func BuildFileDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{BuildFile()}}
}

// BuildFile returns the FileDescriptorProto. Kept separate so callers can
// merge it into a larger set if they ever need to.
func BuildFile() *descriptorpb.FileDescriptorProto {
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	enumType := descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum()
	labelOptional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	labelRepeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	stringField := func(name string, number int32, repeated bool) *descriptorpb.FieldDescriptorProto {
		label := labelOptional
		if repeated {
			label = labelRepeated
		}

		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(number),
			Label:    label,
			Type:     stringType,
			JsonName: proto.String(name),
		}
	}

	statusField := func(name string, number int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(number),
			Label:    labelOptional,
			Type:     enumType,
			TypeName: proto.String(".tales.rpc.v1.Status"),
			JsonName: proto.String(name),
		}
	}

	echoFields := []*descriptorpb.FieldDescriptorProto{
		stringField("id", 1, false),
		stringField("name", 2, false),
		stringField("tags", 3, true),
		statusField("status", 4),
	}

	echoReq := &descriptorpb.DescriptorProto{Name: proto.String("EchoRequest"), Field: echoFields}
	echoResp := &descriptorpb.DescriptorProto{Name: proto.String("EchoResponse"), Field: echoFields}

	failReq := &descriptorpb.DescriptorProto{
		Name:  proto.String("FailRequest"),
		Field: []*descriptorpb.FieldDescriptorProto{stringField("reason", 1, false)},
	}
	failResp := &descriptorpb.DescriptorProto{Name: proto.String("FailResponse")}

	statusEnum := &descriptorpb.EnumDescriptorProto{
		Name: proto.String("Status"),
		Value: []*descriptorpb.EnumValueDescriptorProto{
			{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
			{Name: proto.String("STATUS_ACTIVE"), Number: proto.Int32(1)},
			{Name: proto.String("STATUS_DISABLED"), Number: proto.Int32(2)},
		},
	}

	service := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("TestService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{
				Name:       proto.String("Echo"),
				InputType:  proto.String(".tales.rpc.v1.EchoRequest"),
				OutputType: proto.String(".tales.rpc.v1.EchoResponse"),
			},
			{
				Name:       proto.String("Fail"),
				InputType:  proto.String(".tales.rpc.v1.FailRequest"),
				OutputType: proto.String(".tales.rpc.v1.FailResponse"),
			},
		},
	}

	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/rpc/v1/test.proto"),
		Package:     proto.String(FullPackage),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{echoReq, echoResp, failReq, failResp},
		EnumType:    []*descriptorpb.EnumDescriptorProto{statusEnum},
		Service:     []*descriptorpb.ServiceDescriptorProto{service},
	}
}
