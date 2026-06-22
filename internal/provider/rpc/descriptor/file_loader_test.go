package descriptor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLoader_LoadValidSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "descriptor.bin")

	if err := os.WriteFile(path, buildTestFileSet(t), 0o600); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	loader := &FileLoader{Path: path}

	files, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if files == nil {
		t.Fatal("Load returned nil files")
	}

	if loader.Source() != path {
		t.Errorf("Source() = %q, want %q", loader.Source(), path)
	}
}

func TestFileLoader_LoadMissingFile(t *testing.T) {
	t.Parallel()

	loader := &FileLoader{Path: filepath.Join(t.TempDir(), "nope.bin")}

	_, err := loader.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}

	if !strings.Contains(err.Error(), "read descriptor file") {
		t.Errorf("error message = %q, want containing %q", err.Error(), "read descriptor file")
	}
}

func TestFileLoader_LoadInvalidBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "descriptor.bin")

	if err := os.WriteFile(path, []byte("not a descriptor"), 0o600); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	_, err := (&FileLoader{Path: path}).Load(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid binary, got nil")
	}

	if !strings.Contains(err.Error(), "invalid descriptor file") {
		t.Errorf("error message = %q, want containing %q", err.Error(), "invalid descriptor file")
	}
}

func TestFileLoader_LoadEmptyPath(t *testing.T) {
	t.Parallel()

	_, err := (&FileLoader{}).Load(context.Background())
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}
