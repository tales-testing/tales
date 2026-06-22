// rpc.go boots the Connect HTTP handler + native gRPC server with
// reflection that back the e2e/rpc/* Tales scenarios. Both servers share
// the same dynamic dispatch map keyed by full method name so the schema
// and the response shapes are defined once. The descriptor is built
// in-memory from the schema package (single source of truth shared with
// e2e/rpc/genbin/main.go) so the mockserver needs no disk read; the
// .tales scenarios load the matching descriptor.bin from disk.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/tales-testing/tales/e2e/rpc/schema"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// rpcGRPCAddr is the listen address of the gRPC server the e2e scenarios
// dial. Hardcoded so the rpc .tales fixtures can use a literal default
// (env-overridable via GRPC_ADDR in the scenarios themselves).
const rpcGRPCAddr = "127.0.0.1:50051"

// methodHandler is the in-process signature shared by Connect and gRPC
// dispatch. Implementations receive a populated dynamic request and return
// either a dynamic response or an error (mapped to the appropriate
// gRPC / Connect code by the transport-specific shims below).
type methodHandler func(req *dynamicpb.Message) (*dynamicpb.Message, error)

// rpcRegistry bundles the loaded descriptor types and the per-method
// handlers so both the Connect HTTP handler and the gRPC handler share
// one source of truth.
type rpcRegistry struct {
	files    *protoregistry.Files
	types    *protoregistry.Types
	handlers map[string]methodHandler
	inputs   map[string]protoreflect.MessageDescriptor
	outputs  map[string]protoreflect.MessageDescriptor
}

// installRPCHandlers mounts the Connect HTTP routes on the provided mux
// and starts the gRPC listener synchronously. It returns a closer that
// tears down the gRPC server at shutdown.
func installRPCHandlers(mux *http.ServeMux) func() {
	registry := buildRPCRegistry()

	for fullMethod := range registry.handlers {
		path := "/" + fullMethod
		mux.HandleFunc(path, registry.connectHandler(fullMethod))
	}

	stopGRPC := startGRPCServer(registry)

	return stopGRPC
}

// buildRPCRegistry builds the descriptor + type lookup tables and wires
// the in-process handler implementations (echo, fail).
func buildRPCRegistry() *rpcRegistry {
	files, err := protodesc.NewFiles(schema.BuildFileDescriptorSet())
	if err != nil {
		log.Fatalf("mockserver rpc: build files: %v", err)
	}

	types, err := buildRPCTypes(files)
	if err != nil {
		log.Fatalf("mockserver rpc: build types: %v", err)
	}

	echoIn := mustMessage(files, "tales.rpc.v1.EchoRequest")
	echoOut := mustMessage(files, "tales.rpc.v1.EchoResponse")
	failIn := mustMessage(files, "tales.rpc.v1.FailRequest")
	failOut := mustMessage(files, "tales.rpc.v1.FailResponse")

	echoMethod := "tales.rpc.v1.TestService/Echo"
	failMethod := "tales.rpc.v1.TestService/Fail"

	return &rpcRegistry{
		files: files,
		types: types,
		handlers: map[string]methodHandler{
			echoMethod: echoHandler(echoIn, echoOut),
			failMethod: failHandler(failOut),
		},
		inputs:  map[string]protoreflect.MessageDescriptor{echoMethod: echoIn, failMethod: failIn},
		outputs: map[string]protoreflect.MessageDescriptor{echoMethod: echoOut, failMethod: failOut},
	}
}

// echoHandler returns the request payload unchanged. Verifies that
// repeated and enum fields round-trip cleanly through the wire format.
// The request and response messages have identical field layouts in the
// e2e schema, so we re-marshal the request bytes into a fresh response
// message: this avoids dynamicpb's strict list type-identity check that
// would otherwise reject a List value bound to the request descriptor
// being Set on the response descriptor.
func echoHandler(_, out protoreflect.MessageDescriptor) methodHandler {
	return func(req *dynamicpb.Message) (*dynamicpb.Message, error) {
		wire, err := proto.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("echo: marshal request: %w", err)
		}

		resp := dynamicpb.NewMessage(out)
		if err := proto.Unmarshal(wire, resp); err != nil {
			return nil, fmt.Errorf("echo: unmarshal response: %w", err)
		}

		return resp, nil
	}
}

// failNotFoundMessage is the canonical message the Fail handler returns
// when the request carries no reason. Extracted to a constant because the
// scenario fixtures assert on it via contains() and the goconst linter
// otherwise flags the literal as duplicated.
const failNotFoundMessage = "not found"

// failHandler always returns a gRPC NotFound (Connect not_found) carrying
// the reason field of the request verbatim. Used by the rpc error
// envelope scenario.
func failHandler(out protoreflect.MessageDescriptor) methodHandler {
	return func(req *dynamicpb.Message) (*dynamicpb.Message, error) {
		reason := ""

		if fd := req.Descriptor().Fields().ByName("reason"); fd != nil {
			reason = req.Get(fd).String()
		}

		msg := failNotFoundMessage
		if reason != "" {
			msg = failNotFoundMessage + ": " + reason
		}

		_ = out

		return nil, status.Error(codes.NotFound, msg)
	}
}

