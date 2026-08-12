package mobile

import "context"

// Lifecycle is everything the provider needs from a platform's device
// tooling once a session exists: installing the app under test, resetting
// it, granting permissions, and stopping it again.
//
// It deliberately excludes session construction (booting a device,
// starting the driver). That is the Backend's job and is inherently
// platform-shaped — simctl + xcodebuild on iOS, adb + am instrument on
// Android — whereas the operations below map one-to-one across platforms
// and are called from the shared step execution path.
//
// deviceID is the platform's device handle: a simulator UDID on iOS, an
// adb serial on Android. The provider treats it as an opaque string and
// only ever passes back what the Backend put on the Session.
type Lifecycle interface {
	// InstallApp installs (or reinstalls) Target.AppPath on the device.
	InstallApp(ctx context.Context, deviceID string, target Target) error

	// ClearAppState returns the app to a first-launch state, then leaves it
	// installed and ready to launch. Backs `launch { clear_state = true }`.
	ClearAppState(ctx context.Context, deviceID string, target Target) error

	// SetPermission grants or revokes one privacy permission for the app.
	// action is "grant" or "revoke"; service is a Tales service name that
	// the backend maps onto its platform's permission model.
	SetPermission(ctx context.Context, deviceID string, target Target, action, service string) error

	// TerminateApp stops the app under test. Terminating an app that is not
	// running is a no-op, not an error.
	TerminateApp(ctx context.Context, deviceID string, target Target) error

	// TerminateDriverRunner kills any on-device process hosting the driver,
	// as a defensive companion to stopping the driver subprocess: the host
	// process can die while its on-device child briefly survives and squats
	// the port the next session needs.
	TerminateDriverRunner(ctx context.Context, deviceID string) error

	// ScreenshotFallback captures a PNG straight from the device tooling,
	// for when the driver's own screenshot endpoint is unreachable (which
	// is precisely when a failure screenshot matters most).
	ScreenshotFallback(ctx context.Context, deviceID, path string) error
}

// HostAppLauncher is implemented by a Lifecycle whose platform tooling can
// start the app from the host, out of band from the UI driver.
//
// When a backend provides it, the provider performs the cold launch here
// and then asks the driver only to re-bind its automation session
// (Driver.Activate) instead of driving the launch itself. That split
// matters on iOS: XCUIApplication.launch() runs inside the XCTest runner,
// so a simulator that declines to open the app ("unknown to FrontBoard",
// seen on loaded CI runners right after a clear_state reinstall) becomes a
// recorded XCTest failure, which tears the runner down and takes every
// later scenario with it — after spending a minute waiting for
// accessibility on a process that never existed. The same refusal from
// simctl is an immediate, retryable error that costs one step.
//
// Backends without host-side launching keep driving it through the driver,
// which is what a platform whose driver owns the app lifecycle wants.
type HostAppLauncher interface {
	// LaunchApp starts the app under test, replacing any running instance.
	LaunchApp(ctx context.Context, deviceID string, target Target) error
}

// DeviceLogDumper is implemented by a Lifecycle whose platform can dump
// the device's own system log.
//
// It is captured next to the failure screenshot and hierarchy, because
// those two answer "what was on screen" and not "what did the system
// think it was doing". An Android run lost scenarios to a launcher ANR,
// and a later one to an app that simply never got a window; both were
// indistinguishable from the outside, and neither could be settled
// without the device log.
type DeviceLogDumper interface {
	// CaptureDeviceLog writes a bounded dump of the device log to path.
	CaptureDeviceLog(ctx context.Context, deviceID, path string) error
}

// DriverHandle stops the driver process Tales started. Backends return a
// nil handle when the driver is external, since Tales does not own it.
type DriverHandle interface {
	Stop(ctx context.Context) error
}

// Diagnostics carries the on-disk paths holding the most useful
// post-mortem information when the driver dies mid-scenario. The provider
// quotes them in transport-level error messages ("connection refused",
// "EOF" on a POST) and attaches them to the step report, so users land on
// the file with the crash report instead of a bare network error.
//
// It is a list rather than named fields because the useful files differ by
// platform — an .xcresult bundle on iOS, a logcat dump on Android — while
// the provider only ever forwards them verbatim. Backends choose the type
// strings; they surface as-is in the visual and JSONL reports.
//
// Empty for external drivers: Tales owns none of their files.
type Diagnostics struct {
	Artifacts []Artifact
}

// driverStartError is implemented by backend errors that know where the
// driver's startup log landed. It lets the provider attach that log to a
// failed session without importing any platform's launcher package.
type driverStartError interface {
	// DriverLogPath returns the log path, or "" when none was written.
	DriverLogPath() string
}
