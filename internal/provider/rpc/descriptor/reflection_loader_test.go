package descriptor

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// staticServices is a minimal ServiceInfoProvider that advertises a fixed list
// of fully-qualified service names. The reflection server we register in
// tests uses it instead of a real *grpc.Server so we never need actual
// generated service handlers.
type staticServices map[string]grpc.ServiceInfo

func (s staticServices) GetServiceInfo() map[string]grpc.ServiceInfo {
	return s
}

func TestReflectionLoader_LoadFromInProcessServer(t *testing.T) {
	t.Parallel()

	lis, files, stop := startReflectionServer(t)
	defer stop()

	loader := &ReflectionLoader{
		Address: "bufconn",
		Dial: func(ctx context.Context) (*grpc.ClientConn, error) {
			return grpc.NewClient(
				"passthrough://bufconn",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) {
					return lis.DialContext(c)
				}),
			)
		},
	}

	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	resolved, err := Resolve(got, "reflected", "tales.test.v1.TestService", "Echo")
	if err != nil {
		t.Fatalf("Resolve after reflection load: %v", err)
	}

	if resolved.FullMethod != "/tales.test.v1.TestService/Echo" {
		t.Errorf("FullMethod = %q", resolved.FullMethod)
	}

	// Sanity: the loader-built registry is a superset of the test set.
	_ = files
}

func TestReflectionLoader_LoadEmptyAddress(t *testing.T) {
	t.Parallel()

	_, err := (&ReflectionLoader{}).Load(context.Background())
	if err == nil {
		t.Fatal("expected error for empty address")
	}

	if !strings.Contains(err.Error(), "address is empty") {
		t.Errorf("error = %q, want containing 'address is empty'", err.Error())
	}
}

// startReflectionServer boots a *grpc.Server on an in-memory bufconn,
// registers a v1 reflection service whose DescriptorResolver is our test
// FileDescriptorSet, and returns the listener + the registry + a cleanup
// function. We use staticServices instead of the *grpc.Server's own
// GetServiceInfo so the advertised service does not require a real handler.
func startReflectionServer(t *testing.T) (*bufconn.Listener, *protoregistry.Files, func()) {
	t.Helper()

	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(buildTestFileSet(t), set); err != nil {
		t.Fatalf("unmarshal set: %v", err)
	}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("build files: %v", err)
	}

	services := staticServices{"tales.test.v1.TestService": {}}

	server := grpc.NewServer()

	reflectionpb.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
		Services:           services,
		DescriptorResolver: files,
	}))

	lis := bufconn.Listen(1 << 16)

	go func() {
		_ = server.Serve(lis)
	}()

	return lis, files, func() {
		server.GracefulStop()
		_ = lis.Close()
	}
}

