package androiddriver

import (
	"errors"
	"testing"
)

func TestAvailableMatchesArtifactPresence(t *testing.T) {
	t.Parallel()

	// A working tree that has never built the driver carries no APKs,
	// and a released binary always does. Both are valid; what must hold
	// is that Available agrees with what APK can actually return, so the
	// backend can gate on it instead of discovering the gap mid-session.
	_, appErr := APK(AppAPKName)
	_, testErr := APK(TestAPKName)

	want := appErr == nil && testErr == nil

	if got := Available(); got != want {
		t.Fatalf("Available() = %v, but APK errors were app=%v test=%v", got, appErr, testErr)
	}
}

func TestMissingArtifactsReportHowToBuildThem(t *testing.T) {
	t.Parallel()

	if Available() {
		t.Skip("driver APKs are embedded in this build")
	}

	_, err := APK(AppAPKName)
	if err == nil {
		t.Fatal("expected an error for a missing APK")
	}

	if !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("expected ErrNotBuilt, got %v", err)
	}

	// The message has to name the command: this is what a contributor
	// sees the first time they run an Android scenario from a fresh
	// checkout, and guessing the target is a poor first experience.
	if got := err.Error(); !contains(got, "make build-android-driver") {
		t.Fatalf("error should name the build command, got %q", got)
	}
}

func TestUnknownArtifactIsRejected(t *testing.T) {
	t.Parallel()

	if _, err := APK("not-a-driver.apk"); err == nil {
		t.Fatal("expected an error for an unknown artifact name")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}
