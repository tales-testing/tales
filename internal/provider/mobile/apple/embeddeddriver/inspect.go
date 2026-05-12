package embeddeddriver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CacheEntry describes one directory under the apple-driver cache base.
// It is the unit consumed by the doctor subcommand.
type CacheEntry struct {
	// Key is the directory name relative to the cache base.
	Key string `json:"key"`
	// Path is the absolute filesystem path of the entry.
	Path string `json:"path"`
	// Metadata mirrors the JSON object Manager writes after a successful
	// build. Zero value when metadata.json is absent or malformed.
	Metadata Metadata `json:"metadata"`
	// HasExtractOK reports whether the extract.ok marker is present.
	HasExtractOK bool `json:"extracted"`
	// HasBuildOK reports whether the build.ok marker is present (the
	// build was completed successfully at some point).
	HasBuildOK bool `json:"built"`
	// XCTestRunPath is the cached .xctestrun path recorded in build.ok.
	// Empty string when build.ok is missing or unreadable.
	XCTestRunPath string `json:"xctestrun_path,omitempty"`
	// SizeBytes is the total on-disk size of the entry directory tree.
	SizeBytes int64 `json:"size_bytes"`
	// MetadataError describes why Metadata is zero. Useful for the
	// doctor output to flag broken entries explicitly.
	MetadataError string `json:"metadata_error,omitempty"`
}

// ReadMetadata loads <cache>/metadata.json from disk.
func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata %q: %w", path, err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata %q: %w", path, err)
	}

	return meta, nil
}

// ListCacheEntries enumerates every direct subdirectory under base and
// returns one CacheEntry per directory, sorted by Key. A missing base
// directory is reported as an empty slice with no error.
func ListCacheEntries(base string) ([]CacheEntry, error) {
	if base == "" {
		return nil, errors.New("cache base is empty")
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []CacheEntry{}, nil
		}

		return nil, fmt.Errorf("read cache base %q: %w", base, err)
	}

	result := make([]CacheEntry, 0, len(entries))

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		entry := inspectEntry(base, e.Name())
		result = append(result, entry)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})

	return result, nil
}

func inspectEntry(base, name string) CacheEntry {
	paths := PathsFor(base, name)
	entry := CacheEntry{
		Key:  name,
		Path: paths.Root,
	}

	if _, err := os.Stat(paths.ExtractOK); err == nil {
		entry.HasExtractOK = true
	}

	if data, err := os.ReadFile(paths.BuildOK); err == nil {
		entry.HasBuildOK = true
		entry.XCTestRunPath = strings.TrimSpace(string(data))
	}

	meta, err := ReadMetadata(paths.Metadata)
	if err == nil {
		entry.Metadata = meta
	} else if !errors.Is(err, os.ErrNotExist) {
		entry.MetadataError = err.Error()
	}

	entry.SizeBytes = directorySize(paths.Root)

	return entry
}

// directorySize sums the sizes of every regular file under root.
// Best-effort: unreadable entries (permissions, race with deletion) are
// silently skipped so doctor still produces useful output on a partially
// broken cache.
func directorySize(root string) int64 {
	var total int64

	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return fs.SkipDir
		case d.IsDir():
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			//nolint:nilerr // best-effort cache size accounting: a file that vanishes mid-walk should not abort `tales doctor`.
			return nil
		}

		total += info.Size()

		return nil
	})

	return total
}

// EmbeddedSourceStats reports the deterministic hash, file count, and
// total uncompressed size of the embedded driver source. The hash
// matches Hash(fsys, root) so callers can compare cache entries against
// the running binary.
func EmbeddedSourceStats(fsys fs.FS, root string) (string, int, int64, error) {
	if fsys == nil {
		return "", 0, 0, errors.New("stats: filesystem is nil")
	}

	type fileRecord struct {
		path string
		size int64
	}

	var files []fileRecord

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %q: %w", p, infoErr)
		}

		files = append(files, fileRecord{path: p, size: info.Size()})

		return nil
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("walk %q: %w", root, err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	h := sha256.New()

	var bytes int64

	for _, f := range files {
		rel := strings.TrimPrefix(f.path, root)
		rel = strings.TrimPrefix(rel, "/")

		if _, hashErr := h.Write([]byte(rel)); hashErr != nil {
			return "", 0, 0, fmt.Errorf("hash path %q: %w", f.path, hashErr)
		}

		if _, hashErr := h.Write([]byte{0}); hashErr != nil {
			return "", 0, 0, fmt.Errorf("hash separator %q: %w", f.path, hashErr)
		}

		if mixErr := mixFileContents(h, fsys, f.path); mixErr != nil {
			return "", 0, 0, mixErr
		}

		if _, hashErr := h.Write([]byte{0}); hashErr != nil {
			return "", 0, 0, fmt.Errorf("hash trailer %q: %w", f.path, hashErr)
		}

		bytes += f.size
	}

	return hex.EncodeToString(h.Sum(nil)), len(files), bytes, nil
}

// EmbeddedShortHash truncates a full source-hash to the 16-hex-char
// prefix the cache key uses. Exposed so doctor output can render both
// forms consistently.
func EmbeddedShortHash(fullHash string) string {
	if len(fullHash) > 16 {
		return fullHash[:16]
	}

	return fullHash
}
