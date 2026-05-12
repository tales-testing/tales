package embeddeddriver

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExtractWritesNestedFiles(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"src/TalesAppleDriver.xcodeproj/project.pbxproj":              {Data: []byte("project")},
		"src/TalesAppleDriverUITests/HTTPServer.swift":                {Data: []byte("http")},
		"src/TalesAppleDriverUITests/Subdir/Handlers.swift":           {Data: []byte("handlers")},
		"src/TalesAppleDriver.xcodeproj/xcshareddata/sample.xcscheme": {Data: []byte("scheme")},
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := Extract(fsys, "src", dst); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, rel := range []string{
		"TalesAppleDriver.xcodeproj/project.pbxproj",
		"TalesAppleDriverUITests/HTTPServer.swift",
		"TalesAppleDriverUITests/Subdir/Handlers.swift",
		"TalesAppleDriver.xcodeproj/xcshareddata/sample.xcscheme",
	} {
		full := filepath.Join(dst, rel)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("missing %q: %v", rel, err)

			continue
		}

		if info.IsDir() {
			t.Errorf("%q should be file, got dir", rel)
		}
	}

	tmp := dst + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp dir should be cleaned up after rename, got err=%v", err)
	}
}

func TestExtractIsAtomicOnReExtract(t *testing.T) {
	t.Parallel()

	fsys1 := fstest.MapFS{
		"src/file.txt": {Data: []byte("v1")},
	}
	fsys2 := fstest.MapFS{
		"src/file.txt":    {Data: []byte("v2")},
		"src/new_file.md": {Data: []byte("new")},
	}

	dst := filepath.Join(t.TempDir(), "out")

	if err := Extract(fsys1, "src", dst); err != nil {
		t.Fatalf("first extract: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dst, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := Extract(fsys2, "src", dst); err != nil {
		t.Fatalf("second extract: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed during re-extract, err=%v", err)
	}

	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}

	if string(data) != "v2" {
		t.Errorf("expected updated content v2, got %q", data)
	}

	if _, err := os.Stat(filepath.Join(dst, "new_file.md")); err != nil {
		t.Errorf("new file should exist after re-extract: %v", err)
	}
}

func TestExtractRecoversFromStaleTmp(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"src/file.txt": {Data: []byte("clean")},
	}

	dst := filepath.Join(t.TempDir(), "out")
	tmp := dst + ".tmp"
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatalf("seed tmp: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, "leaked.txt"), []byte("leak"), 0o600); err != nil {
		t.Fatalf("seed leak: %v", err)
	}

	if err := Extract(fsys, "src", dst); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "leaked.txt")); !os.IsNotExist(err) {
		t.Errorf("leaked file should not appear in final destination, err=%v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "file.txt")); err != nil {
		t.Errorf("expected file.txt in destination: %v", err)
	}
}

func TestExtractRejectsEmptyDestination(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"src/a.txt": {Data: []byte("a")},
	}

	if err := Extract(fsys, "src", ""); err == nil {
		t.Fatalf("expected error for empty destination")
	}
}

func TestExtractRejectsNilFilesystem(t *testing.T) {
	t.Parallel()

	if err := Extract(nil, "src", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatalf("expected error for nil filesystem")
	}
}
