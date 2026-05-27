package embeddeddriver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

type fakeBuilder struct {
	mu        sync.Mutex
	calls     int32
	failFirst int32
	hook      func(sourceDir, derivedDataPath, logPath string) (string, error)
}

func (b *fakeBuilder) BuildForTesting(_ context.Context, sourceDir, derivedDataPath, logPath string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	atomic.AddInt32(&b.calls, 1)

	if b.failFirst > 0 {
		b.failFirst--

		return "", errors.New("build failed")
	}

	if b.hook != nil {
		return b.hook(sourceDir, derivedDataPath, logPath)
	}

	xctestrun := filepath.Join(derivedDataPath, "Build", "Products", "TalesAppleDriverUITests_Debug.xctestrun")
	if err := os.MkdirAll(filepath.Dir(xctestrun), 0o755); err != nil {
		return "", err
	}

	if err := os.WriteFile(xctestrun, []byte("xml"), 0o600); err != nil {
		return "", err
	}

	if err := os.WriteFile(logPath, []byte("ok"), 0o600); err != nil {
		return "", err
	}

	return xctestrun, nil
}

func newTestManager(t *testing.T, builder Builder) (*Manager, fstest.MapFS, string) {
	t.Helper()

	fsys := fstest.MapFS{
		"TalesAppleDriver/TalesAppleDriver.xcodeproj/project.pbxproj":     {Data: []byte("project")},
		"TalesAppleDriver/TalesAppleDriverUITests/TalesAppleDriver.swift": {Data: []byte("swift")},
	}

	cacheBase := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cacheBase, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	mgr := &Manager{
		Source:     fsys,
		SourceRoot: "TalesAppleDriver",
		CacheBase:  cacheBase,
		Builder:    builder,
		Runner:     nil,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}

	return mgr, fsys, cacheBase
}

func TestManagerPrepareExtractsAndBuilds(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	mgr, _, cacheBase := newTestManager(t, builder)

	prepared, err := mgr.Prepare(context.Background(), "", "iOS 18.0")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if prepared.XCTestRunPath == "" {
		t.Fatalf("XCTestRunPath should be set")
	}

	if prepared.CacheKey == "" {
		t.Fatalf("CacheKey should be set")
	}

	paths := PathsFor(cacheBase, prepared.CacheKey)
	if _, err := os.Stat(paths.ExtractOK); err != nil {
		t.Errorf("ExtractOK marker missing: %v", err)
	}

	if _, err := os.Stat(paths.BuildOK); err != nil {
		t.Errorf("BuildOK marker missing: %v", err)
	}

	if _, err := os.Stat(filepath.Join(paths.Source, "TalesAppleDriver.xcodeproj", "project.pbxproj")); err != nil {
		t.Errorf("source not extracted: %v", err)
	}

	meta, err := os.ReadFile(paths.Metadata)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(meta, &parsed); err != nil {
		t.Fatalf("parse metadata: %v", err)
	}

	if parsed["source_hash"] == "" {
		t.Errorf("metadata missing source_hash")
	}

	if parsed["ios_runtime"] != "iOS 18.0" {
		t.Errorf("metadata ios_runtime = %q, want iOS 18.0", parsed["ios_runtime"])
	}
}

func TestManagerPrepareReusesBuildOK(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	mgr, _, _ := newTestManager(t, builder)

	if _, err := mgr.Prepare(context.Background(), "", "iOS 18.0"); err != nil {
		t.Fatalf("first Prepare: %v", err)
	}

	if _, err := mgr.Prepare(context.Background(), "", "iOS 18.0"); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}

	if calls := atomic.LoadInt32(&builder.calls); calls != 1 {
		t.Fatalf("expected builder to run once (cached on second call), got %d", calls)
	}
}

func TestManagerInvalidateBuildForcesRebuild(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	mgr, _, _ := newTestManager(t, builder)

	prepared, err := mgr.Prepare(context.Background(), "", "iOS 18.0")
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}

	if err := mgr.InvalidateBuild(prepared.CacheKey); err != nil {
		t.Fatalf("InvalidateBuild: %v", err)
	}

	if _, err := mgr.Prepare(context.Background(), "", "iOS 18.0"); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}

	if calls := atomic.LoadInt32(&builder.calls); calls != 2 {
		t.Fatalf("expected builder to run twice after invalidate, got %d", calls)
	}
}

func TestManagerConcurrentPrepareSerializes(t *testing.T) {
	t.Parallel()

	var active int32
	var maxActive int32

	builder := &fakeBuilder{
		hook: func(_, derivedDataPath, logPath string) (string, error) {
			cur := atomic.AddInt32(&active, 1)
			defer atomic.AddInt32(&active, -1)

			for {
				m := atomic.LoadInt32(&maxActive)
				if cur <= m || atomic.CompareAndSwapInt32(&maxActive, m, cur) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)

			xctestrun := filepath.Join(derivedDataPath, "Build", "Products", "TalesAppleDriverUITests_Debug.xctestrun")
			if err := os.MkdirAll(filepath.Dir(xctestrun), 0o755); err != nil {
				return "", err
			}

			if err := os.WriteFile(xctestrun, []byte("xml"), 0o600); err != nil {
				return "", err
			}

			if err := os.WriteFile(logPath, []byte("ok"), 0o600); err != nil {
				return "", err
			}

			return xctestrun, nil
		},
	}

	mgr, _, _ := newTestManager(t, builder)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := mgr.Prepare(context.Background(), "", "iOS 18.0"); err != nil {
				t.Errorf("Prepare: %v", err)
			}
		})
	}

	wg.Wait()

	if m := atomic.LoadInt32(&maxActive); m != 1 {
		t.Fatalf("expected at most 1 concurrent build, observed %d", m)
	}

	if calls := atomic.LoadInt32(&builder.calls); calls != 1 {
		t.Fatalf("expected exactly one build call (cache hit on later calls), got %d", calls)
	}
}

func TestManagerPrepareSourceOverride(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}
	mgr, _, _ := newTestManager(t, builder)

	override := t.TempDir()
	if err := os.MkdirAll(filepath.Join(override, "TalesAppleDriver.xcodeproj"), 0o755); err != nil {
		t.Fatalf("mkdir override project: %v", err)
	}

	if err := os.WriteFile(filepath.Join(override, "TalesAppleDriver.xcodeproj", "project.pbxproj"), []byte("override"), 0o600); err != nil {
		t.Fatalf("write override pbxproj: %v", err)
	}

	if err := os.WriteFile(filepath.Join(override, "Sources.swift"), []byte("swift override"), 0o600); err != nil {
		t.Fatalf("write override swift: %v", err)
	}

	prepared, err := mgr.Prepare(context.Background(), override, "iOS 18.0")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if prepared.SourceHash == "" {
		t.Fatalf("expected source hash to be set")
	}

	if _, err := os.Stat(filepath.Join(prepared.SourceDir, "Sources.swift")); err != nil {
		t.Errorf("override source not extracted: %v", err)
	}
}

func TestManagerPrepareFailsWithoutBuilder(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, nil)

	if _, err := mgr.Prepare(context.Background(), "", "iOS 18.0"); err == nil {
		t.Fatalf("expected error when Builder is nil")
	}
}

func TestManagerPrepareRejectsEmptyCacheBase(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, &fakeBuilder{})
	mgr.CacheBase = ""

	_, err := mgr.Prepare(context.Background(), "", "iOS 18.0")
	if err == nil {
		t.Fatalf("expected error when CacheBase is empty")
	}

	if !contains(err.Error(), "cache base") {
		t.Errorf("expected error to mention cache base, got %q", err.Error())
	}
}
