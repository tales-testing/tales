// Package artifacts holds filesystem helpers shared by every provider that
// writes per-step / per-action artifacts (mobile screenshots, browser DOM
// dumps, etc.). The path layout is collision-resistant: scenario directory
// names mix the scenario name with a hash over (file, scenario) so identical
// names from different files do not clash.
package artifacts

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultBase is the prefix used when callers do not override the artifact
// root via CLI flags or environment.
const DefaultBase = "build/artifacts"

// DefaultPhase is the phase name used when callers do not provide one. The
// runtime currently distinguishes "step" and "teardown" phases.
const DefaultPhase = "step"

const unnamedSegment = "unnamed"

var unsafePath = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// SafePathSegment turns a free-form name (scenario, step, kind, selector)
// into a filesystem-safe segment.
func SafePathSegment(in string) string {
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

// Hash returns a stable short hash (8 hex chars) over the given parts. A
// zero byte is inserted between parts so the hash is robust against parts
// that happen to concatenate into the same string.
func Hash(parts ...string) string {
	h := fnv.New64a()

	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}

	return fmt.Sprintf("%08x", h.Sum64())[:8]
}

// Dir returns a stable, collision-resistant directory under base where the
// given provider may write the artifacts for one (scenario, step, phase,
// attempt) tuple. The provider name is embedded as a path segment so two
// providers writing under the same base do not collide.
//
//	<base>/<provider>/<scenario-safe>-<hash(file,scenario)>/<step-safe>/<phase>/attempt-<N>/
func Dir(base, providerName, file, scenario, step, phase string, attempt int) string {
	if base == "" {
		base = DefaultBase
	}

	if phase == "" {
		phase = DefaultPhase
	}

	if attempt <= 0 {
		attempt = 1
	}

	return filepath.Join(
		base,
		SafePathSegment(providerName),
		fmt.Sprintf("%s-%s", SafePathSegment(scenario), Hash(file, scenario)),
		SafePathSegment(step),
		SafePathSegment(phase),
		fmt.Sprintf("attempt-%d", attempt),
	)
}
