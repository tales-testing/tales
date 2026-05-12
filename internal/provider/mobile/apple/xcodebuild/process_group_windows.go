//go:build windows

package xcodebuild

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// enableProcessGroup is a no-op on Windows. The Apple driver only runs
// on macOS at runtime; this file exists so the package compiles in
// cross-platform CI.
func enableProcessGroup(_ *exec.Cmd) {}

// signalProcessGroup falls back to signaling the immediate process on
// Windows since the unix process-group concept does not apply.
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Signal(sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process: %w", err)
	}

	return nil
}
