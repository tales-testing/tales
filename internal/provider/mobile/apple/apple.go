package apple

import (
	"context"
	"fmt"

	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
)

// SimctlTool is the subset of simctl operations the lifecycle uses. The real
// implementation lives in package simctl; tests provide a fake.
type SimctlTool interface {
	FindDeviceByName(ctx context.Context, name string) (Device, error)
	Boot(ctx context.Context, udid string) error
	Install(ctx context.Context, udid, appPath string) error
	Uninstall(ctx context.Context, udid, bundleID string) error
	Launch(ctx context.Context, udid, bundleID string) error
	Terminate(ctx context.Context, udid, bundleID string) error
	Screenshot(ctx context.Context, udid, path string) error
}

// Device is the minimal device representation needed by the lifecycle.
type Device struct {
	UDID   string
	Booted bool
}

// XcodebuildLauncher starts the XCUITest driver. Production code uses
// *xcodebuild.Launcher; tests provide a fake.
type XcodebuildLauncher interface {
	Start(ctx context.Context, opts xcodebuild.Options, pinger xcodebuild.Pinger) (*xcodebuild.Handle, error)
}

// DriverFactory builds a driver.Driver for the given base URL.
type DriverFactory func(baseURL string) driver.Driver

// Lifecycle aggregates simctl, xcodebuild, and the driver factory into the
// operations the mobile provider needs.
type Lifecycle struct {
	Simctl     SimctlTool
	Xcodebuild XcodebuildLauncher
	NewDriver  DriverFactory
}

// DriverHandle is returned alongside a Driver and lets the caller stop the
// xcodebuild subprocess Tales started. It is nil in external-driver mode.
type DriverHandle interface {
	Stop(ctx context.Context) error
}

// EnsureBooted finds the simulator and boots it if needed, returning its UDID.
func (l *Lifecycle) EnsureBooted(ctx context.Context, target Target) (string, error) {
	device, err := l.Simctl.FindDeviceByName(ctx, target.DeviceName)
	if err != nil {
		return "", fmt.Errorf("find simulator %q: %w", target.DeviceName, err)
	}

	if !device.Booted {
		if err := l.Simctl.Boot(ctx, device.UDID); err != nil {
			return "", fmt.Errorf("boot simulator %q: %w", target.DeviceName, err)
		}
	}

	return device.UDID, nil
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

// EnsureDriver returns a driver client connected to the running driver. In
// external mode, only a health check is performed. Otherwise the XCUITest
// runner is spawned via xcodebuild and Tales owns the handle.
func (l *Lifecycle) EnsureDriver(ctx context.Context, udid string, target Target) (driver.Driver, DriverHandle, error) {
	if l.NewDriver == nil {
		return nil, nil, fmt.Errorf("driver factory is not configured")
	}

	client := l.NewDriver(target.Driver.BaseURL())

	if target.Driver.External {
		if err := client.Health(ctx); err != nil {
			return nil, nil, fmt.Errorf("external driver health: %w", err)
		}

		return client, nil, nil
	}

	if target.Driver.Project == "" {
		return nil, nil, fmt.Errorf("config.mobile.targets.%s.driver.project is required when driver.external is false", target.Name)
	}

	if target.Driver.Scheme == "" {
		return nil, nil, fmt.Errorf("config.mobile.targets.%s.driver.scheme is required when driver.external is false", target.Name)
	}

	handle, err := l.Xcodebuild.Start(ctx, xcodebuild.Options{
		UDID:        udid,
		Project:     target.Driver.Project,
		Scheme:      target.Driver.Scheme,
		Destination: fmt.Sprintf("platform=iOS Simulator,id=%s", udid),
	}, client)
	if err != nil {
		return nil, nil, fmt.Errorf("start xcuitest driver: %w", err)
	}

	return client, handle, nil
}

// ScreenshotFallback uses simctl io screenshot to capture a PNG when the
// driver-side screenshot endpoint cannot be reached.
func (l *Lifecycle) ScreenshotFallback(ctx context.Context, udid, path string) error {
	if err := l.Simctl.Screenshot(ctx, udid, path); err != nil {
		return fmt.Errorf("screenshot fallback: %w", err)
	}

	return nil
}
