package screenrecord

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// ExecSpawner starts screenrecord with os/exec.
type ExecSpawner struct{}

// Spawn starts name with args in its own process group, discarding its
// output.
//
// screenrecord is stopped by interrupting it *on the device*, not by
// signaling this host process: the recorder has to write the MP4
// trailer itself, and killing the adb side would sever the shell before
// it did. This handle exists only so Stop can reap the command after
// the device-side interrupt.
func (ExecSpawner) Spawn(ctx context.Context, name string, args []string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: the adb path and args are built by this package from resolved config, never from scenario input.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}

	return &execProcess{cmd: cmd}, nil
}

type execProcess struct {
	cmd *exec.Cmd
}

// Wait reaps the command. The error is discarded: adb exits non-zero
// when the device-side screenrecord is interrupted, which is the normal
// end of a successful recording.
func (p *execProcess) Wait(_ context.Context) error {
	if p == nil || p.cmd == nil {
		return nil
	}

	_ = p.cmd.Wait()

	return nil
}
