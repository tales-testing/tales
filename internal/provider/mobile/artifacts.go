package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
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

// defaultArtifactsBase is the prefix used when callers do not override it.
const defaultArtifactsBase = "build/artifacts"

const artifactDefaultPhase = "step"

var unsafePath = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// unnamedSegment is the placeholder used when a scenario or step name is empty.
const unnamedSegment = "unnamed"

// safePathSegment turns a scenario / step name into a filesystem-safe segment.
func safePathSegment(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return unnamedSegment
	}

	out := strings.Trim(unsafePath.ReplaceAllString(s, "_"), "_")
	if out == "" {
		return unnamedSegment
	}

	return out
}

// artifactDir returns a stable, collision-resistant artifact directory.
func artifactDir(base, file, scenario, step, phase string, attempt int) string {
	if base == "" {
		base = defaultArtifactsBase
	}

	if phase == "" {
		phase = artifactDefaultPhase
	}

	if attempt <= 0 {
		attempt = 1
	}

	return filepath.Join(
		base,
		"mobile",
		fmt.Sprintf("%s-%s", safePathSegment(scenario), artifactHash(file, scenario)),
		safePathSegment(step),
		safePathSegment(phase),
		fmt.Sprintf("attempt-%d", attempt),
	)
}

func artifactHash(parts ...string) string {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}

	return fmt.Sprintf("%08x", h.Sum64())[:8]
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
