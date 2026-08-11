// Package adb wraps the subset of the Android Debug Bridge CLI the
// mobile provider needs, behind a Runner-backed API so unit tests can
// substitute a fake executor.
//
// Tales shells out to adb rather than speaking the ADB wire protocol
// itself. That matches how the rest of the project drives external
// tooling (xcrun, xcodebuild, the Chrome binary), keeps `adb forward`
// and friends available without reimplementing them, and means a user
// debugging a failure can reproduce every step by hand.
package adb

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// execLookPath is split out so tests can stub binary lookup.
var execLookPath = defaultLookPath

// Locate returns the path to the adb executable. Resolution order:
//
//  1. override (the target's driver.adb_path, when set)
//  2. ADB_PATH environment variable
//  3. $ANDROID_HOME / $ANDROID_SDK_ROOT platform-tools
//  4. PATH lookup
//  5. OS-specific default SDK locations
//
// The returned path is verified with os.Stat so a stale reference fails
// immediately rather than at the first command. A missing binary
// surfaces as an error naming ANDROID_HOME, since an unset SDK root is
// by far the most common cause.
func Locate(override string) (string, error) {
	if override != "" {
		if err := verifyExecutable(override); err != nil {
			return "", fmt.Errorf("driver.adb_path: %w", err)
		}

		return override, nil
	}

	if env := os.Getenv("ADB_PATH"); env != "" {
		if err := verifyExecutable(env); err != nil {
			return "", fmt.Errorf("ADB_PATH: %w", err)
		}

		return env, nil
	}

	for _, root := range sdkRoots() {
		candidate := filepath.Join(root, "platform-tools", binaryName())
		if err := verifyExecutable(candidate); err == nil {
			return candidate, nil
		}
	}

	if path, ok := execLookPath(binaryName()); ok {
		return path, nil
	}

	for _, candidate := range osCandidates() {
		if err := verifyExecutable(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"adb executable not found; install the Android SDK platform-tools and set ANDROID_HOME, " +
			"or point ADB_PATH at the binary",
	)
}

// SDKRoot returns the resolved Android SDK root, or "" when none is
// configured. Used to find sibling tools (the emulator) once adb itself
// has been located.
func SDKRoot() string {
	for _, root := range sdkRoots() {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
	}

	return ""
}

// sdkRoots lists candidate SDK roots in priority order. ANDROID_HOME is
// the documented variable; ANDROID_SDK_ROOT is its deprecated
// predecessor and still set on plenty of machines and CI images.
func sdkRoots() []string {
	roots := make([]string, 0, 3)

	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, defaultSDKPath(home))
	}

	return roots
}

// goosDarwin and goosWindows name the GOOS values this file branches on.
const (
	goosDarwin  = "darwin"
	goosWindows = "windows"
)

func defaultSDKPath(home string) string {
	if runtime.GOOS == goosDarwin {
		return filepath.Join(home, "Library", "Android", "sdk")
	}

	return filepath.Join(home, "Android", "Sdk")
}

func osCandidates() []string {
	const usrLocalADB = "/usr/local/bin/adb"

	switch runtime.GOOS {
	case goosDarwin:
		return []string{"/opt/homebrew/bin/adb", usrLocalADB}
	case "linux":
		return []string{"/usr/lib/android-sdk/platform-tools/adb", usrLocalADB}
	default:
		return nil
	}
}

func binaryName() string {
	if runtime.GOOS == goosWindows {
		return "adb.exe"
	}

	return "adb"
}

func verifyExecutable(path string) error {
	//nolint:gosec // G703: probing user-supplied adb paths is the whole point of this function; we only read metadata.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%q is a directory, not an executable", path)
	}

	return nil
}
