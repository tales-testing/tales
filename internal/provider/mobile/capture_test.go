package mobile

import (
	"strings"
	"testing"
)

func TestParseCaptureMode_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want CaptureMode
	}{
		{"none", CaptureNone},
		{"failures", CaptureFailures},
		{"steps", CaptureSteps},
		{"actions", CaptureActions},
		{"NONE", CaptureNone},
		{"  Actions  ", CaptureActions},
		{"Failures", CaptureFailures},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCaptureMode(tc.in)
			if err != nil {
				t.Fatalf("ParseCaptureMode(%q) returned error: %v", tc.in, err)
			}

			if got != tc.want {
				t.Fatalf("ParseCaptureMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestProviderDefaultCaptureMode(t *testing.T) {
	t.Parallel()

	p := New()
	if p.captureMode != CaptureFailures {
		t.Fatalf("default capture mode = %q, want %q", p.captureMode, CaptureFailures)
	}
}

func TestWithCaptureModeOverridesDefault(t *testing.T) {
	t.Parallel()

	p := New(WithCaptureMode(CaptureActions))
	if p.captureMode != CaptureActions {
		t.Fatalf("captureMode after WithCaptureMode(CaptureActions) = %q, want %q", p.captureMode, CaptureActions)
	}
}

func TestParseCaptureMode_Invalid(t *testing.T) {
	t.Parallel()

	_, err := ParseCaptureMode("loud")
	if err == nil {
		t.Fatal("expected error for invalid capture mode, got nil")
	}

	msg := err.Error()
	for _, want := range []string{"none", "failures", "steps", "actions"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing valid value %q", msg, want)
		}
	}

	if !strings.Contains(msg, "loud") {
		t.Errorf("error message %q should echo the invalid input", msg)
	}
}
