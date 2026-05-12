package xcodebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// gracefulStopTimeout is the budget given to a SIGTERM before SIGKILL.
const gracefulStopTimeout = 5 * time.Second

// ExecSpawner runs xcodebuild as a real subprocess.
type ExecSpawner struct{}

// Spawn starts the command and returns a Process backed by os/exec.
func (ExecSpawner) Spawn(ctx context.Context, name string, args []string, logPath string, env map[string]string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = mergedEnv(env)

	var logFile *os.File

	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, fmt.Errorf("create xcodebuild log dir: %w", err)
		}

		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open xcodebuild log: %w", err)
		}

		logFile = file
		cmd.Stdout = file
		cmd.Stderr = file
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}

		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	return &execProcess{cmd: cmd, logFile: logFile}, nil
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
	cmd     *exec.Cmd
	logFile *os.File
}

func (p *execProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- p.cmd.Wait()
	}()

	stopCtx, cancel := context.WithTimeout(ctx, gracefulStopTimeout)
	defer cancel()

	select {
	case <-done:
		p.closeLog()

		return nil
	case <-stopCtx.Done():
		_ = p.cmd.Process.Kill()

		<-done
		p.closeLog()

		return nil
	}
}

func (p *execProcess) closeLog() {
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
}
