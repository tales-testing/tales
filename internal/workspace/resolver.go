// Package workspace resolves user-supplied paths against the allowed roots of
// one scenario: the per-scenario workspace directory (read-write) and the
// project directory (read-only). It is the single chokepoint shared by the
// HTTP save block, the file provider, and the exec provider so traversal
// outside the allowed roots is rejected consistently.
//
// Symlink handling is best-effort: the resolver canonicalizes the deepest
// existing ancestor of the target via filepath.EvalSymlinks and re-checks
// containment, which defeats a symlinked directory that points outside the
// roots at resolve time. It does NOT defend against a symlink created
// between resolution and use (a TOCTOU window), and it cannot inspect a path
// whose ancestors do not yet exist. Treat the resolver as a guardrail against
// accidental traversal, not as a security sandbox.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolver holds the allowed roots for one scenario. Both paths must be
// absolute and cleaned by the caller (the runtime builds them with
// filepath.Abs). The zero value is unusable.
type Resolver struct {
	// Workdir is the per-scenario workspace root. Relative paths resolve
	// under it and it is the only root writes may target.
	Workdir string
	// ProjectDir is the project / repository root. Reads may target it (for
	// fixtures referenced via ${project.dir}/...), writes may not.
	ProjectDir string
}

// ResolveOutput resolves a path for writing (HTTP save bodies, exec
// artifacts). Relative paths resolve under Workdir; absolute paths are
// accepted only when they stay within Workdir. Anything escaping Workdir —
// traversal or an absolute path elsewhere — is rejected.
func (r Resolver) ResolveOutput(p string) (string, error) {
	target := cleanAbs(r.Workdir, p)

	ok, err := contained(r.Workdir, target)
	if err != nil {
		return "", err
	}

	if !ok {
		return "", escapeError(p)
	}

	return target, nil
}

// ResolveInput resolves a path for reading (the file provider). Relative
// paths resolve under Workdir; absolute paths are accepted when they stay
// within Workdir or ProjectDir, which is what ${project.dir}/fixtures/x
// expands to. Anything outside both roots is rejected.
func (r Resolver) ResolveInput(p string) (string, error) {
	target := cleanAbs(r.Workdir, p)

	for _, root := range []string{r.Workdir, r.ProjectDir} {
		if root == "" {
			continue
		}

		ok, err := contained(root, target)
		if err != nil {
			return "", err
		}

		if ok {
			return target, nil
		}
	}

	return "", escapeError(p)
}

// Contains reports whether target (an absolute, cleaned path) resolves within
// root, applying the same best-effort symlink canonicalization as the
// resolver. The exec provider uses it to enforce the command-path policy
// (absolute commands must live under the scenario workdir or the project dir).
func Contains(root, target string) (bool, error) {
	return contained(root, target)
}

// cleanAbs turns a possibly-relative path into a cleaned absolute path under
// base. Relative paths are joined onto base first; absolute paths are cleaned
// as-is. filepath.Join already cleans, so this collapses ../ segments before
// the containment check sees them.
func cleanAbs(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}

	return filepath.Join(base, p)
}

// contained reports whether target is base itself or lives somewhere beneath
// it, after canonicalizing symlinks on the deepest existing ancestor. The
// relationship is computed structurally with filepath.Rel and then re-checked
// against the symlink-resolved form so a symlinked ancestor cannot smuggle the
// path outside base.
func contained(base, target string) (bool, error) {
	if structurallyInside, err := relInside(base, target); err != nil || !structurallyInside {
		return false, err
	}

	resolvedBase := resolveExisting(base)
	resolvedTarget := resolveExisting(target)

	return relInside(resolvedBase, resolvedTarget)
}

// relInside reports whether target is base or nested under it, using the
// cleaned relative path: a "." result means equal, a ".." prefix means the
// target climbs above base.
func relInside(base, target string) (bool, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, fmt.Errorf("compute relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}

	return true, nil
}

// resolveExisting canonicalizes the deepest existing ancestor of p with
// EvalSymlinks and re-attaches the not-yet-created suffix. It never errors:
// when nothing along the path exists (or EvalSymlinks fails) it returns the
// cleaned input, leaving the purely-structural check from relInside in force.
func resolveExisting(p string) string {
	current := filepath.Clean(p)

	var suffix []string

	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(append([]string{resolved}, suffix...)...)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(p)
		}

		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func escapeError(original string) error {
	return fmt.Errorf("path %q escapes the scenario workspace", original)
}
