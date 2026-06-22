package rpc

import (
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// rpcConfig builds the {"rpc": { descriptors: ..., targets: ...}} value the
// resolvers traverse. Both subblocks are optional.
func rpcConfig(descriptors, targets map[string]cty.Value) map[string]cty.Value {
	inner := map[string]cty.Value{}

	if descriptors != nil {
		inner["descriptors"] = cty.ObjectVal(descriptors)
	}

	if targets != nil {
		inner["targets"] = cty.ObjectVal(targets)
	}

	if len(inner) == 0 {
		return map[string]cty.Value{}
	}

	return map[string]cty.Value{"rpc": cty.ObjectVal(inner)}
}

func TestResolveDescriptor_PathSuccess(t *testing.T) {
	t.Parallel()

	config := rpcConfig(map[string]cty.Value{
		"app": cty.ObjectVal(map[string]cty.Value{
			"path": cty.StringVal("/abs/descriptor.bin"),
		}),
	}, nil)

	got, err := ResolveDescriptor(config, "app")
	if err != nil {
		t.Fatalf("ResolveDescriptor: %v", err)
	}

	if got.Path != "/abs/descriptor.bin" {
		t.Errorf("Path = %q", got.Path)
	}

	if got.Reflection != nil {
		t.Errorf("Reflection unexpectedly populated")
	}
}

func TestResolveDescriptor_ReflectionSuccess(t *testing.T) {
	t.Parallel()

	config := rpcConfig(map[string]cty.Value{
		"reflected": cty.ObjectVal(map[string]cty.Value{
			"reflection": cty.ObjectVal(map[string]cty.Value{
				"address":   cty.StringVal("127.0.0.1:50051"),
				"plaintext": cty.True,
				"headers": cty.ObjectVal(map[string]cty.Value{
					"authorization": cty.StringVal("Bearer x"),
				}),
			}),
		}),
	}, nil)

	got, err := ResolveDescriptor(config, "reflected")
	if err != nil {
		t.Fatalf("ResolveDescriptor: %v", err)
	}

	if got.Path != "" {
		t.Errorf("Path should be empty")
	}

	if got.Reflection == nil {
		t.Fatal("Reflection is nil")
	}

	if got.Reflection.Address != "127.0.0.1:50051" || !got.Reflection.Plaintext {
		t.Errorf("Reflection = %+v", got.Reflection)
	}

	if got.Reflection.Headers["authorization"] != "Bearer x" {
		t.Errorf("Reflection.Headers = %v", got.Reflection.Headers)
	}
}

func TestResolveDescriptor_ExactlyOneSource(t *testing.T) {
	t.Parallel()

	both := rpcConfig(map[string]cty.Value{
		"x": cty.ObjectVal(map[string]cty.Value{
			"path": cty.StringVal("/p"),
			"reflection": cty.ObjectVal(map[string]cty.Value{
				"address": cty.StringVal("a:1"),
			}),
		}),
	}, nil)

	if _, err := ResolveDescriptor(both, "x"); err == nil || !strings.Contains(err.Error(), "exactly one of path or reflection") {
		t.Errorf("expected exactly-one-of error, got %v", err)
	}

	neither := rpcConfig(map[string]cty.Value{"x": cty.EmptyObjectVal}, nil)
	if _, err := ResolveDescriptor(neither, "x"); err == nil || !strings.Contains(err.Error(), "must define either path or reflection") {
		t.Errorf("expected must-define error, got %v", err)
	}
}

func TestResolveDescriptor_Missing(t *testing.T) {
	t.Parallel()

	if _, err := ResolveDescriptor(rpcConfig(nil, nil), "absent"); err == nil {
		t.Fatal("expected error when config.rpc is missing")
	}

	if _, err := ResolveDescriptor(rpcConfig(map[string]cty.Value{"other": cty.EmptyObjectVal}, nil), "absent"); err == nil {
		t.Fatal("expected error for absent name")
	}
}

func TestResolveTarget_ConnectSuccess(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("connect"),
			"encoding":   cty.StringVal("json"),
			"base_url":   cty.StringVal("http://localhost:8080"),
			"headers": cty.ObjectVal(map[string]cty.Value{
				"X-Trace": cty.StringVal("abc"),
			}),
		}),
	})

	got, err := ResolveTarget(config, "api")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}

	if got.Protocol != ProtocolConnect || got.BaseURL != "http://localhost:8080" || got.Encoding != EncodingJSON {
		t.Errorf("target = %+v", got)
	}

	if got.Headers["X-Trace"] != "abc" {
		t.Errorf("headers = %v", got.Headers)
	}
}

