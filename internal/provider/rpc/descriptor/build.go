package descriptor

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildFiles constructs a *protoregistry.Files from a FileDescriptorSet. It
// preserves the order of the files in the set so the resulting registry's
// iteration order is deterministic for tests. Errors point to the offending
// file name and the underlying protodesc failure (typically an unresolved
// import; users must build their descriptor.bin with `--include_imports`).
func buildFiles(set *descriptorpb.FileDescriptorSet) (*protoregistry.Files, error) {
	if set == nil {
		return nil, fmt.Errorf("descriptor set is nil")
	}

	if len(set.File) == 0 {
		return nil, fmt.Errorf("descriptor set is empty")
	}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("build descriptor registry: %w (descriptors must be self-contained: rebuild with `buf build --as-file-descriptor-set` or `protoc --include_imports`)", err)
	}

	return files, nil
}
