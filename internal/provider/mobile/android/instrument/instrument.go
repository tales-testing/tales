// Package instrument starts the on-device Tales driver through
// `adb shell am instrument` and waits for it to answer /health.
//
// It mirrors the xcodebuild launcher: a Spawner/Process/Pinger triple so
// the whole start-and-wait sequence is unit-testable without a device,
// and a StartError that names the log file when the driver never comes
// up.
package instrument

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Defaults for the health handshake after spawning the instrumentation.
const (
	// DefaultHealthTimeout bounds the wait for the driver's first
	// answer. Installing and starting an instrumentation on a cold
	// emulator is slow, and failing early here would be indistinguishable
	// from a broken driver.
	DefaultHealthTimeout = 60 * time.Second
	// DefaultPollInterval is how often /health is retried.
	DefaultPollInterval = 500 * time.Millisecond
)

// TestRunner is the instrumentation runner the driver APK declares.
const TestRunner = "androidx.test.runner.AndroidJUnitRunner"

// DriverTestClass is the fully qualified entry point started on device.
const DriverTestClass = "org.taleslabs.tales.driver.TalesDriverTest#runServer"

// Process is a running instrumentation. Stop terminates it.
type Process interface {
	Stop(ctx context.Context) error
}

// Spawner starts a long-lived external command, capturing its output to
// logPath. Tests provide a fake.
type Spawner interface {
	Spawn(ctx context.Context, name string, args []string, logPath string) (Process, error)
}

// Pinger reports whether the driver is answering. The driver client
// satisfies it.
type Pinger interface {
	Health(ctx context.Context) error
}

// Options describes one instrumentation launch.
type Options struct {
	// ADBPath and Serial address the device.
	ADBPath string
	Serial  string
	// TestPackage is the instrumentation package
	// ("org.taleslabs.tales.driver.test").
	TestPackage string
	// DevicePort is the port the driver binds inside the device.
	DevicePort int
	// LogPath receives the instrumentation's stdout and stderr.
	LogPath string

	// HealthTimeout and PollInterval override the defaults above.
	HealthTimeout time.Duration
	PollInterval  time.Duration
}

// StartError adds the driver-log path to a startup failure, so the user
// lands on the file explaining it rather than a bare timeout.
type StartError struct {
	Err     error
	LogPath string
}

func (e *StartError) Error() string {
	if e == nil {
		return ""
	}

	if e.LogPath == "" {
		return e.Err.Error()
	}

	return fmt.Sprintf("%s\nDriver log: %s\nRun `make doctor-android` for device diagnostics.", e.Err, e.LogPath)
}

func (e *StartError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// DriverLogPath implements the mobile provider's driverStartError
// interface, letting it attach this log to the failed step's report
// without importing this package.
func (e *StartError) DriverLogPath() string {
	if e == nil {
		return ""
	}

	return e.LogPath
}

// Handle owns a running instrumentation.
type Handle struct {
	process Process
}

// Stop terminates the instrumentation.
func (h *Handle) Stop(ctx context.Context) error {
	if h == nil || h.process == nil {
		return nil
	}

	if err := h.process.Stop(ctx); err != nil {
		return fmt.Errorf("stop instrumentation: %w", err)
	}

	return nil
}

// Launcher starts instrumentations through a Spawner.
type Launcher struct {
	spawner Spawner
}

// New returns a Launcher using the given Spawner.
func New(spawner Spawner) *Launcher {
	return &Launcher{spawner: spawner}
}

// Start spawns the instrumentation and blocks until the driver answers
// /health or the timeout elapses. On timeout the process is stopped, so
// a failed start does not leave an orphan holding the device port.
func (l *Launcher) Start(ctx context.Context, opts Options, pinger Pinger) (*Handle, error) {
	if l.spawner == nil {
		return nil, errors.New("instrument: spawner is not configured")
	}

	args := BuildArgs(opts)

	process, err := l.spawner.Spawn(ctx, opts.ADBPath, args, opts.LogPath)
	if err != nil {
		return nil, &StartError{Err: fmt.Errorf("spawn instrumentation: %w", err), LogPath: opts.LogPath}
	}

	if err := waitForHealth(ctx, opts, pinger); err != nil {
		_ = process.Stop(ctx)

		return nil, &StartError{Err: err, LogPath: opts.LogPath}
	}

	return &Handle{process: process}, nil
}

// BuildArgs renders the adb argument list for one launch.
//
//	adb -s <serial> shell am instrument -w -r \
//	  -e class <DriverTestClass> -e port <port> \
//	  <testPackage>/<TestRunner>
//
// -w keeps adb attached for the instrumentation's lifetime, which is
// what keeps the driver alive: the process is torn down when this
// command is killed. -r asks for raw output, so the log holds the
// runner's own stream rather than a human-formatted summary.
func BuildArgs(opts Options) []string {
	return []string{
		"-s", opts.Serial,
		"shell", "am", "instrument", "-w", "-r",
		"-e", "class", DriverTestClass,
		"-e", "port", fmt.Sprintf("%d", opts.DevicePort),
		opts.TestPackage + "/" + TestRunner,
	}
}

func waitForHealth(ctx context.Context, opts Options, pinger Pinger) error {
	if pinger == nil {
		return errors.New("instrument: health pinger is not configured")
	}

	timeout := opts.HealthTimeout
	if timeout <= 0 {
		timeout = DefaultHealthTimeout
	}

	interval := opts.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error

	for {
		if err := pinger.Health(deadline); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-deadline.Done():
			if lastErr != nil {
				return fmt.Errorf("driver did not become healthy within %s: %w", timeout, lastErr)
			}

			return fmt.Errorf("driver did not become healthy within %s", timeout)
		case <-ticker.C:
		}
	}
}
