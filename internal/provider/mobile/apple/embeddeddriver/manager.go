package embeddeddriver

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manager orchestrates extract → build → cache for the embedded Apple
// driver. It is safe to share across goroutines: a per-cache-key mutex
// serializes in-process work and a file lock guards concurrent Tales
// invocations.
type Manager struct {
	Source     fs.FS
	SourceRoot string
	CacheBase  string
	Builder    Builder
	Runner     CommandRunner
	Now        func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Prepared captures the cache-resolved paths the runner needs to launch
// the driver via `xcodebuild test-without-building`.
type Prepared struct {
	XCTestRunPath string
	BuildLogPath  string
	SourceDir     string
	DerivedData   string
	CacheKey      string
	SourceHash    string
}

// Prepare ensures the driver is extracted and built for the inputs that
// participate in the cache key, then returns the paths to use for
// running. Pass sourcePathOverride to use a local source checkout
// instead of the embedded filesystem (developer override).
func (m *Manager) Prepare(ctx context.Context, sourcePathOverride, iosRuntime string) (Prepared, error) {
	fsys, root := m.resolveSource(sourcePathOverride)
	if fsys == nil {
		return Prepared{}, fmt.Errorf("embedded driver source is not configured")
	}

	sourceHash, err := Hash(fsys, root)
	if err != nil {
		return Prepared{}, fmt.Errorf("hash driver source: %w", err)
	}

	xcodeVersion := XcodeVersion(ctx, m.Runner)
	sdkVersion := XcodeSDKVersion(ctx, m.Runner)
	developerDir := DeveloperDir(ctx, m.Runner)
	macosMajor := MacOSMajor(ctx, m.Runner)

	key := CacheKey(sourceHash, xcodeVersion, sdkVersion, developerDir, iosRuntime, macosMajor)
	paths := PathsFor(m.CacheBase, key)

	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return Prepared{}, fmt.Errorf("ensure cache root: %w", err)
	}

	if err := os.MkdirAll(paths.LogDir, 0o755); err != nil {
		return Prepared{}, fmt.Errorf("ensure log dir: %w", err)
	}

	mtx := m.lockFor(key)
	mtx.Lock()
	defer mtx.Unlock()

	release, err := acquireFileLock(paths.Lock)
	if err != nil {
		return Prepared{}, fmt.Errorf("acquire cache lock: %w", err)
	}

	defer release()

	if err := m.ensureExtracted(fsys, root, paths); err != nil {
		return Prepared{}, err
	}

	xctestrun, err := m.ensureBuilt(ctx, paths)
	if err != nil {
		return Prepared{}, err
	}

	_ = m.writeMetadata(paths, metadata{
		SourceHash:   sourceHash,
		CacheKey:     key,
		XcodeVersion: xcodeVersion,
		SDKVersion:   sdkVersion,
		DeveloperDir: developerDir,
		IOSRuntime:   iosRuntime,
		MacOSMajor:   macosMajor,
	})

	return Prepared{
		XCTestRunPath: xctestrun,
		BuildLogPath:  paths.BuildLog,
		SourceDir:     paths.Source,
		DerivedData:   paths.DerivedData,
		CacheKey:      key,
		SourceHash:    sourceHash,
	}, nil
}

// InvalidateBuild removes build.ok for the given cache key so that the
// next Prepare call rebuilds the driver. Useful when health check fails
// after a successful build and the caller wants to retry from scratch.
func (m *Manager) InvalidateBuild(key string) error {
	paths := PathsFor(m.CacheBase, key)

	if err := os.Remove(paths.BuildOK); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("invalidate build.ok: %w", err)
	}

	return nil
}

func (m *Manager) resolveSource(sourcePathOverride string) (fs.FS, string) {
	if sourcePathOverride != "" {
		return os.DirFS(sourcePathOverride), "."
	}

	return m.Source, m.SourceRoot
}

func (m *Manager) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locks == nil {
		m.locks = map[string]*sync.Mutex{}
	}

	if mtx, ok := m.locks[key]; ok {
		return mtx
	}

	mtx := &sync.Mutex{}
	m.locks[key] = mtx

	return mtx
}

func (m *Manager) ensureExtracted(fsys fs.FS, root string, paths Paths) error {
	if hasMarker(paths.ExtractOK) && projectExists(paths.Source) {
		return nil
	}

	if err := Extract(fsys, root, paths.Source); err != nil {
		return fmt.Errorf("extract embedded source: %w", err)
	}

	if err := writeMarker(paths.ExtractOK); err != nil {
		return fmt.Errorf("write extract marker: %w", err)
	}

	return nil
}

func (m *Manager) ensureBuilt(ctx context.Context, paths Paths) (string, error) {
	if data, err := os.ReadFile(paths.BuildOK); err == nil {
		xctestrun := strings.TrimSpace(string(data))
		if xctestrun != "" {
			if _, statErr := os.Stat(xctestrun); statErr == nil {
				return xctestrun, nil
			}
		}

		_ = os.Remove(paths.BuildOK)
	}

	if m.Builder == nil {
		return "", fmt.Errorf("embeddeddriver: builder is not configured")
	}

	xctestrun, err := m.Builder.BuildForTesting(ctx, paths.Source, paths.DerivedData, paths.BuildLog)
	if err != nil {
		return "", fmt.Errorf("build embedded driver: %w", err)
	}

	if writeErr := os.WriteFile(paths.BuildOK, []byte(xctestrun), 0o600); writeErr != nil {
		return "", fmt.Errorf("write build.ok: %w", writeErr)
	}

	return xctestrun, nil
}

type metadata struct {
	SourceHash   string `json:"source_hash"`
	CacheKey     string `json:"cache_key"`
	XcodeVersion string `json:"xcode_version"`
	SDKVersion   string `json:"sdk_version"`
	DeveloperDir string `json:"developer_dir"`
	IOSRuntime   string `json:"ios_runtime"`
	MacOSMajor   string `json:"macos_major"`
	CreatedAt    string `json:"created_at"`
}

func (m *Manager) writeMetadata(paths Paths, meta metadata) error {
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}

	meta.CreatedAt = now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(paths.Metadata, data, 0o600); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

func hasMarker(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func projectExists(sourceDir string) bool {
	_, err := os.Stat(filepath.Join(sourceDir, DriverProject))

	return err == nil
}

func writeMarker(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open marker: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close marker: %w", err)
	}

	return nil
}
