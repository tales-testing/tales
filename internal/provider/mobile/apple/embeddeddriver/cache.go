// Package embeddeddriver orchestrates extraction, build, and caching of
// the embedded Apple XCUITest driver source so the Tales binary can run
// iOS tests without the repository being checked out next to it.
package embeddeddriver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EnvCacheDir overrides the default cache base. When set, the value is
// used as the final base directory (no extra prefix appended).
const EnvCacheDir = "TALES_DRIVER_CACHE_DIR"

const cacheBaseSubdir = "tales" + string(filepath.Separator) + "apple-driver"

// Paths gathers every filesystem path used by the cache layout for a
// single cache key.
type Paths struct {
	Base        string
	Root        string
	Source      string
	DerivedData string
	LogDir      string
	BuildLog    string
	BuildOK     string
	ExtractOK   string
	Metadata    string
	Lock        string
}

// ResolveBase returns the cache base directory. It honors EnvCacheDir
// when set, otherwise it appends "tales/apple-driver" to os.UserCacheDir.
func ResolveBase() (string, error) {
	if env := strings.TrimSpace(os.Getenv(EnvCacheDir)); env != "" {
		return env, nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}

	return filepath.Join(base, cacheBaseSubdir), nil
}

var unsafeKey = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeKeySegment(s string) string {
	cleaned := unsafeKey.ReplaceAllString(s, "_")
	cleaned = strings.Trim(cleaned, "_")

	if cleaned == "" {
		return "unknown"
	}

	return cleaned
}

// CacheKey assembles a deterministic, filesystem-safe cache key from
// the inputs that should invalidate the cache. Any empty input is
// replaced by "unknown" so the key remains usable.
func CacheKey(sourceHash, xcodeVersion, sdkVersion, developerDir, iosRuntime, macosMajor string) string {
	hash := sanitizeKeySegment(sourceHash)
	if len(hash) > 16 {
		hash = hash[:16]
	}

	return fmt.Sprintf(
		"%s-xcode-%s-sdk-%s-dev-%s-ios-%s-mac-%s",
		hash,
		sanitizeKeySegment(xcodeVersion),
		sanitizeKeySegment(sdkVersion),
		sanitizeKeySegment(developerDir),
		sanitizeKeySegment(iosRuntime),
		sanitizeKeySegment(macosMajor),
	)
}

// PathsFor returns the file layout for the given cache base and key.
func PathsFor(base, key string) Paths {
	root := filepath.Join(base, key)

	return Paths{
		Base:        base,
		Root:        root,
		Source:      filepath.Join(root, "source"),
		DerivedData: filepath.Join(root, "derived-data"),
		LogDir:      filepath.Join(root, "logs"),
		BuildLog:    filepath.Join(root, "logs", "build.log"),
		BuildOK:     filepath.Join(root, "build.ok"),
		ExtractOK:   filepath.Join(root, "extract.ok"),
		Metadata:    filepath.Join(root, "metadata.json"),
		Lock:        filepath.Join(root, ".lock"),
	}
}
