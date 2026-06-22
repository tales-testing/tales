package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rpcstatus "github.com/tales-testing/tales/internal/provider/rpc/status"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestConnectClient_InvokeJSONSuccess(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	var capturedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		if r.URL.Path != "/tales.transport.v1.EchoService/Echo" {
			http.Error(w, "wrong path", http.StatusNotFound)

			return
		}

		body, _ := io.ReadAll(r.Body)
		req := dynamicpb.NewMessage(inputDesc)

		if err := (protojson.UnmarshalOptions{Resolver: types}).Unmarshal(body, req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		resp := dynamicpb.NewMessage(outputDesc)
		setMessageStringField(resp, "text", "got: "+getDynamicStringField(req, "text"))

		out, _ := (protojson.MarshalOptions{Resolver: types}).Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	defer srv.Close()

	client := NewConnectClient(ConnectConfig{
		BaseURL:        srv.URL,
		Encoding:       "json",
		DefaultHeaders: map[string]string{"X-Trace": "trace-1"},
		Types:          types,
	})
	defer func() { _ = client.Close() }()

	reqMsg := dynamicpb.NewMessage(inputDesc)
	setMessageStringField(reqMsg, "text", "hi")

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    reqMsg,
		Types:      types,
		Headers:    map[string]string{"X-Override": "step-1"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.Status != rpcstatus.StatusOK {
		t.Errorf("status = %q", res.Status)
	}

	if got := getDynamicStringField(res.Message, "text"); got != "got: hi" {
		t.Errorf("response.text = %q", got)
	}

	if capturedHeaders.Get("Connect-Protocol-Version") != "1" {
		t.Errorf("Connect-Protocol-Version header missing: %v", capturedHeaders)
	}

	if capturedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", capturedHeaders.Get("Content-Type"))
	}

	if capturedHeaders.Get("X-Trace") != "trace-1" {
		t.Errorf("X-Trace = %q (default header missing)", capturedHeaders.Get("X-Trace"))
	}

	if capturedHeaders.Get("X-Override") != "step-1" {
		t.Errorf("X-Override = %q (step header missing)", capturedHeaders.Get("X-Override"))
	}
}

func TestConnectClient_InvokeProtoSuccess(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/proto" {
			http.Error(w, "wrong content-type: "+ct, http.StatusBadRequest)

			return
		}

		body, _ := io.ReadAll(r.Body)
		req := dynamicpb.NewMessage(inputDesc)

		if err := proto.Unmarshal(body, req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		resp := dynamicpb.NewMessage(outputDesc)
		setMessageStringField(resp, "text", "binary:"+getDynamicStringField(req, "text"))

		out, _ := proto.Marshal(resp)
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(out)
	}))
	defer srv.Close()

	client := NewConnectClient(ConnectConfig{
		BaseURL:  srv.URL,
		Encoding: "proto",
		Types:    types,
	})
	defer func() { _ = client.Close() }()

	reqMsg := dynamicpb.NewMessage(inputDesc)
	setMessageStringField(reqMsg, "text", "tales")

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    reqMsg,
		Types:      types,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := getDynamicStringField(res.Message, "text"); got != "binary:tales" {
		t.Errorf("response.text = %q", got)
	}
}

func TestConnectClient_InvokeErrorEnvelope(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		envelope := map[string]any{
			"code":    "invalid_argument",
			"message": "text is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	client := NewConnectClient(ConnectConfig{
		BaseURL:  srv.URL,
		Encoding: "json",
		Types:    types,
	})
	defer func() { _ = client.Close() }()

	reqMsg := dynamicpb.NewMessage(inputDesc)

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    reqMsg,
		Types:      types,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.Status != rpcstatus.StatusInvalidArgument {
		t.Errorf("status = %q", res.Status)
	}

	if res.Error == nil || res.Error.Code != rpcstatus.StatusInvalidArgument {
		t.Errorf("error = %+v", res.Error)
	}

	if !strings.Contains(res.Error.Message, "text is required") {
		t.Errorf("error.message = %q", res.Error.Message)
	}
}

func TestConnectClient_MasksAuthorizationOnResponse(t *testing.T) {
	t.Parallel()

	_, types, inputDesc, outputDesc := buildTransportFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Authorization", "Bearer top-secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewConnectClient(ConnectConfig{
		BaseURL:  srv.URL,
		Encoding: "json",
		Types:    types,
	})
	defer func() { _ = client.Close() }()

	res, err := client.Invoke(context.Background(), Call{
		FullMethod: "/tales.transport.v1.EchoService/Echo",
		Output:     outputDesc,
		Request:    dynamicpb.NewMessage(inputDesc),
		Types:      types,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := res.Headers["Authorization"]; len(got) != 1 || got[0] != "***" {
		t.Errorf("Authorization not masked: %v", res.Headers)
	}
}
