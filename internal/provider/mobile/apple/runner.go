// Package apple groups the macOS/Xcode lifecycle helpers used by the mobile
// provider: simctl wrappers, xcodebuild driver launcher, and the high-level
// Lifecycle facade. Apple tooling is only invoked through the Runner interface
// so tests can use a fake executor.
package apple

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Runner runs a command and returns its standard output.
//
// Implementations must call the underlying tool with the given context so a
// caller can cancel slow operations.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner uses os/exec to run real commands. Use it in production code.
type ExecRunner struct{}

// Run executes the command with the given context and returns its standard output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, fmt.Errorf("%s %v: %s", name, args, string(exitErr.Stderr))
		}

		return out, fmt.Errorf("%s %v: %w", name, args, err)
	}

	return out, nil
}
