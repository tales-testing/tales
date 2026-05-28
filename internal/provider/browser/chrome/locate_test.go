package chrome

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFakeChrome(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake chrome: %v", err)
	}

	return path
}

func TestLocateOverrideTakesPrecedence(t *testing.T) {
	path := newFakeChrome(t)
	t.Setenv("CHROME_PATH", "")

	got, err := Locate(path)
	if err != nil {
		t.Fatalf("Locate returned: %v", err)
	}

	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func TestLocateOverrideMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Locate("/nope/does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "driver.executable") {
		t.Fatalf("expected driver.executable diag, got: %v", err)
	}
}

func TestLocateUsesChromePathEnv(t *testing.T) {
	path := newFakeChrome(t)
	t.Setenv("CHROME_PATH", path)

	got, err := Locate("")
	if err != nil {
		t.Fatalf("Locate returned: %v", err)
	}

	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func TestLocateChromePathMissingFile(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nope/does-not-exist")

	_, err := Locate("")
	if err == nil || !strings.Contains(err.Error(), "CHROME_PATH") {
		t.Fatalf("expected CHROME_PATH diag, got: %v", err)
	}
}

func TestLocateNotFoundReturnsHint(t *testing.T) {
	t.Setenv("CHROME_PATH", "")

	// Stub PATH lookup so the test runs deterministically on machines
	// where Chrome is installed.
	originalLook := execLookPath
	execLookPath = func(string) (string, error) {
		return "", errors.New("not in PATH")
	}

	defer func() { execLookPath = originalLook }()

	// Stub OS install paths too — point at a temp dir that won't exist
	// under any of the candidate names.
	originalOS := osCandidatesFn
	osCandidatesFn = func() []string { return []string{"/definitely/nope"} }

	defer func() { osCandidatesFn = originalOS }()

	_, err := Locate("")
	if err == nil || !strings.Contains(err.Error(), "chrome executable not found") {
		t.Fatalf("expected not-found diag, got: %v", err)
	}
}
