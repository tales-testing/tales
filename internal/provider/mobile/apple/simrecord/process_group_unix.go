//go:build !windows

package simrecord

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// enableProcessGroup attaches simctl recordVideo to its own process group
// so SIGINT reaches every child, mirroring the xcodebuild launcher pattern.
// In practice simctl spawns no children, but the same setup keeps the stop
// path uniform across Apple subprocesses.
func enableProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup delivers sig to the entire process group. ESRCH is
// silently ignored (the process exited between Getpgid and Kill).
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		if killErr := syscall.Kill(-pgid, sig); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return fmt.Errorf("signal process group: %w", killErr)
		}

		return nil
	}

	if err := cmd.Process.Signal(sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process: %w", err)
	}

	return nil
}
