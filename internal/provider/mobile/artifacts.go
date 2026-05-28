package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tales-testing/tales/internal/provider/artifacts"
	"github.com/tales-testing/tales/internal/provider/mobile/tree"
)

// Artifact lists a single file produced by the provider for the current step.
type Artifact struct {
	Type string
	Path string
}

// artifactKindScreenshot identifies PNG screenshot artifacts.
const artifactKindScreenshot = "screenshot"

// artifactKindHierarchy identifies JSON hierarchy artifacts.
const artifactKindHierarchy = "hierarchy"

// safePathSegment delegates to the shared sanitizer; kept as a private alias
// so the existing mobile call sites stay terse.
func safePathSegment(in string) string {
	return artifacts.SafePathSegment(in)
}

// artifactDir returns a stable, collision-resistant artifact directory for
// the mobile provider via the shared artifacts helper.
func artifactDir(base, file, scenario, step, phase string, attempt int) string {
	return artifacts.Dir(base, "mobile", file, scenario, step, phase, attempt)
}

// actionArtifactDir returns the directory under stepDir where the artifacts
// for one UI action are written. The layout nests under the existing mobile
// scenario/step/phase/attempt directory so screenshots stay grouped with the
// failure-path artifacts they belong to.
//
//	<stepDir>/actions/NNNN-<kind>-<safe(id)>/
//
// index is zero-padded to 4 digits. kind and id are sanitized via
// safePathSegment so user-supplied IDs cannot escape the artifacts root.
func actionArtifactDir(stepDir string, index int, kind, id string) string {
	segment := fmt.Sprintf("%04d-%s-%s", index, safePathSegment(kind), safePathSegment(id))

	return filepath.Join(stepDir, "actions", segment)
}

// stepLevelArtifactDir returns the directory used for the synthetic
// end-of-step capture in CaptureSteps mode. It lives next to the per-action
// directories so the artifacts root remains a single tree.
//
//	<stepDir>/step/
func stepLevelArtifactDir(stepDir string) string {
	return filepath.Join(stepDir, "step")
}

// writeScreenshot writes PNG bytes to <dir>/screenshot.png and returns its
// path. Empty input or directory creation errors short-circuit to (nil, err).
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

// writeScreenshotFallback uses the Apple lifecycle (simctl io screenshot) to
// capture a PNG directly to <dir>/screenshot.png when the driver-side
// screenshot endpoint is unavailable. Returns an Artifact pointing at the
// written file. The session's Lifecycle must be set for this to succeed.
func writeScreenshotFallback(ctx context.Context, dir string, session *Session) (Artifact, error) {
	if session == nil || session.Lifecycle == nil || session.UDID == "" {
		return Artifact{}, fmt.Errorf("screenshot fallback: session not ready")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	path := filepath.Join(dir, "screenshot.png")

	if err := session.Lifecycle.ScreenshotFallback(ctx, session.UDID, path); err != nil {
		return Artifact{}, fmt.Errorf("screenshot fallback: %w", err)
	}

	return Artifact{Type: artifactKindScreenshot, Path: path}, nil
}

// writeHierarchy serializes the given tree to <dir>/hierarchy.json.
func writeHierarchy(dir string, node *tree.ViewNode) (Artifact, error) {
	if node == nil {
		return Artifact{}, fmt.Errorf("hierarchy is nil")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	data, err := json.MarshalIndent(node, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal hierarchy: %w", err)
	}

	path := filepath.Join(dir, "hierarchy.json")

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Artifact{}, fmt.Errorf("write hierarchy: %w", err)
	}

	return Artifact{Type: artifactKindHierarchy, Path: path}, nil
}
