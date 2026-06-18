package simrecord

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"syscall"
)

// ExecSpawner runs simctl recordVideo as a real subprocess. It enables a
// process group so SIGINT reaches simctl even if it forked any helper.
type ExecSpawner struct{}

// Spawn starts the command and returns a Process backed by os/exec.
func (ExecSpawner) Spawn(ctx context.Context, name string, args []string, env map[string]string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: arguments originate from internal Tales options, not user input.
	cmd.Env = mergedEnv(env)
	enableProcessGroup(cmd)

	// simctl recordVideo writes the MP4 directly to the path argument; its
	// own stdout/stderr is small and only useful when something fails.
	// Inheriting the parent streams keeps the noise minimal without losing
	// diagnostics when the user runs Tales with --verbose.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	return &execProcess{cmd: cmd}, nil
}

func mergedEnv(env map[string]string) []string {
	out := os.Environ()
	if len(env) == 0 {
		return out
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}

	return out
}

type execProcess struct {
	cmd *exec.Cmd
}

// Stop sends SIGINT and waits for the process to exit. It never escalates
// to SIGTERM or SIGKILL: simctl flushes the MP4 moov atom on SIGINT only,
// so a forced kill would leave an unplayable file. If the context fires
// before the process exits, the function returns the timeout error but
// leaves the process running so it can still finish writing.
func (p *execProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if err := signalProcessGroup(p.cmd, syscall.SIGINT); err != nil {
		return fmt.Errorf("send SIGINT: %w", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil && !isExitOnSignal(err) {
			return fmt.Errorf("wait simctl: %w", err)
		}

		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait simctl: %w", ctx.Err())
	}
}

// isExitOnSignal reports whether err is the expected "process terminated by
// the signal we sent" outcome. SIGINT delivered to simctl results in a
// non-zero exit which os/exec wraps in *exec.ExitError; that path is not
// a recorder failure.
func isExitOnSignal(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}

	if status.Signaled() {
		return true
	}

	// Some shells/wrappers translate the signal into an exit code; treat
	// any non-zero exit as expected since simctl never returns zero after
	// a clean SIGINT stop.
	return status.Exited()
}
