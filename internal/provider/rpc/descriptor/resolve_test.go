package descriptor

import (
	"strings"
	"testing"
)

func TestResolve_Success(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)

	resolved, err := Resolve(files, "app", "tales.test.v1.TestService", "Echo")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolved.FullMethod != "/tales.test.v1.TestService/Echo" {
		t.Errorf("FullMethod = %q, want %q", resolved.FullMethod, "/tales.test.v1.TestService/Echo")
	}

	if string(resolved.Input.FullName()) != "tales.test.v1.EchoRequest" {
		t.Errorf("Input = %q, want tales.test.v1.EchoRequest", resolved.Input.FullName())
	}

	if string(resolved.Output.FullName()) != "tales.test.v1.EchoResponse" {
		t.Errorf("Output = %q, want tales.test.v1.EchoResponse", resolved.Output.FullName())
	}
}

func TestResolve_UnknownService(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)

	_, err := Resolve(files, "app", "missing.v1.Service", "Echo")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}

	if !strings.Contains(err.Error(), `rpc service "missing.v1.Service" not found in descriptor "app"`) {
		t.Errorf("error = %q, want the canonical not-found message", err.Error())
	}
}

func TestResolve_UnknownMethod(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)

	_, err := Resolve(files, "app", "tales.test.v1.TestService", "Unknown")
	if err == nil {
		t.Fatal("expected error for unknown method")
	}

	if !strings.Contains(err.Error(), `rpc method "Unknown" not found in service "tales.test.v1.TestService"`) {
		t.Errorf("error = %q, want the canonical not-found message", err.Error())
	}
}

func TestResolve_RejectsStreaming(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)

	_, err := Resolve(files, "app", "tales.test.v1.TestService", "WatchStream")
	if err == nil {
		t.Fatal("expected error for streaming method")
	}

	if !strings.Contains(err.Error(), `rpc method "WatchStream" is streaming; streaming RPC is not supported in V1`) {
		t.Errorf("error = %q, want the canonical streaming message", err.Error())
	}
}

func TestResolve_RejectsEmptyServiceOrMethod(t *testing.T) {
	t.Parallel()

	files := registryFromTestSet(t)

	if _, err := Resolve(files, "app", "", "Echo"); err == nil {
		t.Errorf("expected error for empty service")
	}

	if _, err := Resolve(files, "app", "tales.test.v1.TestService", ""); err == nil {
		t.Errorf("expected error for empty method")
	}
}

func TestResolve_RejectsNilFiles(t *testing.T) {
	t.Parallel()

	if _, err := Resolve(nil, "app", "x", "y"); err == nil {
		t.Errorf("expected error for nil files")
	}
}
