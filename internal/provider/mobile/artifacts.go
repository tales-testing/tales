package mobile

import (
	"encoding/json"
	"fmt"
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

var unsafePath = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// unnamedSegment is the placeholder used when a scenario or step name is empty.
const unnamedSegment = "unnamed"

// safePathSegment turns a scenario / step name into a filesystem-safe segment.
func safePathSegment(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return unnamedSegment
	}

	return strings.Trim(unsafePath.ReplaceAllString(s, "_"), "_")
}

// artifactDir returns the directory artifacts for the given scenario/step go
// under `<base>/<scenario>/<step>/`.
func artifactDir(base, scenario, step string) string {
	if base == "" {
		base = defaultArtifactsBase
	}

	return filepath.Join(base, safePathSegment(scenario), safePathSegment(step))
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
