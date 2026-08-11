package android

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	androiddriver "github.com/tales-testing/tales/drivers/android"
)

// EnvCacheDir overrides where the driver APKs are materialized.
const EnvCacheDir = "TALES_DRIVER_CACHE_DIR"

// EmbeddedArtifacts writes the APKs embedded in the Tales binary to a
// cache directory so adb can install them from disk.
//
// Unlike the Apple driver, nothing is compiled here: the APKs are built
// ahead of time and committed, so this is an extraction and nothing
// more. The cache exists because adb installs from a path, not a stream.
type EmbeddedArtifacts struct {
	// CacheBase is the directory the APKs are written under. Resolved
	// from the user cache dir when empty.
	CacheBase string
}

// Prepare materializes the embedded APKs and returns their paths.
//
// Extraction is skipped when the files are already present with the
// expected size, which makes repeat runs free.
func (a EmbeddedArtifacts) Prepare(_ context.Context) (Prepared, error) {
	hash, err := androiddriver.SourceHash()
	if err != nil {
		return Prepared{}, fmt.Errorf("read embedded driver hash: %w", err)
	}

	base := a.CacheBase

	if base == "" {
		base, err = ResolveCacheBase()
		if err != nil {
			return Prepared{}, err
		}
	}

	// Keying the directory on the source hash means a driver upgrade
	// lands in a new directory rather than overwriting one that a
	// concurrent run may be installing from.
	dir := filepath.Join(base, hash[:16])

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Prepared{}, fmt.Errorf("create driver cache dir: %w", err)
	}

	appPath, err := extract(dir, androiddriver.AppAPKName)
	if err != nil {
		return Prepared{}, err
	}

	testPath, err := extract(dir, androiddriver.TestAPKName)
	if err != nil {
		return Prepared{}, err
	}

	return Prepared{AppAPKPath: appPath, TestAPKPath: testPath, SourceHash: hash}, nil
}

// ResolveCacheBase returns the directory driver artifacts are cached in.
func ResolveCacheBase() (string, error) {
	if env := os.Getenv(EnvCacheDir); env != "" {
		return filepath.Join(env, "android-driver"), nil
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user cache dir: %w (set %s to a writable directory)", err, EnvCacheDir)
	}

	return filepath.Join(cacheDir, "tales", "android-driver"), nil
}

// extract writes one embedded APK, skipping the write when an identical
// file is already there.
func extract(dir, name string) (string, error) {
	data, err := androiddriver.APK(name)
	if err != nil {
		return "", fmt.Errorf("read embedded driver: %w", err)
	}

	path := filepath.Join(dir, name)

	if info, statErr := os.Stat(path); statErr == nil && info.Size() == int64(len(data)) {
		return path, nil
	}

	// Write to a temporary then rename: a concurrent run must never see
	// a half-written APK, which adb would reject with a confusing
	// parse error.
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", name, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return "", fmt.Errorf("install %s into the cache: %w", name, err)
	}

	return path, nil
}

// brokenArtifacts reports a fixed cause from every call. It stands in
// when the binary carries no driver, so an Android scenario fails with
// the real reason and the command that fixes it rather than a vague
// "driver not configured".
type brokenArtifacts struct {
	cause error
}

func (b brokenArtifacts) Prepare(_ context.Context) (Prepared, error) {
	return Prepared{}, b.cause
}

// newArtifacts returns the production artifact source, or a broken one
// carrying the reason when the binary has no embedded driver.
func newArtifacts() DriverArtifacts {
	if !androiddriver.Available() {
		return brokenArtifacts{cause: fmt.Errorf(
			"%w; build it with `make build-android-driver`, "+
				"or set driver.external = true to connect to a driver you started yourself",
			androiddriver.ErrNotBuilt)}
	}

	if _, err := ResolveCacheBase(); err != nil {
		return brokenArtifacts{cause: err}
	}

	return EmbeddedArtifacts{}
}
