//go:build !windows

package xcodebuild

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// enableProcessGroup attaches the child to its own process group so that
// signals can be delivered to the whole tree of grandchildren (xcodebuild
// → xctest → in-simulator runner host) rather than just the immediate
// xcodebuild PID. Without this the XCUITest runner inside the simulator
// leaks past Tales' shutdown and squats port 9080 for the next run.
func enableProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup sends sig to the entire process group when the
// child was launched with enableProcessGroup. If the group cannot be
// resolved (race with Wait), it falls back to signaling the immediate
// process so callers still get best-effort cleanup.
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
