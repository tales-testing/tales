package appledriver

import (
	"io/fs"
	"testing"
)

func TestFSContainsDriverSource(t *testing.T) {
	t.Parallel()

	want := []string{
		"TalesAppleDriver/TalesAppleDriver.xcodeproj/project.pbxproj",
		"TalesAppleDriver/TalesAppleDriverUITests/TalesAppleDriverUITests.swift",
		"TalesAppleDriver/TalesAppleDriverUITests/HTTPServer.swift",
		"TalesAppleDriver/TalesAppleDriverHost/TalesAppleDriverHostApp.swift",
	}

	for _, path := range want {
		info, err := fs.Stat(FS(), path)
		if err != nil {
			t.Errorf("embedded driver source missing %q: %v", path, err)

			continue
		}

		if info.IsDir() {
			t.Errorf("%q should be a file, got directory", path)
		}

		if info.Size() == 0 {
			t.Errorf("%q is empty in the embedded FS", path)
		}
	}
}

func TestFSRootMatchesConstant(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(FS(), SourceRoot)
	if err != nil {
		t.Fatalf("read %q: %v", SourceRoot, err)
	}

	if len(entries) == 0 {
		t.Fatalf("embedded source root %q is empty", SourceRoot)
	}
}
