package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newResolver(t *testing.T) (Resolver, string, string) {
	t.Helper()

	root := t.TempDir()
	workdir := filepath.Join(root, "workdir")
	project := filepath.Join(root, "project")

	for _, dir := range []string{workdir, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	return Resolver{Workdir: workdir, ProjectDir: project}, workdir, project
}

func TestResolveOutputRelativeUnderWorkdir(t *testing.T) {
	t.Parallel()

	r, workdir, _ := newResolver(t)

	got, err := r.ResolveOutput("downloads/cert.pdf")
	if err != nil {
		t.Fatalf("ResolveOutput error: %v", err)
	}

	want := filepath.Join(workdir, "downloads", "cert.pdf")
	if got != want {
		t.Fatalf("ResolveOutput = %q, want %q", got, want)
	}
}

func TestResolveOutputAbsoluteWithinWorkdir(t *testing.T) {
	t.Parallel()

	r, workdir, _ := newResolver(t)

	abs := filepath.Join(workdir, "sub", "file.bin")

	got, err := r.ResolveOutput(abs)
	if err != nil {
		t.Fatalf("ResolveOutput error: %v", err)
	}

	if got != abs {
		t.Fatalf("ResolveOutput = %q, want %q", got, abs)
	}
}

func TestResolveOutputRejectsTraversal(t *testing.T) {
	t.Parallel()

	r, _, _ := newResolver(t)

	_, err := r.ResolveOutput("../outside")
	if err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	if want := `path "../outside" escapes the scenario workspace`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestResolveOutputRejectsNestedTraversal(t *testing.T) {
	t.Parallel()

	r, _, _ := newResolver(t)

	if _, err := r.ResolveOutput("a/b/../../../etc/passwd"); err == nil {
		t.Fatal("expected nested traversal to be rejected")
	}
}

func TestResolveOutputRejectsAbsoluteOutside(t *testing.T) {
	t.Parallel()

	r, _, project := newResolver(t)

	// The project dir is a read-only root: writes there are not allowed.
	if _, err := r.ResolveOutput(filepath.Join(project, "x")); err == nil {
		t.Fatal("expected absolute path outside workdir to be rejected for output")
	}

	if _, err := r.ResolveOutput("/etc/passwd"); err == nil {
		t.Fatal("expected /etc/passwd to be rejected")
	}
}

func TestResolveInputRelativeUnderWorkdir(t *testing.T) {
	t.Parallel()

	r, workdir, _ := newResolver(t)

	got, err := r.ResolveInput("downloads/cert.pdf")
	if err != nil {
		t.Fatalf("ResolveInput error: %v", err)
	}

	if want := filepath.Join(workdir, "downloads", "cert.pdf"); got != want {
		t.Fatalf("ResolveInput = %q, want %q", got, want)
	}
}

func TestResolveInputAbsoluteUnderProjectDir(t *testing.T) {
	t.Parallel()

	r, _, project := newResolver(t)

	// Mirrors ${project.dir}/fixtures/document.pdf, which evaluates to an
	// absolute path under the project root before the resolver sees it.
	abs := filepath.Join(project, "fixtures", "document.pdf")

	got, err := r.ResolveInput(abs)
	if err != nil {
		t.Fatalf("ResolveInput error: %v", err)
	}

	if got != abs {
		t.Fatalf("ResolveInput = %q, want %q", got, abs)
	}
}

func TestResolveInputRejectsOutsideBothRoots(t *testing.T) {
	t.Parallel()

	r, _, _ := newResolver(t)

	if _, err := r.ResolveInput("/etc/passwd"); err == nil {
		t.Fatal("expected /etc/passwd to be rejected for input")
	}

	if _, err := r.ResolveInput("../../outside"); err == nil {
		t.Fatal("expected traversal to be rejected for input")
	}
}

// TestResolveOutputRejectsSymlinkEscape verifies the best-effort symlink
// canonicalization: a directory inside the workspace that symlinks to a
// location outside both roots cannot be used to escape.
func TestResolveOutputRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}

	r, workdir, _ := newResolver(t)

	outside := t.TempDir()

	link := filepath.Join(workdir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Structurally "escape/x" is under workdir, but escape resolves outside.
	if _, err := r.ResolveOutput("escape/x"); err == nil {
		t.Fatal("expected symlinked escape to be rejected")
	}
}

// TestResolveOutputAllowsInternalSymlink confirms a symlink that stays within
// the workspace is still accepted.
func TestResolveOutputAllowsInternalSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}

	r, workdir, _ := newResolver(t)

	realDir := filepath.Join(workdir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	link := filepath.Join(workdir, "alias")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := r.ResolveOutput("alias/file.bin")
	if err != nil {
		t.Fatalf("ResolveOutput error: %v", err)
	}

	if !strings.HasPrefix(got, workdir) {
		t.Fatalf("ResolveOutput = %q, expected under %q", got, workdir)
	}
}
