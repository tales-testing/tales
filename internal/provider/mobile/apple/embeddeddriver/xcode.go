package embeddeddriver

import (
	"context"
	"runtime"
	"strings"
)

// XcodeUnknown is the sentinel returned when Xcode introspection fails
// so the cache key remains computable (just less specific).
const XcodeUnknown = "unknown"

// CommandRunner runs an external command synchronously and returns its
// combined output. The Apple embedded-driver subsystem invokes
// xcodebuild / xcrun / sw_vers through this interface so tests can
// substitute a fake.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// XcodeVersion returns a stable, single-line representation of
// `xcodebuild -version`. On failure XcodeUnknown is returned.
func XcodeVersion(ctx context.Context, r CommandRunner) string {
	if r == nil {
		return XcodeUnknown
	}

	out, err := r.Run(ctx, "xcodebuild", "-version")
	if err != nil {
		return XcodeUnknown
	}

	return collapseLines(string(out))
}

// XcodeSDKVersion returns the iphonesimulator SDK version.
func XcodeSDKVersion(ctx context.Context, r CommandRunner) string {
	if r == nil {
		return XcodeUnknown
	}

	out, err := r.Run(ctx, "xcrun", "--show-sdk-version", "--sdk", "iphonesimulator")
	if err != nil {
		return XcodeUnknown
	}

	return strings.TrimSpace(string(out))
}

// DeveloperDir returns the active Xcode developer directory.
func DeveloperDir(ctx context.Context, r CommandRunner) string {
	if r == nil {
		return XcodeUnknown
	}

	out, err := r.Run(ctx, "xcode-select", "-p")
	if err != nil {
		return XcodeUnknown
	}

	return strings.TrimSpace(string(out))
}

// MacOSMajor returns the major macOS version (e.g. "14"). On non-darwin
// hosts it returns the GOOS name so the cache key still differs across
// platforms.
func MacOSMajor(ctx context.Context, r CommandRunner) string {
	if runtime.GOOS != "darwin" {
		return runtime.GOOS
	}

	if r == nil {
		return XcodeUnknown
	}

	out, err := r.Run(ctx, "sw_vers", "-productVersion")
	if err != nil {
		return XcodeUnknown
	}

	version := strings.TrimSpace(string(out))
	if idx := strings.Index(version, "."); idx > 0 {
		return version[:idx]
	}

	return version
}

func collapseLines(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}

	return strings.Join(lines, " ")
}
