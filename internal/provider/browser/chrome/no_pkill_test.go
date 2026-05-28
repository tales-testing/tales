package chrome_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProductionCodeNeverInvokesPkill is a guard test pinning the
// isolation guarantee documented in builder.go: the browser provider
// (and the tales binary at large) must never call out to `pkill`, `kill
// -9`, or any other name-based process matcher. Process termination is
// PID-scoped through chromedp's context cancellation only.
//
// Catching this at test time avoids accidental regressions that could
// kill the user's regular Chrome browser when they invoke `tales test`.
func TestProductionCodeNeverInvokesPkill(t *testing.T) {
	t.Parallel()

	roots := []string{
		repoPath(t, "internal"),
		repoPath(t, "cmd"),
		repoPath(t, "drivers"),
	}

	// Allowed: comments / strings that document the absence of pkill.
	// We grep the .go source and reject any line that actually calls
	// these binaries — comments are fine.
	forbidden := []string{`"pkill"`, "`pkill`", `exec.Command("pkill"`, `exec.Command("killall"`}

	for _, root := range roots {
		walk(t, root, forbidden)
	}
}

func walk(t *testing.T, root string, forbidden []string) {
	t.Helper()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, rerr := os.ReadFile(path) //nolint:gosec // walking a known repo subtree; safe.
		if rerr != nil {
			return rerr
		}

		for _, needle := range forbidden {
			if strings.Contains(string(data), needle) {
				t.Errorf("%s contains forbidden process-killing pattern %q; use context.CancelFunc / PID-scoped termination instead", path, needle)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func repoPath(t *testing.T, relative string) string {
	t.Helper()

	// Walk up from the test's CWD to find the repo root, then join.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, relative)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod) from %s", dir)
		}

		dir = parent
	}
}
