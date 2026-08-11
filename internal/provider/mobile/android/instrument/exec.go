package instrument

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// gracefulStopTimeout is how long the instrumentation is given to exit
// after SIGTERM before it is killed outright.
const gracefulStopTimeout = 5 * time.Second

// ExecSpawner starts the instrumentation with os/exec, capturing its
// output to a log file.
type ExecSpawner struct{}

// Spawn starts name with args in its own process group.
//
// The group matters: `adb shell am instrument` fans out into child
// processes, and signaling only the parent would leave the on-device
// instrumentation running with nothing attached to it. Signaling the
// group takes the whole tree down.
func (ExecSpawner) Spawn(ctx context.Context, name string, args []string, logPath string) (Process, error) {
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, fmt.Errorf("create driver log dir: %w", err)
		}
	}

	var logFile *os.File

	if logPath != "" {
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open driver log: %w", err)
		}

		logFile = file
	}

	// WithoutCancel deliberately detaches the driver from the caller's
	// context. That context bounds the health handshake, not the
	// driver's lifetime: the driver has to survive Start and every
	// later step, and stop only when the session closes and Stop
	// signals it.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, args...) //nolint:gosec // G204: the adb path and args are built by this package from resolved config, never from scenario input.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		closeLog(logFile)

		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	return &execProcess{cmd: cmd, logFile: logFile}, nil
}

type execProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
}

// Stop signals the process group, escalating to SIGKILL if the tree has
// not exited in time, then closes the log.
func (p *execProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	defer closeLog(p.logFile)

	pgid := -p.cmd.Process.Pid

	_ = syscall.Kill(pgid, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		_ = p.cmd.Wait()

		close(done)
	}()

	stopCtx, cancel := context.WithTimeout(ctx, gracefulStopTimeout)
	defer cancel()

	select {
	case <-done:
		return nil
	case <-stopCtx.Done():
		_ = syscall.Kill(pgid, syscall.SIGKILL)

		<-done

		return nil
	}
}

func closeLog(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}
