package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

func TestArtifactDirIncludesFileHashToAvoidCollisions(t *testing.T) {
	t.Parallel()

	left := artifactDir("build/artifacts", "e2e/ios/pass/register.tales", "Same Name", "tap", "step", 1)
	right := artifactDir("build/artifacts", "e2e/ios/fail/register.tales", "Same Name", "tap", "step", 1)

	if left == right {
		t.Fatalf("expected different files with same scenario to get unique dirs: %q", left)
	}

	if !strings.Contains(left, "Same_Name-") || !strings.Contains(left, filepath.Join("tap", "step", "attempt-1")) {
		t.Fatalf("expected sanitized scenario, step, phase, and attempt in path, got %q", left)
	}
}

func TestArtifactDirDefaultsAndSanitizesSegments(t *testing.T) {
	t.Parallel()

	dir := artifactDir("", "weird file.tales", " / Demo: iOS! ", " tap/register ", "teardown phase", 0)

	if !strings.HasPrefix(dir, filepath.Join(defaultArtifactsBase, "mobile")) {
		t.Fatalf("expected default artifacts base, got %q", dir)
	}

	if strings.Contains(dir, " ") || strings.Contains(dir, ":") || strings.Contains(dir, "!") {
		t.Fatalf("expected unsafe characters to be sanitized, got %q", dir)
	}

	if !strings.Contains(dir, "attempt-1") {
		t.Fatalf("expected invalid attempt to normalize to attempt-1, got %q", dir)
	}
}

func TestArtifactWritersCanOverwriteSameAttempt(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "artifacts")
	node := &tree.ViewNode{ID: "root", Visible: true}

	if _, err := writeScreenshot(dir, []byte("png-1")); err != nil {
		t.Fatalf("write screenshot first time: %v", err)
	}
	if _, err := writeScreenshot(dir, []byte("png-2")); err != nil {
		t.Fatalf("write screenshot second time: %v", err)
	}
	if _, err := writeHierarchy(dir, node); err != nil {
		t.Fatalf("write hierarchy first time: %v", err)
	}
	if _, err := writeHierarchy(dir, node); err != nil {
		t.Fatalf("write hierarchy second time: %v", err)
	}

	png, err := os.ReadFile(filepath.Join(dir, "screenshot.png"))
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	if string(png) != "png-2" {
		t.Fatalf("expected second screenshot to overwrite first, got %q", string(png))
	}
}
