package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommandBareNameUsesPath(t *testing.T) {
	t.Parallel()
	requireSh(t)

	resolved, err := resolveCommand("sh", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveCommand: %v", err)
	}

	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute resolved path, got %q", resolved)
	}
}

func TestResolveCommandRelativeUnderProjectDir(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	bin := filepath.Join(project, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tool := filepath.Join(bin, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test fixture executable
		t.Fatalf("write tool: %v", err)
	}

	resolved, err := resolveCommand("./bin/tool", t.TempDir(), project)
	if err != nil {
		t.Fatalf("resolveCommand: %v", err)
	}

	if resolved != tool {
		t.Fatalf("resolved = %q, want %q", resolved, tool)
	}
}

func TestResolveCommandAbsoluteOutsideRootsRejected(t *testing.T) {
	t.Parallel()

	// A system binary path escapes the allowed roots and must be rejected;
	// system tools should be referenced by bare name instead.
	if _, err := resolveCommand("/usr/bin/python3", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("expected absolute system path to be rejected")
	}

	if _, err := resolveCommand("/tmp/tool", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("expected /tmp/tool to be rejected")
	}
}

func TestResolveCommandAbsoluteUnderProjectDirAllowed(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	tool := filepath.Join(project, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test fixture executable
		t.Fatalf("write tool: %v", err)
	}

	resolved, err := resolveCommand(tool, t.TempDir(), project)
	if err != nil {
		t.Fatalf("resolveCommand: %v", err)
	}

	if resolved != tool {
		t.Fatalf("resolved = %q, want %q", resolved, tool)
	}
}

func TestResolveCommandRelativeTraversalRejected(t *testing.T) {
	t.Parallel()

	if _, err := resolveCommand("../escape/tool", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("expected relative traversal to be rejected")
	}
}

func TestBuildEnvMinimal(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()

	env, err := buildEnv("minimal", workdir, map[string]string{"CUSTOM": "1"})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	got := envMap(env)

	if got["HOME"] != workdir {
		t.Fatalf("HOME = %q, want %q", got["HOME"], workdir)
	}

	if got["TMPDIR"] != filepath.Join(workdir, "tmp") {
		t.Fatalf("TMPDIR = %q", got["TMPDIR"])
	}

	if got["CUSTOM"] != "1" {
		t.Fatal("expected user-provided CUSTOM to be present")
	}

	if _, ok := got["PATH"]; !ok {
		t.Fatal("expected PATH in minimal env")
	}

	// Minimal must not leak arbitrary host variables.
	if _, leaked := got["HOSTNAME_TALES_SHOULD_NOT_LEAK"]; leaked {
		t.Fatal("unexpected host var leaked into minimal env")
	}
}

func TestBuildEnvInheritIncludesHostVar(t *testing.T) {
	t.Setenv("TALES_EXEC_TEST_VAR", "present")

	env, err := buildEnv("inherit", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	if envMap(env)["TALES_EXEC_TEST_VAR"] != "present" {
		t.Fatal("expected inherited host var in inherit env")
	}
}

func TestBuildEnvUserOverridesBase(t *testing.T) {
	t.Setenv("TALES_EXEC_OVERRIDE", "host")

	env, err := buildEnv("inherit", t.TempDir(), map[string]string{"TALES_EXEC_OVERRIDE": "user"})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}

	if envMap(env)["TALES_EXEC_OVERRIDE"] != "user" {
		t.Fatal("user env must override the base value")
	}
}

func TestCappedBuffer(t *testing.T) {
	t.Parallel()

	b := &cappedBuffer{limit: 3}

	n, _ := b.Write([]byte("abcdef"))
	if n != 6 {
		t.Fatalf("Write returned %d, want 6 (full claim)", n)
	}

	if string(b.bytes()) != "abc" {
		t.Fatalf("capped content = %q, want abc", string(b.bytes()))
	}

	if !b.truncated {
		t.Fatal("expected truncated = true")
	}
}

func envMap(env []string) map[string]string {
	out := map[string]string{}

	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}

	return out
}
