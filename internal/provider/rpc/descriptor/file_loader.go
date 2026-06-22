package descriptor

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// FileLoader loads a FileDescriptorSet serialized to disk (the .bin produced
// by `buf build -o ...` or `protoc --descriptor_set_out=...`). Path must be
// absolute; the caller is responsible for resolving it against the project
// directory.
type FileLoader struct {
	Path string
}

// Load reads the file, unmarshals it as a FileDescriptorSet, and builds the
// registry. Errors are explicit so users can tell missing file from invalid
// binary from broken descriptors.
func (l *FileLoader) Load(_ context.Context) (*protoregistry.Files, error) {
	if l.Path == "" {
		return nil, fmt.Errorf("descriptor file path is empty")
	}

	bytes, err := os.ReadFile(l.Path)
	if err != nil {
		return nil, fmt.Errorf("read descriptor file %q: %w", l.Path, err)
	}

	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(bytes, set); err != nil {
		return nil, fmt.Errorf("invalid descriptor file %q: %w", l.Path, err)
	}

	return buildFiles(set)
}

// Source returns the file path used in error messages.
func (l *FileLoader) Source() string {
	return l.Path
}
