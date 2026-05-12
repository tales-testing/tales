package embeddeddriver

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

type fakeRunner struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	output []byte
	err    error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}

	if resp, ok := r.responses[key]; ok {
		return resp.output, resp.err
	}

	return nil, errors.New("unexpected command: " + key)
}

func TestXcodeVersionFromRunner(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{responses: map[string]fakeResponse{
		"xcodebuild -version": {output: []byte("Xcode 26.5\nBuild version 17F42")},
	}}

	got := XcodeVersion(context.Background(), r)
	want := "Xcode 26.5 Build version 17F42"

	if got != want {
		t.Fatalf("XcodeVersion = %q, want %q", got, want)
	}
}

func TestXcodeVersionFallback(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{responses: map[string]fakeResponse{
		"xcodebuild -version": {err: errors.New("not installed")},
	}}

	if got := XcodeVersion(context.Background(), r); got != XcodeUnknown {
		t.Fatalf("expected fallback, got %q", got)
	}

	if got := XcodeVersion(context.Background(), nil); got != XcodeUnknown {
		t.Fatalf("expected fallback for nil runner, got %q", got)
	}
}

func TestXcodeSDKVersionFromRunner(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{responses: map[string]fakeResponse{
		"xcrun --show-sdk-version --sdk iphonesimulator": {output: []byte("17.4\n")},
	}}

	if got := XcodeSDKVersion(context.Background(), r); got != "17.4" {
		t.Fatalf("XcodeSDKVersion = %q, want 17.4", got)
	}
}

func TestDeveloperDirFromRunner(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{responses: map[string]fakeResponse{
		"xcode-select -p": {output: []byte("/Applications/Xcode.app/Contents/Developer\n")},
	}}

	if got := DeveloperDir(context.Background(), r); got != "/Applications/Xcode.app/Contents/Developer" {
		t.Fatalf("DeveloperDir = %q", got)
	}
}

func TestMacOSMajorOnDarwin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skipf("test only meaningful on darwin (current: %s)", runtime.GOOS)
	}

	r := &fakeRunner{responses: map[string]fakeResponse{
		"sw_vers -productVersion": {output: []byte("14.5.1\n")},
	}}

	if got := MacOSMajor(context.Background(), r); got != "14" {
		t.Fatalf("MacOSMajor = %q, want 14", got)
	}
}

func TestMacOSMajorOnNonDarwin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "darwin" {
		t.Skipf("non-darwin behavior only verifiable off-darwin (current: %s)", runtime.GOOS)
	}

	if got := MacOSMajor(context.Background(), nil); got != runtime.GOOS {
		t.Fatalf("MacOSMajor on non-darwin = %q, want %q", got, runtime.GOOS)
	}
}
