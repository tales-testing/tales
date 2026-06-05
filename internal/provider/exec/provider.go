// Package exec implements the exec provider (step "exec"), which runs an
// external program directly (never through a shell) and exposes its exit code,
// stdout, stderr and duration for assertions and capture. The provider is
// disabled unless the CLI passes --allow-exec.
//
// The "process" sandbox is a portability and hygiene feature, NOT a security
// boundary: it controls the working directory, environment, timeout and output
// capture, but it does not isolate the filesystem and does not reliably block
// network access. Use a real sandbox (e.g. Docker, reserved here but not yet
// implemented) for stronger isolation.
package exec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

const providerType = "exec"

// defaultMaxOutput caps each captured stream at 1 MiB unless overridden.
const defaultMaxOutput = 1 << 20

const (
	sandboxModeProcess = "process"
	sandboxModeDocker  = "docker"
)

// Response attribute keys for the exec output object.
const (
	attrRaw       = "raw"
	attrJSON      = "json"
	attrTruncated = "truncated"
)

// disabledMessage is the exact error returned when exec runs without
// --allow-exec. It is asserted verbatim by tests and documented for users.
const disabledMessage = "exec provider is disabled by default. Re-run with --allow-exec."

// Provider executes exec steps. allowExec is immutable after construction, so
// the provider is safe to share across parallel scenarios.
type Provider struct {
	allowExec bool
}

// Option configures the provider.
type Option func(*Provider)

// WithAllowExec enables (or disables) command execution. Disabled is the
// default; the CLI wires this from --allow-exec.
func WithAllowExec(allowed bool) Option {
	return func(p *Provider) { p.allowExec = allowed }
}

// New creates an exec provider. Execution is disabled unless WithAllowExec(true)
// is passed.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Type returns the provider type.
func (p *Provider) Type() string {
	return providerType
}

// Execute gates on --allow-exec, then runs the program. The gate is checked
// before any work so a disabled run never resolves a command or spawns a
// process. docker mode is rejected (reserved, not implemented).
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if !p.allowExec {
		//nolint:staticcheck // ST1005: this exact two-sentence message (with trailing period) is the documented, test-asserted exec-disabled contract surfaced verbatim to users; it is never wrapped.
		return nil, errors.New(disabledMessage)
	}

	ee := input.Exec
	if ee == nil {
		return nil, errors.New("exec step is missing execution data")
	}

	if ee.SandboxMode == sandboxModeDocker {
		return nil, fmt.Errorf("exec sandbox mode %q is not implemented yet", sandboxModeDocker)
	}

	resolved, err := resolveCommand(ee.Command, ee.Workdir, ee.ProjectDir)
	if err != nil {
		return nil, err
	}

	env, err := buildEnv(ee.EnvMode, ee.Workdir, ee.Env)
	if err != nil {
		return nil, err
	}

	run := runProcess(ctx, resolved, env, ee)

	if err := writeExecArtifacts(ee, resolved, run); err != nil {
		return nil, err
	}

	if run.startErr != nil {
		return nil, fmt.Errorf("run command %q: %w", ee.Command, run.startErr)
	}

	if run.timedOut {
		return nil, fmt.Errorf("command %q timed out after %s", ee.Command, ee.Timeout)
	}

	return buildExecOutput(ee, resolved, run), nil
}

// execResult holds the outcome of one process run.
type execResult struct {
	exitCode        int
	stdout          []byte
	stderr          []byte
	stdoutTruncated bool
	stderrTruncated bool
	duration        time.Duration
	timedOut        bool
	startErr        error // non-nil when the process could not be started
}

// runProcess runs the resolved command with the given environment, capping
// each captured stream and enforcing the timeout. A non-zero exit is recorded
// in exitCode (not an error); only a start failure populates startErr.
func runProcess(ctx context.Context, resolved string, env []string, ee *provider.ExecExecution) execResult {
	runCtx := ctx

	if ee.Timeout > 0 {
		var cancel context.CancelFunc

		runCtx, cancel = context.WithTimeout(ctx, ee.Timeout)
		defer cancel()
	}

	limit := ee.MaxOutput
	if limit <= 0 {
		limit = defaultMaxOutput
	}

	cmd := exec.CommandContext(runCtx, resolved, ee.Args...) //nolint:gosec // G204: running an external tool is the provider's purpose; it is gated behind --allow-exec and path-restricted.
	cmd.Dir = ee.Workdir
	cmd.Env = env

	if ee.Stdin != "" {
		cmd.Stdin = newStdinReader(ee.Stdin)
	}

	stdout := &cappedBuffer{limit: limit}
	stderr := &cappedBuffer{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := nowFunc()
	runErr := cmd.Run()
	duration := nowFunc().Sub(start)

	result := execResult{
		stdout:          stdout.bytes(),
		stderr:          stderr.bytes(),
		stdoutTruncated: stdout.truncated,
		stderrTruncated: stderr.truncated,
		duration:        duration,
		timedOut:        errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}

	result.exitCode, result.startErr = classifyRunError(runErr, result.timedOut)

	return result
}

// classifyRunError turns cmd.Run's error into an exit code plus an optional
// start error. A non-zero exit (ExitError) is normal and assertable; a timeout
// yields exit code -1; anything else is a start failure.
func classifyRunError(runErr error, timedOut bool) (int, error) {
	if runErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}

	if timedOut {
		return -1, nil
	}

	return -1, runErr
}

// buildExecOutput assembles the provider output exposed to the runtime.
func buildExecOutput(ee *provider.ExecExecution, resolved string, run execResult) *provider.Output {
	return &provider.Output{
		Duration:   run.duration,
		StatusCode: run.exitCode,
		Request:    execRequestMetadata(ee, resolved),
		Response:   execResponse(run),
	}
}

// execResponse builds the response.* object: exit_code, stdout{raw,json,
// truncated}, stderr{raw,truncated}, duration_ms.
func execResponse(run execResult) map[string]cty.Value {
	return map[string]cty.Value{
		"exit_code": cty.NumberIntVal(int64(run.exitCode)),
		"stdout": cty.ObjectVal(map[string]cty.Value{
			attrRaw:       cty.StringVal(string(run.stdout)),
			attrJSON:      parseJSONValue(run.stdout),
			attrTruncated: cty.BoolVal(run.stdoutTruncated),
		}),
		"stderr": cty.ObjectVal(map[string]cty.Value{
			attrRaw:       cty.StringVal(string(run.stderr)),
			attrTruncated: cty.BoolVal(run.stderrTruncated),
		}),
		"duration_ms": cty.NumberIntVal(run.duration.Milliseconds()),
	}
}

// execRequestMetadata is the metadata-only request view: it carries the
// resolved command, the argument count (never the argument values, which may
// be sensitive), the sandbox mode and the working directory. No environment.
func execRequestMetadata(ee *provider.ExecExecution, resolved string) map[string]cty.Value {
	return map[string]cty.Value{
		"command":    cty.StringVal(resolved),
		"args_count": cty.NumberIntVal(int64(len(ee.Args))),
		"sandbox":    cty.StringVal(ee.SandboxMode),
		"workdir":    cty.StringVal(ee.Workdir),
	}
}
