package descriptor

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/reflect/protoregistry"
)

// Registry caches loaded descriptor sets per logical name (the key the user
// gave the descriptor entry in config.rpc.descriptors). Concurrent scenarios
// sharing one descriptor name pay the load cost exactly once; subsequent
// calls return the cached *protoregistry.Files. The cache serializes
// concurrent loads of the same name via a per-entry sync.Once.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// entry holds either the cached files or the in-flight load. Each named
// descriptor has its own load lock so two unrelated descriptors can load
// concurrently while two callers asking for the same name serialize.
type entry struct {
	once  sync.Once
	files *protoregistry.Files
	err   error
}

// NewRegistry returns an empty cache.
func NewRegistry() *Registry {
	return &Registry{entries: map[string]*entry{}}
}

// Get returns the cached Files for name, loading via loader on first request.
// loader is only invoked when the name has no cached entry; further calls
// reuse the first result (success or error). Errors are wrapped with the
// descriptor name so the caller can surface it to the user.
func (r *Registry) Get(ctx context.Context, name string, loader Loader) (*protoregistry.Files, error) {
	if name == "" {
		return nil, fmt.Errorf("descriptor name is empty")
	}

	if loader == nil {
		return nil, fmt.Errorf("descriptor %q: loader is nil", name)
	}

	r.mu.Lock()

	e, ok := r.entries[name]
	if !ok {
		e = &entry{}
		r.entries[name] = e
	}

	r.mu.Unlock()

	e.once.Do(func() {
		files, err := loader.Load(ctx)
		if err != nil {
			e.err = fmt.Errorf("load rpc descriptor %q from %s: %w", name, loader.Source(), err)

			return
		}

		e.files = files
	})

	return e.files, e.err
}
