package apple

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hyperxlab/tales/internal/provider/mobile/apple/embeddeddriver"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
)

// SimctlTool is the subset of simctl operations the lifecycle uses. The real
// implementation lives in package simctl; tests provide a fake.
type SimctlTool interface {
	FindDeviceByName(ctx context.Context, name string) (Device, error)
	Boot(ctx context.Context, udid string) error
	WaitBooted(ctx context.Context, udid string, timeout time.Duration) error
	Install(ctx context.Context, udid, appPath string) error
	Uninstall(ctx context.Context, udid, bundleID string) error
	Launch(ctx context.Context, udid, bundleID string) error
	Terminate(ctx context.Context, udid, bundleID string) error
	Screenshot(ctx context.Context, udid, path string) error
}

// Device is the minimal device representation needed by the lifecycle.
type Device struct {
	UDID    string
	Name    string
	Runtime string
	Booted  bool
}

// XcodebuildLauncher starts the XCUITest driver. Production code uses
// *xcodebuild.Launcher; tests provide a fake.
type XcodebuildLauncher interface {
	Start(ctx context.Context, opts xcodebuild.Options, pinger xcodebuild.Pinger) (*xcodebuild.Handle, error)
}

// EmbeddedDriverManager extracts, builds, and caches the embedded
// XCUITest driver. *embeddeddriver.Manager satisfies it; tests fake it.
type EmbeddedDriverManager interface {
	Prepare(ctx context.Context, sourcePathOverride, iosRuntime string) (embeddeddriver.Prepared, error)
	InvalidateBuild(key string) error
}

// DriverFactory builds a driver.Driver for the given base URL.
type DriverFactory func(baseURL string) driver.Driver

// Lifecycle aggregates simctl, xcodebuild, and the driver factory into the
// operations the mobile provider needs. Embedded is optional; when nil,
// targets that opt into embedded mode (no Project/Scheme, no SourcePath)
// will be rejected with a clean error.
type Lifecycle struct {
	Simctl     SimctlTool
	Xcodebuild XcodebuildLauncher
	NewDriver  DriverFactory
	Embedded   EmbeddedDriverManager
}

// RunnerBundleIDForHost returns the bundle identifier of the XCUITest
// runner application that xcodebuild installs on the simulator next to
// the host app. The runner is the process that actually owns the
// in-simulator HTTP server, so terminating it is the surest belt-and-
// suspenders cleanup when SIGTERM does not reach the test child tree.
func RunnerBundleIDForHost(hostBundleID string) string {
	if hostBundleID == "" {
		return ""
	}

	return hostBundleID + ".xctrunner"
}

// DriverHostBundleID is the bundle identifier of the host app embedded
// in the driver project. xcodebuild installs the matching .xctrunner
// alongside it on the simulator at test time.
const DriverHostBundleID = "com.hyperxlab.TalesAppleDriverHost"

const driverLogsBase = "build/artifacts/mobile/driver"

var unsafeDriverLogSegment = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// DriverHandle is returned alongside a Driver and lets the caller stop the
// xcodebuild subprocess Tales started. It is nil in external-driver mode.
type DriverHandle interface {
	Stop(ctx context.Context) error
}

// EnsureBooted finds the simulator and boots it if needed, returning the
// resolved Device (UDID, runtime, etc.). The runtime field feeds the
// embedded-driver cache key so builds remain valid across iOS runtime
// versions.
func (l *Lifecycle) EnsureBooted(ctx context.Context, target Target) (Device, error) {
	device, err := l.Simctl.FindDeviceByName(ctx, target.DeviceName)
	if err != nil {
		return Device{}, fmt.Errorf("find simulator %q: %w", target.DeviceName, err)
	}

	if device.Name != "" || device.Runtime != "" {
		fmt.Fprintf(os.Stderr, "Selected iOS simulator: name=%q udid=%s runtime=%s\n", device.Name, device.UDID, device.Runtime)
	}

	if !device.Booted {
		if err := l.Simctl.Boot(ctx, device.UDID); err != nil {
			return Device{}, fmt.Errorf("boot simulator %q: %w", target.DeviceName, err)
		}
	}

	if err := l.Simctl.WaitBooted(ctx, device.UDID, 2*time.Minute); err != nil {
		return Device{}, fmt.Errorf("wait simulator boot %q: %w", target.DeviceName, err)
	}

	return device, nil
}

// InstallApp installs (or reinstalls) the app on the simulator.
func (l *Lifecycle) InstallApp(ctx context.Context, udid string, target Target) error {
	if err := l.Simctl.Install(ctx, udid, target.AppPath); err != nil {
		return fmt.Errorf("install app: %w", err)
	}

	return nil
}

// ClearAppState terminates the app, uninstalls it, then installs it again.
// This is the V1 implementation of `launch { clear_state = true }`.
func (l *Lifecycle) ClearAppState(ctx context.Context, udid string, target Target) error {
	_ = l.Simctl.Terminate(ctx, udid, target.BundleID)

	if err := l.Simctl.Uninstall(ctx, udid, target.BundleID); err != nil {
		return fmt.Errorf("clear state (uninstall): %w", err)
	}

	if err := l.Simctl.Install(ctx, udid, target.AppPath); err != nil {
		return fmt.Errorf("clear state (install): %w", err)
	}

	return nil
}

// LaunchApp launches the configured app on the given simulator.
func (l *Lifecycle) LaunchApp(ctx context.Context, udid string, target Target) error {
	if err := l.Simctl.Launch(ctx, udid, target.BundleID); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	return nil
}

