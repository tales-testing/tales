package lang

import (
	goruntime "runtime"
	"testing"
)

func TestHostOSExposedInEvalContext(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `host.os`)
	if value.AsString() != goruntime.GOOS {
		t.Fatalf("host.os = %q want %q", value.AsString(), goruntime.GOOS)
	}
}

func TestHostArchExposedInEvalContext(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `host.arch`)
	if value.AsString() != goruntime.GOARCH {
		t.Fatalf("host.arch = %q want %q", value.AsString(), goruntime.GOARCH)
	}
}

func TestHostCompositeExpression(t *testing.T) {
	t.Parallel()

	// Boolean expression mirroring how skip rules will use host info.
	value := evalTestExpression(t, `host.os == "`+goruntime.GOOS+`"`)
	if !value.True() {
		t.Fatalf("host.os comparison expected true, got %v", value)
	}
}
