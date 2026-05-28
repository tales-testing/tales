package browser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tales-testing/tales/internal/provider/artifacts"
)

// Artifact lists a single file produced by the browser provider for the
// current step.
type Artifact struct {
	Type string
	Path string
}

const (
	artifactKindScreenshot = "screenshot"
	artifactKindDOM        = "dom"
)

// stepArtifactDir is the per-attempt directory for one (scenario, step,
// phase, attempt) tuple. Mirrors mobile's layout.
func stepArtifactDir(base, file, scenario, step, phase string, attempt int) string {
	return artifacts.Dir(base, "browser", file, scenario, step, phase, attempt)
}

// actionArtifactDir returns the directory under stepDir where the artifacts
// for one browser action are written. The layout is:
//
//	<stepDir>/actions/NNNN-<kind>-<safe(selector)>/
//
// index is zero-padded to 4 digits; kind and selector are sanitized.
func actionArtifactDir(stepDir string, index int, kind, selector string) string {
	segment := fmt.Sprintf("%04d-%s-%s", index, artifacts.SafePathSegment(kind), artifacts.SafePathSegment(selector))

	return filepath.Join(stepDir, "actions", segment)
}

// stepLevelArtifactDir returns the directory used for the end-of-step
// snapshot.
func stepLevelArtifactDir(stepDir string) string {
	return filepath.Join(stepDir, "step")
}

// writeScreenshot writes PNG bytes to <dir>/screenshot.png. Empty input or
// directory creation errors short-circuit to (zero, err).
func writeScreenshot(dir string, png []byte) (Artifact, error) {
	if len(png) == 0 {
		return Artifact{}, fmt.Errorf("screenshot bytes are empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	path := filepath.Join(dir, "screenshot.png")

	if err := os.WriteFile(path, png, 0o600); err != nil {
		return Artifact{}, fmt.Errorf("write screenshot: %w", err)
	}

	return Artifact{Type: artifactKindScreenshot, Path: path}, nil
}

// writeDOM serializes the given HTML string to <dir>/dom.html.
func writeDOM(dir string, htmlStr string) (Artifact, error) {
	if htmlStr == "" {
		return Artifact{}, fmt.Errorf("dom is empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	path := filepath.Join(dir, "dom.html")

	if err := os.WriteFile(path, []byte(htmlStr), 0o600); err != nil {
		return Artifact{}, fmt.Errorf("write dom: %w", err)
	}

	return Artifact{Type: artifactKindDOM, Path: path}, nil
}