func TestResolveTarget_ConnectEncodingDefault(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("connect"),
			"base_url":   cty.StringVal("http://localhost:8080"),
		}),
	})

	got, err := ResolveTarget(config, "api")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}

	if got.Encoding != EncodingJSON {
		t.Errorf("default encoding for connect = %q, want %q", got.Encoding, EncodingJSON)
	}
}

func TestResolveTarget_GRPCSuccess(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"backend": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("grpc"),
			"address":    cty.StringVal("127.0.0.1:50051"),
			"plaintext":  cty.True,
		}),
	})

	got, err := ResolveTarget(config, "backend")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}

	if got.Protocol != ProtocolGRPC || got.Address != "127.0.0.1:50051" || !got.Plaintext {
		t.Errorf("target = %+v", got)
	}

	if got.Encoding != EncodingProto {
		t.Errorf("default encoding for grpc = %q, want %q", got.Encoding, EncodingProto)
	}
}

func TestResolveTarget_GRPCRejectsJSONEncoding(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"backend": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("grpc"),
			"address":    cty.StringVal("a:1"),
			"encoding":   cty.StringVal("json"),
		}),
	})

	_, err := ResolveTarget(config, "backend")
	if err == nil || !strings.Contains(err.Error(), "gRPC protocol does not support encoding") {
		t.Errorf("expected gRPC-JSON rejection, got %v", err)
	}
}

func TestResolveTarget_MissingProtocol(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
		}),
	})

	if _, err := ResolveTarget(config, "api"); err == nil {
		t.Fatal("expected error for missing protocol")
	}
}

func TestResolveTarget_UnknownProtocol(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("smtp"),
		}),
	})

	_, err := ResolveTarget(config, "api")
	if err == nil || !strings.Contains(err.Error(), `must be "connect" or "grpc"`) {
		t.Errorf("expected protocol validation, got %v", err)
	}
}

func TestResolveTarget_ConnectRequiresBaseURL(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("connect"),
		}),
	})

	if _, err := ResolveTarget(config, "api"); err == nil {
		t.Fatal("expected error for connect target missing base_url")
	}
}

func TestResolveTarget_GRPCRequiresAddress(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"api": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("grpc"),
		}),
	})

	if _, err := ResolveTarget(config, "api"); err == nil {
		t.Fatal("expected error for grpc target missing address")
	}
}

func TestResolveTarget_TLSBlock(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"backend": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("grpc"),
			"address":    cty.StringVal("a:1"),
			"tls": cty.ObjectVal(map[string]cty.Value{
				"ca_file":              cty.StringVal("/ca.pem"),
				"cert_file":            cty.StringVal("/cert.pem"),
				"key_file":             cty.StringVal("/key.pem"),
				"server_name":          cty.StringVal("svc.example.com"),
				"insecure_skip_verify": cty.False,
			}),
		}),
	})

	got, err := ResolveTarget(config, "backend")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}

	if got.TLS == nil {
		t.Fatal("TLS missing")
	}

	if got.TLS.CAFile != "/ca.pem" || got.TLS.CertFile != "/cert.pem" || got.TLS.KeyFile != "/key.pem" {
		t.Errorf("TLS files = %+v", got.TLS)
	}

	if got.TLS.ServerName != "svc.example.com" || got.TLS.InsecureSkipVerify {
		t.Errorf("TLS extra = %+v", got.TLS)
	}
}

func TestResolveTarget_TLSCertWithoutKey(t *testing.T) {
	t.Parallel()

	config := rpcConfig(nil, map[string]cty.Value{
		"backend": cty.ObjectVal(map[string]cty.Value{
			"descriptor": cty.StringVal("app"),
			"protocol":   cty.StringVal("grpc"),
			"address":    cty.StringVal("a:1"),
			"tls": cty.ObjectVal(map[string]cty.Value{
				"cert_file": cty.StringVal("/cert.pem"),
			}),
		}),
	})

	_, err := ResolveTarget(config, "backend")
	if err == nil || !strings.Contains(err.Error(), "cert_file and tls.key_file must be set together") {
		t.Errorf("expected cert/key pairing error, got %v", err)
	}
}
