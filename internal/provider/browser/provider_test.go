package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
)

func TestProviderType(t *testing.T) {
	t.Parallel()

	p := New()
	if p.Type() != "browser" {
		t.Fatalf("Type = %q, want browser", p.Type())
	}
}

func TestProviderExecuteStubMissingInput(t *testing.T) {
	t.Parallel()

	p := New()

	_, err := p.Execute(context.Background(), provider.Input{})
	if err == nil || !strings.Contains(err.Error(), "missing pre-evaluated step data") {
		t.Fatalf("expected missing-input error, got: %v", err)
	}
}

func TestProviderExecuteStubMissingBuilder(t *testing.T) {
	t.Parallel()

	p := New()

	_, err := p.Execute(context.Background(), provider.Input{Browser: &provider.BrowserExecution{}})
	if err == nil || !strings.Contains(err.Error(), "no session builder configured") {
		t.Fatalf("expected missing-builder error, got: %v", err)
	}
}

// Real end-to-end behavior is covered by provider_exec_test.go via a fake
// driver. The remaining stub tests here only guard input validation paths.

func TestProviderCloseOnEmpty(t *testing.T) {
	t.Parallel()

	p := New()
	if err := p.Close(); err != nil {
		t.Fatalf("Close on empty provider should not error: %v", err)
	}

	// Idempotency check.
	if err := p.Close(); err != nil {
		t.Fatalf("second Close should not error: %v", err)
	}
}

func TestLastSnapshotEmpty(t *testing.T) {
	t.Parallel()

	p := New()
	if snap, ok := p.LastSnapshot("scenario", "step"); ok || snap != nil {
		t.Fatalf("expected empty snapshot, got %v", snap)
	}
}
