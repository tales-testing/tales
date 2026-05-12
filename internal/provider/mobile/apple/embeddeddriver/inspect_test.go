package embeddeddriver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestReadMetadataHappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := Metadata{
		SourceHash:   "abc",
		CacheKey:     "abc-xcode-X",
		XcodeVersion: "Xcode 26.5",
		SDKVersion:   "17.4",
		IOSRuntime:   "iOS-18-0",
		MacOSMajor:   "14",
		CreatedAt:    "2026-05-12T00:00:00Z",
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	path := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	got, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if got != want {
		t.Fatalf("Metadata mismatch: got %+v want %+v", got, want)
	}
}

func TestReadMetadataMissingFile(t *testing.T) {
	t.Parallel()

	_, err := ReadMetadata(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatalf("expected error reading missing metadata")
	}

	if !os.IsNotExist(err) && !contains(err.Error(), "read metadata") {
		t.Fatalf("expected file-not-exist error, got %v", err)
	}
}

func TestReadMetadataMalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := ReadMetadata(path)
	if err == nil {
		t.Fatalf("expected decode error")
	}

	if !contains(err.Error(), "decode metadata") {
		t.Fatalf("expected decode-metadata error, got %v", err)
	}
}

func TestListCacheEntriesEmptyBase(t *testing.T) {
	t.Parallel()

	entries, err := ListCacheEntries(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected nil error for missing base, got %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected zero entries, got %d", len(entries))
	}
}

func TestListCacheEntriesRejectsEmptyBase(t *testing.T) {
	t.Parallel()

	if _, err := ListCacheEntries(""); err == nil {
		t.Fatalf("expected error for empty base")
	}
}

func TestListCacheEntriesPopulated(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// Entry 1: fully healthy (extract.ok + build.ok + metadata.json).
	healthy := seedEntry(t, base, "key-healthy", Metadata{
		SourceHash:   "healthyhash",
		CacheKey:     "key-healthy",
		XcodeVersion: "Xcode 26.5",
		SDKVersion:   "17.4",
		IOSRuntime:   "iOS-18-0",
		MacOSMajor:   "14",
	}, true, true, "/tmp/healthy/Driver.xctestrun")

	// Entry 2: extracted but never built (no build.ok).
	seedEntry(t, base, "key-partial", Metadata{
		SourceHash: "partialhash",
		CacheKey:   "key-partial",
	}, true, false, "")

	// Entry 3: completely broken (no markers, no metadata).
	if err := os.MkdirAll(filepath.Join(base, "key-broken"), 0o755); err != nil {
		t.Fatalf("seed broken: %v", err)
	}

	// Junk: a non-directory file that should be ignored.
	if err := os.WriteFile(filepath.Join(base, ".DS_Store"), []byte("junk"), 0o600); err != nil {
		t.Fatalf("seed junk: %v", err)
	}

	entries, err := ListCacheEntries(base)
	if err != nil {
		t.Fatalf("ListCacheEntries: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (sorted, junk file ignored), got %d: %+v", len(entries), entries)
	}

	// Sorted by Key alphabetically: broken, healthy, partial.
	keys := []string{entries[0].Key, entries[1].Key, entries[2].Key}
	want := []string{"key-broken", "key-healthy", "key-partial"}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("entries[%d].Key = %q, want %q", i, keys[i], want[i])
		}
	}

	if !entries[1].HasBuildOK || !entries[1].HasExtractOK {
		t.Errorf("healthy entry should have both markers, got %+v", entries[1])
	}

	if entries[1].XCTestRunPath != "/tmp/healthy/Driver.xctestrun" {
		t.Errorf("healthy XCTestRunPath = %q", entries[1].XCTestRunPath)
	}

	if entries[1].Metadata.SourceHash != "healthyhash" {
		t.Errorf("healthy metadata not parsed: %+v", entries[1].Metadata)
	}

	if entries[1].SizeBytes <= 0 {
		t.Errorf("healthy entry size should be > 0, got %d", entries[1].SizeBytes)
	}

	if !entries[2].HasExtractOK || entries[2].HasBuildOK {
		t.Errorf("partial entry should have extract.ok but not build.ok, got %+v", entries[2])
	}

	if entries[0].HasBuildOK || entries[0].HasExtractOK {
		t.Errorf("broken entry should have no markers, got %+v", entries[0])
	}

	_ = healthy
}

func TestListCacheEntriesFlagsMalformedMetadata(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	key := "key-malformed"
	root := filepath.Join(base, key)

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "metadata.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	entries, err := ListCacheEntries(base)
	if err != nil {
		t.Fatalf("ListCacheEntries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].MetadataError == "" {
		t.Fatalf("expected MetadataError to be set for malformed metadata")
	}

	if entries[0].Metadata.SourceHash != "" {
		t.Fatalf("Metadata should be zero on parse failure, got %+v", entries[0].Metadata)
	}
}

func TestEmbeddedSourceStatsMatchesHashAndCountsFiles(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"src/a.txt":     {Data: []byte("alpha")},     // 5 bytes
		"src/b.txt":     {Data: []byte("beta")},      // 4 bytes
		"src/sub/c.txt": {Data: []byte("charlie!!")}, // 9 bytes
	}

	hashFromHelper, files, bytes, err := EmbeddedSourceStats(fsys, "src")
	if err != nil {
		t.Fatalf("EmbeddedSourceStats: %v", err)
	}

	if files != 3 {
		t.Errorf("file count = %d, want 3", files)
	}

	if bytes != 5+4+9 {
		t.Errorf("total bytes = %d, want 18", bytes)
	}

	hashFromHashFn, err := Hash(fsys, "src")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if hashFromHelper != hashFromHashFn {
		t.Errorf("EmbeddedSourceStats hash %q != Hash() %q", hashFromHelper, hashFromHashFn)
	}
}

func TestEmbeddedSourceStatsNilFS(t *testing.T) {
	t.Parallel()

	if _, _, _, err := EmbeddedSourceStats(nil, "anything"); err == nil {
		t.Fatalf("expected error for nil filesystem")
	}
}

func TestEmbeddedShortHash(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                                 "",
		"abc":                              "abc",
		"0123456789abcdef":                 "0123456789abcdef",         // exactly 16
		"0123456789abcdefXXXXXXXXXXXXXXXX": "0123456789abcdef",         // truncated
	}

	for in, want := range cases {
		if got := EmbeddedShortHash(in); got != want {
			t.Errorf("EmbeddedShortHash(%q) = %q, want %q", in, got, want)
		}
	}
}

// seedEntry writes the cache files for one fake entry and returns its
// directory path.
func seedEntry(t *testing.T, base, key string, meta Metadata, extractOK, buildOK bool, xctestrun string) string {
	t.Helper()

	root := filepath.Join(base, key)
	if err := os.MkdirAll(filepath.Join(root, "source"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}

	// Drop some non-trivial bytes into source so the SizeBytes assertion fires.
	if err := os.WriteFile(filepath.Join(root, "source", "marker"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "metadata.json"), data, 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	if extractOK {
		if err := os.WriteFile(filepath.Join(root, "extract.ok"), nil, 0o600); err != nil {
			t.Fatalf("write extract.ok: %v", err)
		}
	}

	if buildOK {
		if err := os.WriteFile(filepath.Join(root, "build.ok"), []byte(xctestrun), 0o600); err != nil {
			t.Fatalf("write build.ok: %v", err)
		}
	}

	return root
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}
