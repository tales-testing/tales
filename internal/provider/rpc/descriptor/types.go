package descriptor

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// BuildTypes builds a *protoregistry.Types from a *protoregistry.Files.
// protojson requires a Resolver implementing both MessageTypeResolver and
// ExtensionTypeResolver; *protoregistry.Files only knows about descriptors,
// not types. We register every message in the file graph as a
// dynamicpb.NewMessageType, plus every enum and extension, so protojson can
// look them up while encoding / decoding the dynamic top-level message.
//
// Nested message types are registered recursively. Extensions are registered
// when present so extension-bearing schemas decode without surprises (they
// remain unparsed otherwise).
func BuildTypes(files *protoregistry.Files) (*protoregistry.Types, error) {
	if files == nil {
		return nil, fmt.Errorf("files registry is nil")
	}

	types := &protoregistry.Types{}

	var rangeErr error

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if err := registerMessages(types, fd.Messages()); err != nil {
			rangeErr = err

			return false
		}

		if err := registerEnums(types, fd.Enums()); err != nil {
			rangeErr = err

			return false
		}

		if err := registerExtensions(types, fd.Extensions()); err != nil {
			rangeErr = err

			return false
		}

		return true
	})

	if rangeErr != nil {
		return nil, rangeErr
	}

	return types, nil
}

func registerMessages(types *protoregistry.Types, msgs protoreflect.MessageDescriptors) error {
	for i := range msgs.Len() {
		md := msgs.Get(i)
		if err := types.RegisterMessage(dynamicpb.NewMessageType(md)); err != nil {
			return fmt.Errorf("register message %s: %w", md.FullName(), err)
		}

		if err := registerMessages(types, md.Messages()); err != nil {
			return err
		}

		if err := registerEnums(types, md.Enums()); err != nil {
			return err
		}

		if err := registerExtensions(types, md.Extensions()); err != nil {
			return err
		}
	}

	return nil
}

func registerEnums(types *protoregistry.Types, enums protoreflect.EnumDescriptors) error {
	for i := range enums.Len() {
		ed := enums.Get(i)
		if err := types.RegisterEnum(dynamicpb.NewEnumType(ed)); err != nil {
			return fmt.Errorf("register enum %s: %w", ed.FullName(), err)
		}
	}

	return nil
}

func registerExtensions(types *protoregistry.Types, exts protoreflect.ExtensionDescriptors) error {
	for i := range exts.Len() {
		ed := exts.Get(i)
		if err := types.RegisterExtension(dynamicpb.NewExtensionType(ed)); err != nil {
			return fmt.Errorf("register extension %s: %w", ed.FullName(), err)
		}
	}

	return nil
}