// connectHandler returns an http.HandlerFunc speaking the Connect
// protocol for one unary method. Content-Type drives the encoding choice
// (application/json or application/proto); errors are returned as the
// Connect JSON envelope.
func (r *rpcRegistry) connectHandler(fullMethod string) http.HandlerFunc {
	handler := r.handlers[fullMethod]
	inputDesc := r.inputs[fullMethod]
	outputDesc := r.outputs[fullMethod]

	return func(w http.ResponseWriter, req *http.Request) {
		defer func() { _ = req.Body.Close() }()

		body, err := io.ReadAll(req.Body)
		if err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", "read body: "+err.Error())

			return
		}

		contentType := req.Header.Get("Content-Type")

		reqMsg := dynamicpb.NewMessage(inputDesc)

		if err := r.unmarshalRequest(contentType, body, reqMsg); err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())

			return
		}

		respMsg, runErr := handler(reqMsg)
		if runErr != nil {
			writeConnectErrorFromGRPC(w, runErr)

			return
		}

		out, marshalErr := r.marshalResponse(contentType, respMsg)
		if marshalErr != nil {
			writeConnectError(w, http.StatusInternalServerError, "internal", marshalErr.Error())

			return
		}

		w.Header().Set("Content-Type", responseContentType(contentType))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)

		_ = outputDesc // referenced for documentation; output descriptor is implicit on respMsg.
	}
}

func (r *rpcRegistry) unmarshalRequest(contentType string, body []byte, msg *dynamicpb.Message) error {
	if strings.Contains(contentType, "proto") {
		if err := proto.Unmarshal(body, msg); err != nil {
			return fmt.Errorf("proto unmarshal: %w", err)
		}

		return nil
	}

	if err := (protojson.UnmarshalOptions{Resolver: r.types}).Unmarshal(body, msg); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}

	return nil
}

func (r *rpcRegistry) marshalResponse(contentType string, msg *dynamicpb.Message) ([]byte, error) {
	if strings.Contains(contentType, "proto") {
		out, err := proto.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("proto marshal: %w", err)
		}

		return out, nil
	}

	out, err := (protojson.MarshalOptions{Resolver: r.types}).Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}

	return out, nil
}

// startGRPCServer binds rpcGRPCAddr synchronously, registers the
// reflection service against the descriptor registry (so the rpc
// reflection scenario sees the test service), wires every method through
// a single grpc.ServiceDesc, and serves in a background goroutine.
func startGRPCServer(registry *rpcRegistry) func() {
	lc := &net.ListenConfig{}

	listener, err := lc.Listen(context.Background(), "tcp", rpcGRPCAddr)
	if err != nil {
		log.Printf("mockserver rpc: gRPC listen %s failed: %v (gRPC scenarios will skip)", rpcGRPCAddr, err)

		return func() {}
	}

	server := grpc.NewServer()

	reflectionpb.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
		Services:           grpcServiceInfo{schema.FullService},
		DescriptorResolver: registry.files,
	}))

	registerDynamicService(server, registry)

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("mockserver rpc: gRPC server exited: %v", err)
		}
	}()

	return func() {
		server.GracefulStop()

		_ = listener.Close()
	}
}

// registerDynamicService builds a grpc.ServiceDesc whose handlers wrap
// the in-process registry. The default grpc-go proto codec handles
// *dynamicpb.Message marshaling directly because dynamicpb satisfies
// proto.Message.
func registerDynamicService(server *grpc.Server, registry *rpcRegistry) {
	methods := make([]grpc.MethodDesc, 0, len(registry.handlers))

	for fullMethod := range registry.handlers {
		_, methodName := splitFullMethod(fullMethod)

		methods = append(methods, grpc.MethodDesc{
			MethodName: methodName,
			Handler:    grpcMethodHandler(registry, fullMethod),
		})
	}

	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: schema.FullService,
		HandlerType: (*any)(nil),
		Methods:     methods,
	}, struct{}{})
}

func grpcMethodHandler(registry *rpcRegistry, fullMethod string) grpcUnaryHandler {
	handler := registry.handlers[fullMethod]
	inputDesc := registry.inputs[fullMethod]

	return func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
		_ = ctx

		req := dynamicpb.NewMessage(inputDesc)
		if err := dec(req); err != nil {
			return nil, err //nolint:wrapcheck // grpc decoder errors propagate verbatim.
		}

		return handler(req)
	}
}

// grpcUnaryHandler matches the signature grpc-go expects for MethodDesc.Handler.
type grpcUnaryHandler = func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error)

// grpcServiceInfo is the ServiceInfoProvider the reflection v1 server
// uses to enumerate advertised services. It returns a single hardcoded
// list so reflection works even before any other service registers.
type grpcServiceInfo []string

