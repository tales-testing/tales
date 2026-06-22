package transport

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	rpcstatus "github.com/tales-testing/tales/internal/provider/rpc/status"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestGRPCClient_InvokeSuccess(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	lis, stop := startEchoGRPCServer(t, inputDesc, outputDesc, false, nil)
	defer stop()

	client := newBufconnGRPCClient(t, lis)
	defer func() { _ = client.Close() }()

	req := dynamicpb.NewMessage(inputDesc)
	setStringField(t, req, "text", "hello")

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    req,
		Types:      types,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.Status != rpcstatus.StatusOK {
		t.Errorf("status = %q, want ok", res.Status)
	}

	if got := getStringField(t, res.Message, "text"); got != "hello" {
		t.Errorf("response.text = %q", got)
	}
}

func TestGRPCClient_InvokeReturnsErrorStatus(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	failErr := status.Error(codes.NotFound, "echo target not found")

	lis, stop := startEchoGRPCServer(t, inputDesc, outputDesc, false, failErr)
	defer stop()

	client := newBufconnGRPCClient(t, lis)
	defer func() { _ = client.Close() }()

	req := dynamicpb.NewMessage(inputDesc)
	setStringField(t, req, "text", "x")

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    req,
		Types:      types,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.Status != rpcstatus.StatusNotFound {
		t.Errorf("status = %q, want not_found", res.Status)
	}

	if res.Error == nil || !strings.Contains(res.Error.Message, "echo target not found") {
		t.Errorf("error = %+v", res.Error)
	}
}

func TestGRPCClient_InvokePropagatesMetadata(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	var seenMD metadata.MD

	lis, stop := startEchoGRPCServer(t, inputDesc, outputDesc, true, nil, func(ctx context.Context) {
		md, _ := metadata.FromIncomingContext(ctx)
		seenMD = md
	})
	defer stop()

	cfg := GRPCConfig{
		Address:         "bufconn",
		Plaintext:       true,
		DefaultMetadata: map[string]string{"X-Default": "from-target"},
		Dial: func(ctx context.Context) (*grpc.ClientConn, error) {
			return bufconnDial(ctx, lis)
		},
	}

	client := NewGRPCClient(cfg)
	defer func() { _ = client.Close() }()

	req := dynamicpb.NewMessage(inputDesc)
	setStringField(t, req, "text", "x")

	_, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    req,
		Types:      types,
		Metadata:   map[string]string{"X-Override": "from-step"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := seenMD.Get("x-default"); len(got) != 1 || got[0] != "from-target" {
		t.Errorf("default metadata not propagated: %v", seenMD)
	}

	if got := seenMD.Get("x-override"); len(got) != 1 || got[0] != "from-step" {
		t.Errorf("override metadata not propagated: %v", seenMD)
	}
}

func TestGRPCClient_InvokeRejectsEmptyAddress(t *testing.T) {
	t.Parallel()

	client := NewGRPCClient(GRPCConfig{Plaintext: true})

	_, err := client.Invoke(context.Background(), Call{
		FullMethod: "/svc/m",
		Output:     nil,
		Request:    dynamicpb.NewMessage(nil),
	})
	if err == nil {
		t.Fatal("expected error for empty address / nil descriptor")
	}
}

// startEchoGRPCServer boots a bufconn-backed grpc.Server that handles
// /tales.transport.v1.EchoService/Echo. If failErr is non-nil the handler
// returns it; otherwise it echoes the request's text field back. The optional
// before callback fires inside the handler with the incoming context so the
// test can capture metadata.
func startEchoGRPCServer(t *testing.T, inputDesc, outputDesc protoreflect.MessageDescriptor, _ bool, failErr error, before ...func(context.Context)) (*bufconn.Listener, func()) {
	t.Helper()

	lis := bufconn.Listen(1 << 16)
	server := grpc.NewServer()

	desc := grpc.ServiceDesc{
		ServiceName: "tales.transport.v1.EchoService",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Echo",
				Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
					for _, hook := range before {
						hook(ctx)
					}

					req := dynamicpb.NewMessage(inputDesc)
					if err := dec(req); err != nil {
						return nil, err
					}

					if failErr != nil {
						return nil, failErr
					}

					resp := dynamicpb.NewMessage(outputDesc)
					setMessageStringField(resp, "text", getDynamicStringField(req, "text"))

					return resp, nil
				},
			},
		},
	}

	server.RegisterService(&desc, struct{}{})

	go func() { _ = server.Serve(lis) }()

	return lis, func() {
		server.GracefulStop()
		_ = lis.Close()
	}
}

func newBufconnGRPCClient(t *testing.T, lis *bufconn.Listener) *GRPCClient {
	t.Helper()

	return NewGRPCClient(GRPCConfig{
		Address:   "bufconn",
		Plaintext: true,
		Dial: func(ctx context.Context) (*grpc.ClientConn, error) {
			return bufconnDial(ctx, lis)
		},
	})
}

func bufconnDial(_ context.Context, lis *bufconn.Listener) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) {
			deadline, ok := c.Deadline()
			if !ok {
				deadline = time.Now().Add(5 * time.Second)
			}

			conn, err := lis.DialContext(c)
			if err != nil {
				return nil, err
			}

			_ = conn.SetDeadline(deadline)

			return conn, nil
		}),
	)
}

func setStringField(t *testing.T, msg *dynamicpb.Message, name, value string) {
	t.Helper()

	setMessageStringField(msg, name, value)
}

func getStringField(t *testing.T, msg *dynamicpb.Message, name string) string {
	t.Helper()

	return getDynamicStringField(msg, name)
}

func setMessageStringField(msg *dynamicpb.Message, name, value string) {
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return
	}

	msg.Set(fd, protoreflect.ValueOfString(value))
}

func getDynamicStringField(msg *dynamicpb.Message, name string) string {
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return ""
	}

	return msg.Get(fd).String()
}
