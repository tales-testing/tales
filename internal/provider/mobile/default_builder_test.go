package mobile

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBrokenManagerPrepareSurfacesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("$HOME is undefined")
	mgr := brokenManager{cause: cause}

	_, err := mgr.Prepare(context.Background(), "", "iOS-18-0")
	if err == nil {
		t.Fatalf("expected error from broken manager")
	}

	if !errors.Is(err, cause) {
		t.Errorf("expected wrapped cause to be unwrapped, got %v", err)
	}

	if !strings.Contains(err.Error(), "TALES_DRIVER_CACHE_DIR") {
		t.Errorf("expected error to mention the env override hint, got %q", err.Error())
	}
}

func TestBrokenManagerInvalidateSurfacesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("permission denied")
	mgr := brokenManager{cause: cause}

	err := mgr.InvalidateBuild("any-key")
	if err == nil {
		t.Fatalf("expected error from broken manager")
	}

	if !errors.Is(err, cause) {
		t.Errorf("expected wrapped cause to be unwrapped, got %v", err)
	}
}
