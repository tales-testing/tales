package embeddeddriver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheKeyIncludesAllInputs(t *testing.T) {
	t.Parallel()

	base := CacheKey("abc123", "Xcode 26.5", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "14")

	tests := []struct {
		name    string
		key     string
		differs bool
	}{
		{"source-hash", CacheKey("differentHash", "Xcode 26.5", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "14"), true},
		{"xcode-version", CacheKey("abc123", "Xcode 16.0", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "14"), true},
		{"sdk-version", CacheKey("abc123", "Xcode 26.5", "17.5", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "14"), true},
		{"developer-dir", CacheKey("abc123", "Xcode 26.5", "17.4", "/Applications/Xcode-beta.app/Contents/Developer", "iOS 18.0", "14"), true},
		{"ios-runtime", CacheKey("abc123", "Xcode 26.5", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 17.4", "14"), true},
		{"macos-major", CacheKey("abc123", "Xcode 26.5", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "13"), true},
		{"identical", CacheKey("abc123", "Xcode 26.5", "17.4", "/Applications/Xcode.app/Contents/Developer", "iOS 18.0", "14"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.differs && tc.key == base {
				t.Fatalf("changing %s should change the cache key", tc.name)
			}

			if !tc.differs && tc.key != base {
				t.Fatalf("identical inputs should produce the same cache key (%s vs %s)", base, tc.key)
			}
		})
	}
}

func TestCacheKeySanitization(t *testing.T) {
	t.Parallel()

	key := CacheKey("abc/123", "Xcode 26.5\nBuild 17F42", "17.4", "/path with spaces/Developer", "iOS 18.0 sim", "14")

	if strings.ContainsAny(key, "/\\\n ") {
		t.Fatalf("cache key %q must be filesystem-safe", key)
	}
}

func TestCacheKeyEmptyFallbacks(t *testing.T) {
	t.Parallel()

	key := CacheKey("", "", "", "", "", "")
	if !strings.Contains(key, "unknown") {
		t.Fatalf("expected fallback unknown segments, got %q", key)
	}
}

func TestPathsFor(t *testing.T) {
	t.Parallel()

	paths := PathsFor("/cache", "key123")

	want := map[string]string{
		"Root":        filepath.Join("/cache", "key123"),
		"Source":      filepath.Join("/cache", "key123", "source"),
		"DerivedData": filepath.Join("/cache", "key123", "derived-data"),
		"BuildLog":    filepath.Join("/cache", "key123", "logs", "build.log"),
		"BuildOK":     filepath.Join("/cache", "key123", "build.ok"),
		"ExtractOK":   filepath.Join("/cache", "key123", "extract.ok"),
		"Metadata":    filepath.Join("/cache", "key123", "metadata.json"),
		"Lock":        filepath.Join("/cache", "key123", ".lock"),
	}

	got := map[string]string{
		"Root":        paths.Root,
		"Source":      paths.Source,
		"DerivedData": paths.DerivedData,
		"BuildLog":    paths.BuildLog,
		"BuildOK":     paths.BuildOK,
		"ExtractOK":   paths.ExtractOK,
		"Metadata":    paths.Metadata,
		"Lock":        paths.Lock,
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("Paths.%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestResolveBaseHonorsEnvOverride(t *testing.T) {
	t.Setenv(EnvCacheDir, "/tmp/custom-cache")

	base, err := ResolveBase()
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}

	if base != "/tmp/custom-cache" {
		t.Fatalf("expected env override to be returned verbatim, got %q", base)
	}
}

func TestResolveBaseAppendsSubdirWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvCacheDir, "")

	base, err := ResolveBase()
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}

	if !strings.HasSuffix(base, filepath.Join("tales", "apple-driver")) {
		t.Fatalf("expected base to end with tales/apple-driver, got %q", base)
	}
}
