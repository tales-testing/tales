package rpc

import (
	"strings"
	"testing"
)

func TestFromCode_KnownCodes(t *testing.T) {
	t.Parallel()

	cases := map[uint32]string{
		0:  StatusOK,
		1:  StatusCancelled,
		3:  StatusInvalidArgument,
		5:  StatusNotFound,
		7:  StatusPermissionDenied,
		16: StatusUnauthenticated,
	}

	for code, want := range cases {
		if got := FromCode(code); got != want {
			t.Errorf("FromCode(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestFromCode_OutOfRange(t *testing.T) {
	t.Parallel()

	for _, code := range []uint32{17, 100, 1 << 31} {
		if got := FromCode(code); got != StatusUnknown {
			t.Errorf("FromCode(%d) = %q, want %q", code, got, StatusUnknown)
		}
	}
}

func TestNormalize_CaseInsensitive(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"ok", "OK", "Ok", "  ok  "} {
		got, err := Normalize(raw)
		if err != nil {
			t.Errorf("Normalize(%q) returned error: %v", raw, err)

			continue
		}

		if got != StatusOK {
			t.Errorf("Normalize(%q) = %q, want %q", raw, got, StatusOK)
		}
	}
}

func TestNormalize_RejectsEmpty(t *testing.T) {
	t.Parallel()

	if _, err := Normalize(""); err == nil {
		t.Fatal("expected error for empty status")
	}

	if _, err := Normalize("   "); err == nil {
		t.Fatal("expected error for whitespace status")
	}
}

func TestNormalize_RejectsUnknown(t *testing.T) {
	t.Parallel()

	_, err := Normalize("totally_made_up")
	if err == nil {
		t.Fatal("expected error for unknown status")
	}

	if !strings.Contains(err.Error(), `rpc status "totally_made_up" is not a canonical gRPC code`) {
		t.Errorf("error = %q, want the canonical not-recognised message", err.Error())
	}
}
