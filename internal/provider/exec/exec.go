package exec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/workspace"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// nowFunc is the wall clock used to measure exec duration. exec is inherently
// non-deterministic, so this is intentionally not seedable; it is a var only so
// tests could override it if needed.
var nowFunc = time.Now

// resolveCommand applies the command-resolution policy:
//   - a bare name (no path separator) is looked up on PATH;
//   - a relative path resolves under the project dir and must stay there;
//   - an absolute path is allowed only within the scenario workdir or the
//     project dir.
//
// This forces system interpreters to be referenced by bare name (python3),
// which keeps resolution deterministic, and blocks absolute paths such as
// /tmp/tool or /usr/bin/python3 that escape the allowed roots.
func resolveCommand(command, workdir, projectDir string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	if !strings.ContainsRune(command, '/') && !strings.ContainsRune(command, filepath.Separator) {
		resolved, err := exec.LookPath(command)
		if err != nil {
			return "", fmt.Errorf("command %q not found on PATH: %w", command, err)
		}

		return resolved, nil
	}

	abs := command
	if !filepath.IsAbs(command) {
		abs = filepath.Join(projectDir, command)
	}

	abs = filepath.Clean(abs)

	for _, root := range []string{workdir, projectDir} {
		if root == "" {
			continue
		}

		ok, err := workspace.Contains(root, abs)
		if err != nil {
			return "", fmt.Errorf("check command containment: %w", err)
		}

		if ok {
			return abs, nil
		}
	}

	return "", fmt.Errorf("command path %q escapes the allowed roots (project dir or scenario workdir); reference system tools by bare name", command)
}

// buildEnv constructs the process environment. minimal (default) exposes only
// PATH, a scenario-local TMPDIR and HOME, plus the user-provided entries;
// inherit starts from the host environment. The user-provided env always wins.
// The result is sorted for determinism. Secrets in user env are intentionally
// passed through to the child but never written to artifacts.
func buildEnv(mode, workdir string, user map[string]string) ([]string, error) {
	base := map[string]string{}

	if mode == "inherit" {
		for _, kv := range os.Environ() {
			if k, v, ok := strings.Cut(kv, "="); ok {
				base[k] = v
			}
		}
	} else {
		tmpdir := filepath.Join(workdir, "tmp")
		if err := os.MkdirAll(tmpdir, 0o755); err != nil {
			return nil, fmt.Errorf("create scenario TMPDIR: %w", err)
		}

		base["PATH"] = os.Getenv("PATH")
		base["HOME"] = workdir
		base["TMPDIR"] = tmpdir
	}

	for k, v := range user {
		base[k] = v
	}

	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}

	slices.Sort(out)

	return out, nil
}

// newStdinReader returns a reader over the step's stdin string.
func newStdinReader(stdin string) io.Reader {
	return strings.NewReader(stdin)
}

// cappedBuffer captures up to limit bytes, dropping the rest and recording
// that truncation happened. Write always reports a full write so the child
// process never observes a short write or broken pipe.
type cappedBuffer struct {
	limit     int64
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.limit - int64(c.buf.Len())
	if remaining <= 0 {
		c.truncated = true

		return len(p), nil
	}

	if int64(len(p)) > remaining {
		_, _ = c.buf.Write(p[:remaining])
		c.truncated = true

		return len(p), nil
	}

	// bytes.Buffer.Write never returns a non-nil error; discard it so the
	// io.Writer contract is satisfied without leaking an unwrappable error.
	_, _ = c.buf.Write(p)

	return len(p), nil
}

func (c *cappedBuffer) bytes() []byte {
	return c.buf.Bytes()
}

// parseJSONValue returns the parsed JSON value of data, or null when data is
// empty or not valid JSON. The runtime decides whether invalid JSON is a
// failure (only when stdout_json is asserted).
func parseJSONValue(data []byte) cty.Value {
	if len(bytes.TrimSpace(data)) == 0 {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	inputType, err := ctyjson.ImpliedType(data)
	if err != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	value, err := ctyjson.Unmarshal(data, inputType)
	if err != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	return value
}

// execArtifacts names the files written for one exec step.
const (
	artifactStdout   = "stdout.txt"
	artifactStderr   = "stderr.txt"
	artifactMetadata = "metadata.json"
	artifactStdoutJS = "stdout.json"
)

// execMetadata is the metadata.json schema. It never includes environment
// values or argument values — only counts and paths — so secrets passed via
// env or args are not persisted.
type execMetadata struct {
	Command         string `json:"command"`
	ArgsCount       int    `json:"args_count"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	TimedOut        bool   `json:"timed_out"`
	StdoutPath      string `json:"stdout_path"`
	StderrPath      string `json:"stderr_path"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Workdir         string `json:"workdir"`
	SandboxMode     string `json:"sandbox_mode"`
	Network         bool   `json:"network"`
}

// writeExecArtifacts always writes stdout.txt, stderr.txt and metadata.json
// (and stdout.json when stdout parses as JSON) under the step artifacts dir,
// even on timeout or non-zero exit, so a failed run is still inspectable.
func writeExecArtifacts(ee *provider.ExecExecution, resolved string, run execResult) error {
	dir := ee.ArtifactsDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create exec artifacts dir: %w", err)
	}

	stdoutPath := filepath.Join(dir, artifactStdout)
	stderrPath := filepath.Join(dir, artifactStderr)

	if err := os.WriteFile(stdoutPath, run.stdout, 0o600); err != nil {
		return fmt.Errorf("write stdout artifact: %w", err)
	}

	if err := os.WriteFile(stderrPath, run.stderr, 0o600); err != nil {
		return fmt.Errorf("write stderr artifact: %w", err)
	}

	if value := parseJSONValue(run.stdout); !value.IsNull() {
		if encoded, err := ctyjson.Marshal(value, value.Type()); err == nil {
			_ = os.WriteFile(filepath.Join(dir, artifactStdoutJS), encoded, 0o600)
		}
	}

	meta := execMetadata{
		Command:         resolved,
		ArgsCount:       len(ee.Args),
		ExitCode:        run.exitCode,
		DurationMS:      run.duration.Milliseconds(),
		TimedOut:        run.timedOut,
		StdoutPath:      stdoutPath,
		StderrPath:      stderrPath,
		StdoutTruncated: run.stdoutTruncated,
		StderrTruncated: run.stderrTruncated,
		Workdir:         ee.Workdir,
		SandboxMode:     ee.SandboxMode,
		Network:         ee.Network,
	}

	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode exec metadata: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, artifactMetadata), encoded, 0o600); err != nil {
		return fmt.Errorf("write exec metadata: %w", err)
	}

	return nil
}
