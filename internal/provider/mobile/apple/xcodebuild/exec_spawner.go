package xcodebuild

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// gracefulStopTimeout is the budget given to a SIGTERM before SIGKILL.
const gracefulStopTimeout = 5 * time.Second

// ExecSpawner runs xcodebuild as a real subprocess.
type ExecSpawner struct{}

// Spawn starts the command and returns a Process backed by os/exec.
func (ExecSpawner) Spawn(ctx context.Context, name string, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	return &execProcess{cmd: cmd}, nil
}

type execProcess struct {
	cmd *exec.Cmd
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
		return nil
	case <-stopCtx.Done():
		_ = p.cmd.Process.Kill()

		<-done

		return nil
	}
}