// TerminateApp terminates the configured app on the given simulator. If the
// app was not running, simctl reports a clean exit and the lifecycle treats it
// as a no-op.
func (l *Lifecycle) TerminateApp(ctx context.Context, udid string, target Target) error {
	if err := l.Simctl.Terminate(ctx, udid, target.BundleID); err != nil {
		return fmt.Errorf("terminate app: %w", err)
	}

	return nil
}

// TerminateDriverRunner asks simctl to terminate the XCUITest runner
// (.xctrunner) bundle that hosts the in-simulator HTTP server. Used as
// a best-effort companion to xcodebuild process termination on Session
// Close: even when the parent xcodebuild PID dies cleanly, the runner
// child running inside the simulator can survive briefly and squat the
// port the next session needs. Errors are not returned because this is
// a defensive cleanup; the caller logs decisions about them.
func (l *Lifecycle) TerminateDriverRunner(ctx context.Context, udid string) error {
	if l == nil || l.Simctl == nil || udid == "" {
		return nil
	}

	runnerBundle := RunnerBundleIDForHost(DriverHostBundleID)
	if runnerBundle == "" {
		return nil
	}

	if err := l.Simctl.Terminate(ctx, udid, runnerBundle); err != nil {
		return fmt.Errorf("terminate driver runner %q: %w", runnerBundle, err)
	}

	return nil
}

// EnsureDriver returns a driver client connected to the running driver.
//
// Resolution order:
//  1. driver.external = true → only health-check the configured URL.
//  2. otherwise              → embedded mode: extract+build the embedded
//     driver (or driver.source_path override), then run it via
//     `xcodebuild test-without-building`.
//
// In embedded mode a single retry is attempted if the freshly built
// driver fails to answer /health: the cached build is invalidated and
// rebuilt from scratch before the second attempt, in case Xcode has
// upgraded between Tales runs in a way the cache key did not capture.
func (l *Lifecycle) EnsureDriver(ctx context.Context, device Device, target Target) (driver.Driver, DriverHandle, error) {
	if l.NewDriver == nil {
		return nil, nil, errors.New("driver factory is not configured")
	}

	client := l.NewDriver(target.Driver.BaseURL())

	if target.Driver.External {
		if err := client.Health(ctx); err != nil {
			return nil, nil, fmt.Errorf("external driver health: %w", err)
		}

		return client, nil, nil
	}

	return l.startEmbeddedDriver(ctx, device, target, client)
}

func (l *Lifecycle) startEmbeddedDriver(ctx context.Context, device Device, target Target, client driver.Driver) (driver.Driver, DriverHandle, error) {
	if l.Embedded == nil {
		return nil, nil, fmt.Errorf("config.mobile.targets.%s.driver: embedded driver manager is not configured on the apple.Lifecycle (set driver.external = true to connect to an already-running driver)", target.Name)
	}

	prepared, err := l.Embedded.Prepare(ctx, target.Driver.SourcePath, device.Runtime)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare embedded driver: %w", err)
	}

	logPath := driverLogPath(target.Name)
	opts := xcodebuild.Options{
		UDID:          device.UDID,
		XCTestRunPath: prepared.XCTestRunPath,
		Destination:   fmt.Sprintf("platform=iOS Simulator,id=%s", device.UDID),
		HealthURL:     target.Driver.BaseURL() + "/health",
		LogPath:       logPath,
		Env:           driverEnv(target.Driver),
	}

	handle, startErr := l.Xcodebuild.Start(ctx, opts, client)
	if startErr == nil {
		return client, handle, nil
	}

	// Retry once: invalidate the cached build, force a rebuild, and try
	// again. Covers the case where build.ok is still on disk but the
	// .xctest is no longer launchable (Xcode upgrade without a cache-key
	// input changing, CoreSimulator quirks, ...).
	fmt.Fprintf(os.Stderr, "embedded driver failed to start (%v); invalidating cache %q and rebuilding\n", startErr, prepared.CacheKey)

	if invErr := l.Embedded.InvalidateBuild(prepared.CacheKey); invErr != nil {
		return nil, nil, fmt.Errorf("start xcuitest driver: %w (cache invalidation also failed: %w)", startErr, invErr)
	}

	rebuilt, prepErr := l.Embedded.Prepare(ctx, target.Driver.SourcePath, device.Runtime)
	if prepErr != nil {
		return nil, nil, fmt.Errorf("start xcuitest driver: %w (rebuild after invalidation failed: %w)", startErr, prepErr)
	}

	opts.XCTestRunPath = rebuilt.XCTestRunPath

	retryHandle, retryErr := l.Xcodebuild.Start(ctx, opts, client)
	if retryErr != nil {
		return nil, nil, fmt.Errorf("start xcuitest driver after rebuild: %w (first attempt: %w)", retryErr, startErr)
	}

	return client, retryHandle, nil
}

func driverEnv(cfg DriverConfig) map[string]string {
	return map[string]string{
		"TALES_DRIVER_HOST": cfg.Host,
		"TALES_DRIVER_PORT": fmt.Sprintf("%d", cfg.Port),
	}
}

func driverLogPath(targetName string) string {
	segment := strings.Trim(unsafeDriverLogSegment.ReplaceAllString(targetName, "_"), "_")
	if segment == "" {
		segment = "unnamed"
	}

	return filepath.Join(driverLogsBase, segment, "driver.log")
}

// ScreenshotFallback uses simctl io screenshot to capture a PNG when the
// driver-side screenshot endpoint cannot be reached.
func (l *Lifecycle) ScreenshotFallback(ctx context.Context, udid, path string) error {
	if err := l.Simctl.Screenshot(ctx, udid, path); err != nil {
		return fmt.Errorf("screenshot fallback: %w", err)
	}

	return nil
}
