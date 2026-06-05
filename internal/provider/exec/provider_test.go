package exec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/provider"
)

// requireSh skips a test when /bin/sh is not resolvable on PATH (e.g. Windows
// CI). The process-spawning tests use sh to stay portable across Unix hosts.
func requireSh(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available on PATH")
	}
}

func newExecution(t *testing.T, command string, args ...string) *provider.ExecExecution {
	t.Helper()

	workdir := t.TempDir()

	return &provider.ExecExecution{
		Command:      command,
		Args:         args,
		EnvMode:      "minimal",
		Timeout:      5 * time.Second,
		SandboxMode:  "process",
		Workdir:      workdir,
		ProjectDir:   t.TempDir(),
		ArtifactsDir: filepath.Join(workdir, "exec", "step"),
	}
}

func TestDisabledByDefault(t *testing.T) {
	t.Parallel()

	_, err := New().Execute(context.Background(), provider.Input{Exec: newExecution(t, "sh", "-c", "echo hi")})
	if err == nil {
		t.Fatal("expected exec to be disabled by default")
	}

	if err.Error() != disabledMessage {
		t.Fatalf("disabled message = %q, want %q", err.Error(), disabledMessage)
	}
}

func TestDockerModeUnsupported(t *testing.T) {
	t.Parallel()

	ee := newExecution(t, "sh", "-c", "true")
	ee.SandboxMode = sandboxModeDocker

	_, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: ee})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected docker-not-implemented error, got %v", err)
	}
}

func TestRunSuccessExitZero(t *testing.T) {
	t.Parallel()
	requireSh(t)

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: newExecution(t, "sh", "-c", "echo ok")})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if out.StatusCode != 0 {
		t.Fatalf("exit_code = %d, want 0", out.StatusCode)
	}

	if got := out.Response["stdout"].GetAttr("raw").AsString(); got != "ok\n" {
		t.Fatalf("stdout = %q, want %q", got, "ok\n")
	}
}

func TestNonZeroExitIsNotAnError(t *testing.T) {
	t.Parallel()
	requireSh(t)

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: newExecution(t, "sh", "-c", "exit 3")})
	if err != nil {
		t.Fatalf("a non-zero exit must not be a provider error: %v", err)
	}

	if got, _ := out.Response["exit_code"].AsBigFloat().Int64(); got != 3 {
		t.Fatalf("exit_code = %d, want 3", got)
	}
}

func TestStdoutJSONParsed(t *testing.T) {
	t.Parallel()
	requireSh(t)

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: newExecution(t, "sh", "-c", `printf '{"valid":true}'`)})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	jsonVal := out.Response["stdout"].GetAttr("json")
	if jsonVal.IsNull() || !jsonVal.GetAttr("valid").True() {
		t.Fatalf("expected stdout.json.valid = true, got %#v", jsonVal)
	}
}

func TestStderrCaptured(t *testing.T) {
	t.Parallel()
	requireSh(t)

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: newExecution(t, "sh", "-c", "echo boom 1>&2")})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := out.Response["stderr"].GetAttr("raw").AsString(); got != "boom\n" {
		t.Fatalf("stderr = %q, want %q", got, "boom\n")
	}
}

func TestOutputTruncation(t *testing.T) {
	t.Parallel()
	requireSh(t)

	ee := newExecution(t, "sh", "-c", "printf abcdefghij")
	ee.MaxOutput = 4

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: ee})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	stdout := out.Response["stdout"]
	if got := stdout.GetAttr("raw").AsString(); got != "abcd" {
		t.Fatalf("truncated stdout = %q, want %q", got, "abcd")
	}

	if !stdout.GetAttr("truncated").True() {
		t.Fatal("expected stdout.truncated = true")
	}
}

func TestTimeoutFailsAndRecordsMetadata(t *testing.T) {
	t.Parallel()
	requireSh(t)

	ee := newExecution(t, "sh", "-c", "sleep 5")
	ee.Timeout = 100 * time.Millisecond

	_, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: ee})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	// Artifacts (including metadata with timed_out=true) are written even on timeout.
	meta := readMetadata(t, ee.ArtifactsDir)
	if !meta.TimedOut {
		t.Fatal("expected metadata.timed_out = true")
	}
}

func TestArtifactsWrittenWithoutEnv(t *testing.T) {
	t.Parallel()
	requireSh(t)

	ee := newExecution(t, "sh", "-c", "echo hello")
	ee.Env = map[string]string{"SECRET_TOKEN": "should-not-be-persisted"}

	if _, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: ee}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	for _, name := range []string{"stdout.txt", "stderr.txt", "metadata.json"} {
		if _, err := os.Stat(filepath.Join(ee.ArtifactsDir, name)); err != nil {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(ee.ArtifactsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	if strings.Contains(string(raw), "should-not-be-persisted") || strings.Contains(string(raw), "SECRET_TOKEN") {
		t.Fatal("metadata.json must not contain environment values or keys")
	}
}

func TestNoImplicitShell(t *testing.T) {
	t.Parallel()
	requireSh(t)

	// Tales runs the program directly: the ';' is a literal echo argument, not
	// a shell command separator. echo prints it verbatim.
	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: newExecution(t, "echo", "a;b")})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := out.Response["stdout"].GetAttr("raw").AsString(); got != "a;b\n" {
		t.Fatalf("stdout = %q, want %q (no shell expansion)", got, "a;b\n")
	}
}

func TestStdinPassed(t *testing.T) {
	t.Parallel()
	requireSh(t)

	ee := newExecution(t, "cat")
	ee.Stdin = "piped input"

	out, err := New(WithAllowExec(true)).Execute(context.Background(), provider.Input{Exec: ee})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := out.Response["stdout"].GetAttr("raw").AsString(); got != "piped input" {
		t.Fatalf("stdout = %q, want %q", got, "piped input")
	}
}

func readMetadata(t *testing.T, dir string) execMetadata {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	var meta execMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	return meta
}
