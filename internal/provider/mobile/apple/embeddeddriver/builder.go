package embeddeddriver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DriverScheme is the xcodebuild scheme that ships the in-simulator HTTP
// driver. It must match the shared scheme committed to the driver source.
const DriverScheme = "TalesAppleDriverUITests"

// DriverProject is the file name of the Xcode project inside the
// extracted source directory.
const DriverProject = "TalesAppleDriver.xcodeproj"

// Builder builds the driver test bundle and returns the path of the
// .xctestrun file produced.
type Builder interface {
	BuildForTesting(ctx context.Context, sourceDir, derivedDataPath, logPath string) (string, error)
}

// BuildRunner runs an external command synchronously and captures its
// stdout/stderr to a log file. Provided as an interface so tests can
// inject a fake.
type BuildRunner interface {
	Run(ctx context.Context, name string, args []string, logPath string, env map[string]string) error
}

// BuildError carries the build log path for diagnostics.
type BuildError struct {
	Err     error
	LogPath string
}

func (e *BuildError) Error() string {
	if e == nil {
		return ""
	}

	if e.LogPath == "" {
		return e.Err.Error()
	}

	return fmt.Sprintf("%s\nDriver build log: %s\nRun `make doctor-ios` for Xcode diagnostics.", e.Err, e.LogPath)
}

func (e *BuildError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// XcodebuildBuilder is the production Builder that invokes
// `xcodebuild build-for-testing` for the embedded driver project.
type XcodebuildBuilder struct {
	Runner BuildRunner
}

// BuildForTesting compiles the test bundle and returns the path of the
// produced .xctestrun file. The function wipes any stale .xctestrun
// before the build so the post-build glob is unambiguous.
func (b *XcodebuildBuilder) BuildForTesting(ctx context.Context, sourceDir, derivedDataPath, logPath string) (string, error) {
	if b.Runner == nil {
		return "", fmt.Errorf("embeddeddriver: builder runner is not configured")
	}

	products := filepath.Join(derivedDataPath, "Build", "Products")
	if err := os.MkdirAll(products, 0o755); err != nil {
		return "", fmt.Errorf("ensure products dir: %w", err)
	}

	if stale, _ := filepath.Glob(filepath.Join(products, "*.xctestrun")); len(stale) > 0 {
		for _, m := range stale {
			_ = os.Remove(m)
		}
	}

	args := []string{
		"build-for-testing",
		"-project", filepath.Join(sourceDir, DriverProject),
		"-scheme", DriverScheme,
		"-configuration", "Debug",
		"-sdk", "iphonesimulator",
		"-derivedDataPath", derivedDataPath,
		"CODE_SIGNING_ALLOWED=NO",
	}

	if err := b.Runner.Run(ctx, "xcodebuild", args, logPath, nil); err != nil {
		return "", &BuildError{Err: fmt.Errorf("xcodebuild build-for-testing: %w", err), LogPath: logPath}
	}

	matches, err := filepath.Glob(filepath.Join(products, "*.xctestrun"))
	if err != nil {
		return "", &BuildError{Err: fmt.Errorf("glob xctestrun: %w", err), LogPath: logPath}
	}

	if len(matches) == 0 {
		return "", &BuildError{Err: fmt.Errorf("no .xctestrun produced under %s", products), LogPath: logPath}
	}

	if len(matches) > 1 {
		return "", &BuildError{Err: fmt.Errorf("multiple .xctestrun produced under %s: %v", products, matches), LogPath: logPath}
	}

	return matches[0], nil
}

// ExecBuildRunner is the default BuildRunner using os/exec. It captures
// stdout and stderr into logPath, creating its parent directory on the
// fly.
type ExecBuildRunner struct{}

// Run executes the command, blocking until completion.
func (ExecBuildRunner) Run(ctx context.Context, name string, args []string, logPath string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: command and args are derived from internal config, not user input

	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return fmt.Errorf("ensure log dir: %w", err)
		}

		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("open log: %w", err)
		}

		defer f.Close()

		cmd.Stdout = f
		cmd.Stderr = f
	}

	if len(env) > 0 {
		merged := append([]string{}, os.Environ()...)
		for k, v := range env {
			merged = append(merged, k+"="+v)
		}

		cmd.Env = merged
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}

	return nil
}
