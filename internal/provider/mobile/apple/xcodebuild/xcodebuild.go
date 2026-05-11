// Package xcodebuild starts and stops the XCUITest runner that ships the
// in-simulator HTTP driver. xcodebuild is invoked through a Spawner interface
// so the lifecycle logic (build args, health polling, graceful stop) can be
// unit-tested on machines without Xcode.
package xcodebuild

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultHealthTimeout is how long Start polls for /health before giving up.
const DefaultHealthTimeout = 60 * time.Second

// DefaultPollInterval is the wait between two health pings.
const DefaultPollInterval = 500 * time.Millisecond

// Spawner starts an external command and returns a Process handle. Replace it
// in tests with a fake spawner.
type Spawner interface {
	Spawn(ctx context.Context, name string, args []string) (Process, error)
}

// Process represents a running xcodebuild test subprocess.
type Process interface {
	// Stop attempts a graceful termination, escalating to a forced kill if the
	// process does not exit before ctx is done.
	Stop(ctx context.Context) error
}

// Pinger is the subset of driver.Driver needed to poll for readiness.
type Pinger interface {
	Health(ctx context.Context) error
}

// Options drive a single xcodebuild test invocation.
type Options struct {
	UDID          string
	Project       string
	Scheme        string
	Destination   string
	ExtraArgs     []string
	HealthTimeout time.Duration
	PollInterval  time.Duration
}

// Launcher builds and supervises the xcodebuild subprocess.
type Launcher struct {
	spawner Spawner
}

// New returns a Launcher driven by the given Spawner.
func New(spawner Spawner) *Launcher {
	return &Launcher{spawner: spawner}
}

// Handle is returned by Start and lets the caller Stop the running driver.
type Handle struct {
	process Process
}

// Stop forwards to the underlying Process.
func (h *Handle) Stop(ctx context.Context) error {
	if h == nil || h.process == nil {
		return nil
	}

	if err := h.process.Stop(ctx); err != nil {
		return fmt.Errorf("stop xcodebuild: %w", err)
	}

	return nil
}

// Start launches xcodebuild and waits for the driver to answer /health.
// It returns a Handle owning the running process. If health is never reached
// before HealthTimeout, the process is stopped and an error is returned.
func (l *Launcher) Start(ctx context.Context, opts Options, pinger Pinger) (*Handle, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	args := BuildArgs(opts)

	process, err := l.spawner.Spawn(ctx, "xcodebuild", args)
	if err != nil {
		return nil, fmt.Errorf("spawn xcodebuild: %w", err)
	}

	handle := &Handle{process: process}

	if err := waitForHealth(ctx, pinger, opts); err != nil {
		_ = process.Stop(context.Background())

		return nil, err
	}

	return handle, nil
}

// BuildArgs produces the xcodebuild argv for the given options. Exported for
// test inspection.
func BuildArgs(opts Options) []string {
	args := make([]string, 0, 7+len(opts.ExtraArgs))

	args = append(args,
		"test",
		"-project", opts.Project,
		"-scheme", opts.Scheme,
		"-destination", opts.Destination,
	)

	args = append(args, opts.ExtraArgs...)

	return args
}

func validateOptions(opts Options) error {
	if opts.Project == "" {
		return errors.New("xcodebuild: project is required")
	}

	if opts.Scheme == "" {
		return errors.New("xcodebuild: scheme is required")
	}

	if opts.Destination == "" {
		return errors.New("xcodebuild: destination is required")
	}

	return nil
}

func waitForHealth(ctx context.Context, pinger Pinger, opts Options) error {
	if pinger == nil {
		return errors.New("xcodebuild: pinger is required")
	}

	timeout := opts.HealthTimeout
	if timeout <= 0 {
		timeout = DefaultHealthTimeout
	}

	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		lastErr := pinger.Health(deadlineCtx)
		if lastErr == nil {
			return nil
		}

		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("driver did not become healthy within %s: %w", timeout, lastErr)
		case <-time.After(interval):
		}
	}
}
