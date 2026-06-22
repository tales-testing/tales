package descriptor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ReflectionLoader fetches descriptors from a gRPC server's reflection v1
// service. The loader does not cache anything itself; the Registry above
// memoises the resulting *protoregistry.Files per descriptor name.
//
// Reflection v1alpha is intentionally NOT supported in V1: every modern
// grpc-go server exposes v1 by default; legacy v1alpha-only servers must
// upgrade or ship a descriptor.bin instead.
type ReflectionLoader struct {
	// Address is the gRPC endpoint (host:port) of the target server.
	Address string
	// Plaintext disables TLS. When true, TLS is ignored.
	Plaintext bool
	// TLS is applied when Plaintext is false; nil means use system roots.
	TLS *tls.Config
	// Headers populate the per-call gRPC metadata sent on the reflection
	// stream (e.g. an Authorization bearer). Keys are lowercased on the
	// wire by grpc-go.
	Headers map[string]string
	// Dial overrides the dial step for tests; production leaves it nil and
	// the loader uses grpc.NewClient.
	Dial func(ctx context.Context) (*grpc.ClientConn, error)
}

// Load opens a gRPC connection, walks the reflection service to fetch every
// known file descriptor, and builds a registry. The connection is closed on
// return. Errors do not include the metadata header values.
func (l *ReflectionLoader) Load(ctx context.Context) (*protoregistry.Files, error) {
	if l.Address == "" && l.Dial == nil {
		return nil, fmt.Errorf("reflection loader: address is empty")
	}

	conn, err := l.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection dial: %w", err)
	}

	defer func() { _ = conn.Close() }()

	if len(l.Headers) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(l.Headers))
	}

	client := reflectionpb.NewServerReflectionClient(conn)

	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reflection stream: %w", err)
	}

	services, err := listServices(stream)
	if err != nil {
		return nil, err
	}

	set, err := collectFiles(stream, services)
	if err != nil {
		return nil, err
	}

	_ = stream.CloseSend()

	return buildFiles(set)
}

// Source returns the reflection endpoint used in error messages.
func (l *ReflectionLoader) Source() string {
	return "reflection at " + l.Address
}

func (l *ReflectionLoader) dial(ctx context.Context) (*grpc.ClientConn, error) {
	if l.Dial != nil {
		return l.Dial(ctx)
	}

	var opts []grpc.DialOption

	if l.Plaintext {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(l.TLS)))
	}

	conn, err := grpc.NewClient(l.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", l.Address, err)
	}

	return conn, nil
}

// listServices issues a ListServices request and returns every advertised
// service name. The reflection service itself is excluded so we never try to
// recurse into it.
func listServices(stream reflectionpb.ServerReflection_ServerReflectionInfoClient) ([]string, error) {
	req := &reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}

	if err := stream.Send(req); err != nil {
		return nil, fmt.Errorf("reflection list services send: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("reflection list services recv: %w", err)
	}

	listResp := resp.GetListServicesResponse()
	if listResp == nil {
		if errResp := resp.GetErrorResponse(); errResp != nil {
			return nil, fmt.Errorf("reflection list services error: %s", errResp.GetErrorMessage())
		}

		return nil, fmt.Errorf("reflection list services: empty response")
	}

	services := make([]string, 0, len(listResp.Service))

	for _, svc := range listResp.Service {
		name := svc.GetName()
		if name == "" || name == "grpc.reflection.v1.ServerReflection" || name == "grpc.reflection.v1alpha.ServerReflection" {
			continue
		}

		services = append(services, name)
	}

	sort.Strings(services)

	return services, nil
}

// collectFiles fetches the FileContainingSymbol payload for every service
// and merges the resulting file descriptors into a single FileDescriptorSet.
// Files are deduplicated by name; reflection servers may legitimately return
// the same file twice when it backs multiple services. Order is stabilized by
// service name so the resulting set is reproducible.
func collectFiles(stream reflectionpb.ServerReflection_ServerReflectionInfoClient, services []string) (*descriptorpb.FileDescriptorSet, error) {
	seen := map[string]struct{}{}
	set := &descriptorpb.FileDescriptorSet{}

	for _, svc := range services {
		req := &reflectionpb.ServerReflectionRequest{
			MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: svc},
		}

		if err := stream.Send(req); err != nil {
			return nil, fmt.Errorf("reflection FileContainingSymbol send %q: %w", svc, err)
		}

		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("reflection stream closed before FileContainingSymbol %q", svc)
			}

			return nil, fmt.Errorf("reflection FileContainingSymbol recv %q: %w", svc, err)
		}

		fdResp := resp.GetFileDescriptorResponse()
		if fdResp == nil {
			if errResp := resp.GetErrorResponse(); errResp != nil {
				return nil, fmt.Errorf("reflection FileContainingSymbol %q: %s", svc, errResp.GetErrorMessage())
			}

			return nil, fmt.Errorf("reflection FileContainingSymbol %q: empty response", svc)
		}

		for _, raw := range fdResp.FileDescriptorProto {
			file := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(raw, file); err != nil {
				return nil, fmt.Errorf("reflection FileContainingSymbol %q: invalid file descriptor: %w", svc, err)
			}

			name := file.GetName()
			if _, ok := seen[name]; ok {
				continue
			}

			seen[name] = struct{}{}

			set.File = append(set.File, file)
		}
	}

	if len(set.File) == 0 {
		return nil, fmt.Errorf("reflection returned no file descriptors")
	}

	return set, nil
}
