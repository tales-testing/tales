package rpc

import (
	"strings"
	"testing"
)

func TestStatusFromCode_KnownCodes(t *testing.T) {
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
		if got := StatusFromCode(code); got != want {
			t.Errorf("StatusFromCode(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestStatusFromCode_OutOfRange(t *testing.T) {
	t.Parallel()

	for _, code := range []uint32{17, 100, 1 << 31} {
		if got := StatusFromCode(code); got != StatusUnknown {
			t.Errorf("StatusFromCode(%d) = %q, want %q", code, got, StatusUnknown)
		}
	}
}

func TestNormalizeStatus_CaseInsensitive(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"ok", "OK", "Ok", "  ok  "} {
		got, err := NormalizeStatus(raw)
		if err != nil {
			t.Errorf("NormalizeStatus(%q) returned error: %v", raw, err)

			continue
		}

		if got != StatusOK {
			t.Errorf("NormalizeStatus(%q) = %q, want %q", raw, got, StatusOK)
		}
	}
}

func TestNormalizeStatus_RejectsEmpty(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeStatus(""); err == nil {
		t.Fatal("expected error for empty status")
	}

	if _, err := NormalizeStatus("   "); err == nil {
		t.Fatal("expected error for whitespace status")
	}
}

func TestNormalizeStatus_RejectsUnknown(t *testing.T) {
	t.Parallel()

	_, err := NormalizeStatus("totally_made_up")
	if err == nil {
		t.Fatal("expected error for unknown status")
	}

	if !strings.Contains(err.Error(), `rpc status "totally_made_up" is not a canonical gRPC code`) {
		t.Errorf("error = %q, want the canonical not-recognised message", err.Error())
	}
}
