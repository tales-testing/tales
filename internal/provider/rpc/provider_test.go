package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"

	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestProvider_ExecuteConnectSuccess(t *testing.T) {
	t.Parallel()

	srv, dir, _ := startConnectEchoServer(t)
	defer srv.Close()

	prov := New()
	defer func() { _ = prov.Close() }()

	cfg := connectConfig(t, dir, srv.URL)

	output, err := prov.Execute(context.Background(), provider.Input{
		Config: cfg,
		RPC: &provider.RPCExecution{
			Target:  "api",
			Service: "tales.providertest.v1.EchoService",
			Method:  "Echo",
			Message: map[string]cty.Value{"text": cty.StringVal("ping")},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if output.Response["status"].AsString() != "ok" {
		t.Errorf("status = %v", output.Response["status"])
	}

	msg := output.Response["message"]
	if !msg.Type().IsObjectType() {
		t.Fatalf("message = %v, want object", msg)
	}

	if got := msg.GetAttr("text").AsString(); got != "echo: ping" {
		t.Errorf("message.text = %q", got)
	}

	requestMsg := output.Request["message"]
	if !requestMsg.Type().IsObjectType() {
		t.Fatalf("request.message = %v, want object", requestMsg)
	}

	if got := requestMsg.GetAttr("text").AsString(); got != "ping" {
		t.Errorf("request.message.text = %q", got)
	}
}

func TestProvider_ExecuteResolvesDescriptorPathRelativeToProjectDir(t *testing.T) {
	t.Parallel()

	srv, descPath, _ := startConnectEchoServer(t)
	defer srv.Close()

	prov := New()
	defer func() { _ = prov.Close() }()

	cfg := connectConfig(t, filepath.Base(descPath), srv.URL)

	_, err := prov.Execute(context.Background(), provider.Input{
		Config: cfg,
		RPC: &provider.RPCExecution{
			Target:     "api",
			Service:    "tales.providertest.v1.EchoService",
			Method:     "Echo",
			Message:    map[string]cty.Value{"text": cty.StringVal("relative")},
			ProjectDir: filepath.Dir(descPath),
		},
	})
	if err != nil {
		t.Fatalf("Execute with relative descriptor path: %v", err)
	}
}

func TestProvider_ExecuteConnectErrorEnvelope(t *testing.T) {
	t.Parallel()

	srv, dir, _ := startConnectEchoServer(t)
	defer srv.Close()

	prov := New()
	defer func() { _ = prov.Close() }()

	cfg := connectConfig(t, dir, srv.URL)

	output, err := prov.Execute(context.Background(), provider.Input{
		Config: cfg,
		RPC: &provider.RPCExecution{
			Target:  "api",
			Service: "tales.providertest.v1.EchoService",
			Method:  "Fail",
			Message: map[string]cty.Value{"text": cty.StringVal("x")},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := output.Response["status"].AsString(); got != "not_found" {
		t.Errorf("status = %q", got)
	}

	errVal := output.Response["error"]
	if errVal.IsNull() {
		t.Fatal("error is null")
	}

	if !strings.Contains(errVal.GetAttr("message").AsString(), "not here") {
		t.Errorf("error.message = %q", errVal.GetAttr("message").AsString())
	}
}

func TestProvider_ExecuteWritesArtifacts(t *testing.T) {
	t.Parallel()

	srv, dir, _ := startConnectEchoServer(t)
	defer srv.Close()

	prov := New()
	defer func() { _ = prov.Close() }()

	artifacts := t.TempDir()

	cfg := connectConfig(t, dir, srv.URL)

	_, err := prov.Execute(context.Background(), provider.Input{
		Config: cfg,
		RPC: &provider.RPCExecution{
			Target:       "api",
			Service:      "tales.providertest.v1.EchoService",
			Method:       "Echo",
			Message:      map[string]cty.Value{"text": cty.StringVal("artifact")},
			ArtifactsDir: artifacts,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, name := range []string{"request.json", "response.json", "metadata.json"} {
		path := filepath.Join(artifacts, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("artifact %s missing: %v", name, err)
		}
	}

	metaBytes, err := os.ReadFile(filepath.Join(artifacts, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	metaStr := string(metaBytes)
	if !strings.Contains(metaStr, `"status": "ok"`) {
		t.Errorf("metadata.json missing status: %s", metaStr)
	}

	if !strings.Contains(metaStr, `"full_method": "/tales.providertest.v1.EchoService/Echo"`) {
		t.Errorf("metadata.json missing full_method: %s", metaStr)
	}
}

func TestProvider_ExecuteRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	prov := New()
	defer func() { _ = prov.Close() }()

	_, err := prov.Execute(context.Background(), provider.Input{
		Config: rpcConfig(map[string]cty.Value{
			"app": cty.ObjectVal(map[string]cty.Value{"path": cty.StringVal("/dev/null")}),
		}, map[string]cty.Value{}),
		RPC: &provider.RPCExecution{Target: "absent", Service: "x.S", Method: "M"},
	})
	if err == nil || !strings.Contains(err.Error(), `"absent" not found`) {
		t.Errorf("expected target-not-found error, got %v", err)
	}
}

func TestProvider_ExecuteRejectsStreamingMethod(t *testing.T) {
	t.Parallel()

	_, dir, _ := startConnectEchoServer(t)

	prov := New()
	defer func() { _ = prov.Close() }()

	_, err := prov.Execute(context.Background(), provider.Input{
		Config: connectConfig(t, dir, "http://unused"),
		RPC: &provider.RPCExecution{
			Target:  "api",
			Service: "tales.providertest.v1.EchoService",
			Method:  "StreamEcho",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "is streaming") {
		t.Errorf("expected streaming-rejected error, got %v", err)
	}
}

// startConnectEchoServer boots an httptest.Server speaking the Connect
// protocol for tales.providertest.v1.EchoService. It also writes a
// descriptor.bin to a temp dir and returns its path so the provider test
// can wire a descriptor source via config.rpc.descriptors.app.path.
func startConnectEchoServer(t *testing.T) (*httptest.Server, string, *protoregistry.Files) {
	t.Helper()

	dir := t.TempDir()
	descPath := filepath.Join(dir, "descriptor.bin")

	set := buildProviderTestSet()

	bytes, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal descriptor set: %v", err)
	}

	if err := os.WriteFile(descPath, bytes, 0o600); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("protodesc.NewFiles: %v", err)
	}

	inputDesc := messageByName(t, files, "tales.providertest.v1.EchoRequest")
	outputDesc := messageByName(t, files, "tales.providertest.v1.EchoResponse")

	types := &protoregistry.Types{}
	for _, md := range []protoreflect.MessageDescriptor{inputDesc, outputDesc} {
		_ = types.RegisterMessage(dynamicpb.NewMessageType(md))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tales.providertest.v1.EchoService/Echo":
			echoHandler(w, r, inputDesc, outputDesc, types)
		case "/tales.providertest.v1.EchoService/Fail":
			failHandler(w)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	return srv, descPath, files
}

func echoHandler(w http.ResponseWriter, r *http.Request, inputDesc, outputDesc protoreflect.MessageDescriptor, types *protoregistry.Types) {
	bodyBytes, _ := readAllAndClose(r)
	req := dynamicpb.NewMessage(inputDesc)

	if err := (protojson.UnmarshalOptions{Resolver: types}).Unmarshal(bodyBytes, req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	resp := dynamicpb.NewMessage(outputDesc)
	textField := outputDesc.Fields().ByName("text")
	srcField := inputDesc.Fields().ByName("text")
	resp.Set(textField, protoreflect.ValueOfString("echo: "+req.Get(srcField).String()))

	out, _ := (protojson.MarshalOptions{Resolver: types}).Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func failHandler(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"code":"not_found","message":"echo target not here"}`))
}

func readAllAndClose(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()

	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)

	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}

		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}

			return buf, err
		}
	}
}

func messageByName(t *testing.T, files *protoregistry.Files, name string) protoreflect.MessageDescriptor {
	t.Helper()

	desc, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s is not a message", name)
	}

	return md
}

func connectConfig(t *testing.T, descriptorPath, baseURL string) map[string]cty.Value {
	t.Helper()

	return rpcConfig(map[string]cty.Value{
		"app": cty.ObjectVal(map[string]cty.Value{
			"path": cty.StringVal(descriptorPath),
		}),
	}, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("connect"),
			"encoding":   cty.StringVal("json"),
			"base_url":   cty.StringVal(baseURL),
		}),
	})
}

// buildProviderTestSet constructs a tiny tales.providertest.v1 proto with
// EchoService.Echo (unary), EchoService.Fail (unary), EchoService.StreamEcho
// (server streaming, used to exercise the streaming-rejection branch).
func buildProviderTestSet() *descriptorpb.FileDescriptorSet {
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()

	textField := func(num int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String("text"),
			Number:   proto.Int32(num),
			Type:     stringType,
			JsonName: proto.String("text"),
		}
	}

	echoReq := &descriptorpb.DescriptorProto{Name: proto.String("EchoRequest"), Field: []*descriptorpb.FieldDescriptorProto{textField(1)}}
	echoResp := &descriptorpb.DescriptorProto{Name: proto.String("EchoResponse"), Field: []*descriptorpb.FieldDescriptorProto{textField(1)}}

	service := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("EchoService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{Name: proto.String("Echo"), InputType: proto.String(".tales.providertest.v1.EchoRequest"), OutputType: proto.String(".tales.providertest.v1.EchoResponse")},
			{Name: proto.String("Fail"), InputType: proto.String(".tales.providertest.v1.EchoRequest"), OutputType: proto.String(".tales.providertest.v1.EchoResponse")},
			{
				Name:            proto.String("StreamEcho"),
				InputType:       proto.String(".tales.providertest.v1.EchoRequest"),
				OutputType:      proto.String(".tales.providertest.v1.EchoResponse"),
				ServerStreaming: proto.Bool(true),
			},
		},
	}

	fd := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("tales/providertest/v1/echo.proto"),
		Package:     proto.String("tales.providertest.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{echoReq, echoResp},
		Service:     []*descriptorpb.ServiceDescriptorProto{service},
	}

	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}}
}
