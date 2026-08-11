// Package androiddriver exposes the prebuilt UiAutomator driver APKs as
// an embedded filesystem, so the Tales binary can install them on a
// device without the Android SDK being present.
//
// Unlike the Apple driver — whose Swift source is embedded and compiled
// on first use, because building it needs Xcode which every iOS host
// already has — the Android driver ships as built artifacts. Requiring
// the Android SDK just to *run* a test would be a steep toll for a
// single-binary tool, so the APKs are built once, committed, and
// embedded. `make check-android-driver-fresh` guards against them
// drifting from the Kotlin source.
package androiddriver

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
)

// PrebuiltRoot is the directory holding the committed artifacts inside
// the embedded filesystem.
const PrebuiltRoot = "prebuilt"

// File names of the two APKs. The app APK is an empty shell that owns
// the driver's package and permissions; the test APK carries the
// instrumentation that actually serves the driver.
const (
	AppAPKName  = "tales-driver.apk"
	TestAPKName = "tales-driver-test.apk"
	// SentinelName holds the SHA-256 of the Kotlin sources the committed
	// APKs were built from.
	SentinelName = "source.sha256"
)

//go:embed prebuilt
var fsRoot embed.FS

// ErrNotBuilt reports that the driver APKs are absent from the binary.
// This happens in a working tree where the driver has never been built;
// a released binary always carries them.
var ErrNotBuilt = errors.New("the Android driver APKs are not embedded in this build")

// FS returns the embedded filesystem. Artifacts live under PrebuiltRoot.
func FS() fs.FS {
	return fsRoot
}

// APK returns the bytes of one embedded APK by file name.
func APK(name string) ([]byte, error) {
	data, err := fsRoot.ReadFile(PrebuiltRoot + "/" + name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is missing (run `make build-android-driver`)", ErrNotBuilt, name)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %s is empty (run `make build-android-driver`)", ErrNotBuilt, name)
	}

	return data, nil
}

// SourceHash returns the recorded hash of the Kotlin sources the
// embedded APKs were built from. It identifies a driver build, and feeds
// the on-device install cache key so a device is not reinstalled on
// every run.
func SourceHash() (string, error) {
	data, err := fsRoot.ReadFile(PrebuiltRoot + "/" + SentinelName)
	if err != nil {
		return "", fmt.Errorf("%w: %s is missing (run `make build-android-driver`)", ErrNotBuilt, SentinelName)
	}

	hash := string(trimSpace(data))
	if hash == "" {
		return "", fmt.Errorf("%w: %s is empty (run `make build-android-driver`)", ErrNotBuilt, SentinelName)
	}

	return hash, nil
}

// Available reports whether this binary carries usable driver APKs.
func Available() bool {
	if _, err := APK(AppAPKName); err != nil {
		return false
	}

	if _, err := APK(TestAPKName); err != nil {
		return false
	}

	return true
}

// trimSpace avoids pulling strings/bytes in for a one-line trim of the
// trailing newline Gradle writes into the sentinel.
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && isSpace(b[start]) {
		start++
	}

	end := len(b)
	for end > start && isSpace(b[end-1]) {
		end--
	}

	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
