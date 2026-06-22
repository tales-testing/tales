package descriptor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// fakeLoader counts Load calls so the test can verify Registry caches the
// first result and never calls back into the loader for the same name.
type fakeLoader struct {
	source string
	calls  atomic.Int32
	err    error
	files  *protoregistry.Files
}

func (l *fakeLoader) Load(_ context.Context) (*protoregistry.Files, error) {
	l.calls.Add(1)

	return l.files, l.err
}

func (l *fakeLoader) Source() string { return l.source }

func TestRegistry_GetCachesSuccess(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)
	loader := &fakeLoader{source: "fake", files: files}
	reg := NewRegistry()

	got1, err := reg.Get(context.Background(), "app", loader)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}

	got2, err := reg.Get(context.Background(), "app", loader)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if got1 != got2 {
		t.Errorf("Registry did not return the cached files (got different pointers)")
	}

	if loader.calls.Load() != 1 {
		t.Errorf("loader.calls = %d, want 1", loader.calls.Load())
	}
}

func TestRegistry_GetCachesError(t *testing.T) {
	t.Parallel()

	loader := &fakeLoader{source: "fake", err: errors.New("boom")}
	reg := NewRegistry()

	_, err1 := reg.Get(context.Background(), "app", loader)
	_, err2 := reg.Get(context.Background(), "app", loader)

	if err1 == nil || err2 == nil {
		t.Fatalf("expected errors, got %v / %v", err1, err2)
	}

	if !strings.Contains(err1.Error(), `load rpc descriptor "app" from fake`) {
		t.Errorf("error message = %q, want containing %q", err1.Error(), `load rpc descriptor "app" from fake`)
	}

	if loader.calls.Load() != 1 {
		t.Errorf("loader.calls = %d, want 1 (errors must also be cached)", loader.calls.Load())
	}
}

func TestRegistry_GetSerializesConcurrentLoads(t *testing.T) {
	t.Parallel()

	loader := &fakeLoader{source: "fake", files: registryFromTestSet(t)}
	reg := NewRegistry()

	const callers = 32

	var wg sync.WaitGroup

	wg.Add(callers)

	for range callers {
		go func() {
			defer wg.Done()

			_, err := reg.Get(context.Background(), "app", loader)
			if err != nil {
				t.Errorf("Get: %v", err)
			}
		}()
	}

	wg.Wait()

	if got := loader.calls.Load(); got != 1 {
		t.Errorf("loader.calls = %d, want 1", got)
	}
}

func TestRegistry_GetIndependentNames(t *testing.T) {
	t.Parallel()

	loaderA := &fakeLoader{source: "a", files: registryFromTestSet(t)}
	loaderB := &fakeLoader{source: "b", files: registryFromTestSet(t)}
	reg := NewRegistry()

	if _, err := reg.Get(context.Background(), "a", loaderA); err != nil {
		t.Fatalf("Get a: %v", err)
	}

	if _, err := reg.Get(context.Background(), "b", loaderB); err != nil {
		t.Fatalf("Get b: %v", err)
	}

	if loaderA.calls.Load() != 1 || loaderB.calls.Load() != 1 {
		t.Errorf("calls: a=%d b=%d, want 1/1", loaderA.calls.Load(), loaderB.calls.Load())
	}
}

func TestRegistry_GetRejectsEmptyName(t *testing.T) {
	t.Parallel()

	if _, err := NewRegistry().Get(context.Background(), "", &fakeLoader{}); err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestRegistry_GetRejectsNilLoader(t *testing.T) {
	t.Parallel()

	if _, err := NewRegistry().Get(context.Background(), "app", nil); err == nil {
		t.Fatal("expected error for nil loader, got nil")
	}
}

// registryFromTestSet builds a *protoregistry.Files from the test descriptor
// set so cache tests have a real registry to share without going through the
// disk path.
func registryFromTestSet(t *testing.T) *protoregistry.Files {
	t.Helper()

	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(buildTestFileSet(t), set); err != nil {
		t.Fatalf("unmarshal test set: %v", err)
	}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("build files: %v", err)
	}

	return files
}