func (g grpcServiceInfo) GetServiceInfo() map[string]grpc.ServiceInfo {
	out := make(map[string]grpc.ServiceInfo, len(g))
	for _, name := range g {
		out[name] = grpc.ServiceInfo{}
	}

	return out
}

// writeConnectError emits the Connect error envelope JSON with the
// appropriate HTTP status code. The body shape matches the V1 spec:
//
//	{ "code": "<canonical>", "message": "<text>" }
func writeConnectError(w http.ResponseWriter, httpStatus int, code, message string) {
	body, _ := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_, _ = w.Write(body)
}

// writeConnectErrorFromGRPC converts a gRPC status error returned by an
// in-process handler into the equivalent Connect HTTP envelope so the
// Connect transport sees the same canonical code.
func writeConnectErrorFromGRPC(w http.ResponseWriter, runErr error) {
	st, ok := status.FromError(runErr)
	if !ok {
		writeConnectError(w, http.StatusInternalServerError, "internal", runErr.Error())

		return
	}

	httpStatus := connectHTTPStatusForGRPCCode(st.Code())
	writeConnectError(w, httpStatus, grpcCodeToConnect(st.Code()), st.Message())
}

// connectCodeUnknown is the canonical fallback name for unmapped gRPC
// codes, matching the Tales rpc/status.StatusUnknown constant.
const connectCodeUnknown = "unknown"

// grpcCodeToConnect maps a gRPC code enum value to its canonical Connect
// lowercase-snake-case string (matching the rpc/status package).
func grpcCodeToConnect(c codes.Code) string {
	switch c {
	case codes.OK:
		return "ok"
	case codes.Canceled:
		return "cancelled"
	case codes.Unknown:
		return connectCodeUnknown
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	case codes.NotFound:
		return "not_found"
	case codes.AlreadyExists:
		return "already_exists"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	case codes.FailedPrecondition:
		return "failed_precondition"
	case codes.Aborted:
		return "aborted"
	case codes.OutOfRange:
		return "out_of_range"
	case codes.Unimplemented:
		return "unimplemented"
	case codes.Internal:
		return "internal"
	case codes.Unavailable:
		return "unavailable"
	case codes.DataLoss:
		return "data_loss"
	case codes.Unauthenticated:
		return "unauthenticated"
	default:
		return connectCodeUnknown
	}
}

// connectHTTPStatusForGRPCCode mirrors the Connect spec's
// http-status-from-gRPC-code mapping so the Connect transport's
// status-from-http-status inverse produces the right canonical name.
func connectHTTPStatusForGRPCCode(c codes.Code) int {
	switch c {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return http.StatusRequestTimeout
	case codes.Unknown, codes.Internal, codes.DataLoss:
		return http.StatusInternalServerError
	case codes.InvalidArgument, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound, codes.Unimplemented:
		return http.StatusNotFound
	case codes.DeadlineExceeded:
		return http.StatusRequestTimeout
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.FailedPrecondition:
		return http.StatusPreconditionFailed
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func responseContentType(requestContentType string) string {
	if strings.Contains(requestContentType, "proto") {
		return "application/proto"
	}

	return "application/json"
}

func splitFullMethod(fullMethod string) (string, string) {
	idx := strings.LastIndex(fullMethod, "/")
	if idx < 0 {
		return fullMethod, ""
	}

	return fullMethod[:idx], fullMethod[idx+1:]
}

func mustMessage(files *protoregistry.Files, name string) protoreflect.MessageDescriptor {
	desc, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		log.Fatalf("mockserver rpc: find %s: %v", name, err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		log.Fatalf("mockserver rpc: %s is not a message", name)
	}

	return md
}

// buildRPCTypes registers every message / enum in the file set as a
// dynamicpb type so protojson resolves them when marshaling responses.
func buildRPCTypes(files *protoregistry.Files) (*protoregistry.Types, error) {
	types := &protoregistry.Types{}

	var rangeErr error

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if err := registerRPCMessages(types, fd.Messages()); err != nil {
			rangeErr = err

			return false
		}

		if err := registerRPCEnums(types, fd.Enums()); err != nil {
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

func registerRPCMessages(types *protoregistry.Types, msgs protoreflect.MessageDescriptors) error {
	for i := range msgs.Len() {
		md := msgs.Get(i)
		if err := types.RegisterMessage(dynamicpb.NewMessageType(md)); err != nil {
			return fmt.Errorf("register message %s: %w", md.FullName(), err)
		}

		if err := registerRPCMessages(types, md.Messages()); err != nil {
			return err
		}
	}

	return nil
}

func registerRPCEnums(types *protoregistry.Types, enums protoreflect.EnumDescriptors) error {
	for i := range enums.Len() {
		ed := enums.Get(i)
		if err := types.RegisterEnum(dynamicpb.NewEnumType(ed)); err != nil {
			return fmt.Errorf("register enum %s: %w", ed.FullName(), err)
		}
	}

	return nil
}

// Keep bytes/json imported even if a future refactor removes their last
// use; the rpc.go file fundamentally serializes payloads through both.
var _ = bytes.NewBuffer
