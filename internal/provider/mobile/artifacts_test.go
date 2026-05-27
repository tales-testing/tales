package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider/mobile/tree"
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

func TestActionArtifactDirZeroPadsIndexAndSanitizesIDs(t *testing.T) {
	t.Parallel()

	step := filepath.Join("build", "artifacts", "mobile", "scenario-aabbccdd", "submit", "step", "attempt-1")

	cases := []struct {
		name        string
		index       int
		kind        string
		id          string
		mustContain []string
		mustReject  []string
	}{
		{
			name:        "basic",
			index:       0,
			kind:        "tap",
			id:          "login_button",
			mustContain: []string{filepath.Join(step, "actions", "0000-tap-login_button")},
		},
		{
			name:        "large index pads to 4 digits",
			index:       42,
			kind:        "input_text",
			id:          "register.email",
			mustContain: []string{filepath.Join(step, "actions", "0042-input_text-register.email")},
		},
		{
			name:        "flattens path traversal under one segment",
			index:       3,
			kind:        "tap",
			id:          "../../../etc/passwd",
			mustContain: []string{filepath.Join(step, "actions", "0003-tap-")},
			mustReject:  []string{string(filepath.Separator) + "etc"},
		},
		{
			name:        "rejects spaces and colons",
			index:       7,
			kind:        "tap",
			id:          " bad : id ",
			mustContain: []string{filepath.Join(step, "actions", "0007-tap-bad_id")},
			mustReject:  []string{" ", ":"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := actionArtifactDir(step, tc.index, tc.kind, tc.id)
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("actionArtifactDir(...) = %q, expected to contain %q", got, want)
				}
			}

			for _, reject := range tc.mustReject {
				suffix := strings.TrimPrefix(got, step)
				if strings.Contains(suffix, reject) {
					t.Errorf("actionArtifactDir(...) = %q, expected to NOT contain %q in action segment %q", got, reject, suffix)
				}
			}
		})
	}
}

func TestStepLevelArtifactDirNestsUnderStep(t *testing.T) {
	t.Parallel()

	step := filepath.Join("build", "artifacts", "mobile", "scenario-aabbccdd", "submit", "step", "attempt-1")

	got := stepLevelArtifactDir(step)
	want := filepath.Join(step, "step")

	if got != want {
		t.Fatalf("stepLevelArtifactDir(%q) = %q, want %q", step, got, want)
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
