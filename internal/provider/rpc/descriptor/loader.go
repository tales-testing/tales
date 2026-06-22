// Package descriptor loads Protobuf file descriptors at runtime from either
// a serialized FileDescriptorSet on disk or a gRPC server's reflection
// service, and resolves them to the message descriptors the rpc provider
// needs for dynamic encoding and decoding. No protobuf code generation
// happens; everything is read at runtime so a Tales binary works against any
// service whose schema it can load.
package descriptor

import (
	"context"

	"google.golang.org/protobuf/reflect/protoregistry"
)

// Loader produces a *protoregistry.Files registry from one descriptor source.
// Implementations must be safe to call concurrently if shared; the Registry
// type below serializes calls per descriptor name so concurrent step
// executions never produce duplicate loads.
type Loader interface {
	// Load builds the registry. The ctx applies to any I/O (file reads,
	// network calls for reflection).
	Load(ctx context.Context) (*protoregistry.Files, error)
	// Source returns a short human-readable identifier of the loader (path
	// or remote address) for error messages. It must never include
	// credentials.
	Source() string
}
