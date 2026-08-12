// Package android implements the Android backend of the mobile
// provider: device selection and boot, driver install and launch, and
// the per-step lifecycle operations.
//
// It is the counterpart of package apple. Both import the parent mobile
// package and satisfy its interfaces; the parent knows nothing about
// either.
package android

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/provider/mobile"
	"github.com/tales-testing/tales/internal/provider/mobile/android/adb"
	"github.com/tales-testing/tales/internal/provider/mobile/android/instrument"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// Driver package identifiers, matching the Kotlin project.
const (
	// DriverPackage is the driver app: an empty shell owning the
	// driver's permissions.
	DriverPackage = "org.taleslabs.tales.driver"
	// DriverTestPackage is the instrumentation that serves the HTTP API.
	DriverTestPackage = DriverPackage + ".test"
)

// DriverDevicePort is the port the driver binds inside the device.
//
// It is fixed rather than allocated: the device port namespace is
// per-device, so two emulators cannot collide on it. Only the host side
// of the forward needs a free port.
const DriverDevicePort = 9080

const driverLogsBase = "build/artifacts/mobile/driver"

// logcatLines bounds the log dump attached to a failing step. Enough to
// hold the crash, ANR or slow start and what led to it, without
// attaching megabytes to a step report.
const logcatLines = 2000

var unsafeLogSegment = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// ADBTool is the subset of adb operations the lifecycle uses. The real
// implementation is *adb.Tool; tests provide a fake.
type ADBTool interface {
	Binary() string
	ListDevices(ctx context.Context) ([]adb.Device, error)
	WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error
	APILevel(ctx context.Context, serial string) (int, error)
	Install(ctx context.Context, serial, apkPath string) error
	IsInstalled(ctx context.Context, serial, packageName string) (bool, error)
	Shell(ctx context.Context, serial, command string) (string, error)
	Forward(ctx context.Context, serial string, hostPort, devicePort int) error
	RemoveForward(ctx context.Context, serial string, hostPort int)
	Screencap(ctx context.Context, serial, localPath string) error
	Logcat(ctx context.Context, serial, tag string, maxLines int) (string, error)
}

// InstrumentLauncher starts the on-device driver. The real
// implementation is *instrument.Launcher; tests provide a fake.
type InstrumentLauncher interface {
	Start(ctx context.Context, opts instrument.Options, pinger instrument.Pinger) (*instrument.Handle, error)
}

// DriverArtifacts materializes the embedded driver APKs on disk and
// identifies the build, so an unchanged driver is not reinstalled on
// every run.
type DriverArtifacts interface {
	// Prepare writes the APKs to a cache directory and returns their
	// paths plus a hash identifying this driver build.
	Prepare(ctx context.Context) (Prepared, error)
}

// Prepared is the on-disk result of materializing the driver.
type Prepared struct {
	AppAPKPath  string
	TestAPKPath string
	// SourceHash identifies the driver build. It is written to the
	// device so a later run can tell whether a reinstall is needed.
	SourceHash string
}

// DriverFactory builds a driver client for the given driver config.
//
// It takes the whole config rather than just the base URL because the
// client is also configured by it (the per-request timeout), and a
// factory that received only the URL silently dropped the rest.
type DriverFactory func(cfg mobile.DriverConfig) driver.Driver

// Lifecycle aggregates adb, the instrumentation launcher and the driver
// artifacts into the operations the mobile provider needs.
type Lifecycle struct {
	ADB        ADBTool
	Instrument InstrumentLauncher
	Artifacts  DriverArtifacts
	NewDriver  DriverFactory

	// serial is bound at session build time so the neutral Lifecycle
	// methods can report which device they acted on.
	serial string
	// apiLevel gates permission and recording behavior.
	apiLevel int
}

// SelectDevice resolves and boots the device for a target.
func (l *Lifecycle) SelectDevice(ctx context.Context, target mobile.Target) (adb.Device, error) {
	devices, err := l.ADB.ListDevices(ctx)
	if err != nil {
		return adb.Device{}, fmt.Errorf("list devices: %w", err)
	}

	device, err := adb.SelectDevice(devices, target.Serial)
	if err != nil {
		return adb.Device{}, fmt.Errorf("select device for target %q: %w", target.Name, err)
	}

	if err := l.ADB.WaitForBoot(ctx, device.Serial, 0); err != nil {
		return adb.Device{}, fmt.Errorf("wait for device boot: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Selected Android device: serial=%s model=%s avd=%s\n",
		device.Serial, device.Model, device.AVDName)

	return device, nil
}

// InstallApp installs (or reinstalls) the app under test.
func (l *Lifecycle) InstallApp(ctx context.Context, deviceID string, target mobile.Target) error {
	if err := l.ADB.Install(ctx, deviceID, target.AppPath); err != nil {
		return fmt.Errorf("install app: %w", err)
	}

	return nil
}

// ClearAppState returns the app to a first-launch state.
//
// `pm clear` wipes data, cache, accounts and granted runtime
// permissions in one call, which is the whole of what iOS needs an
// uninstall plus a keychain reset plus a reinstall to achieve. The app
// stays installed, so this is also markedly faster.
func (l *Lifecycle) ClearAppState(ctx context.Context, deviceID string, target mobile.Target) error {
	installed, err := l.ADB.IsInstalled(ctx, deviceID, target.AppID)
	if err != nil {
		return fmt.Errorf("clear state: %w", err)
	}

	if !installed {
		// Nothing to clear, but the scenario still needs the app there.
		return l.InstallApp(ctx, deviceID, target)
	}

	out, err := l.ADB.Shell(ctx, deviceID, "pm clear "+target.AppID)
	if err != nil {
		return fmt.Errorf("clear state: %w", err)
	}

	if !strings.Contains(out, "Success") {
		return fmt.Errorf("clear state: pm clear reported %q", strings.TrimSpace(out))
	}

	return nil
}

// SetPermission grants or revokes one privacy permission.
func (l *Lifecycle) SetPermission(ctx context.Context, deviceID string, target mobile.Target, action, service string) error {
	permissions, err := permissionsFor(service, l.apiLevel)
	if err != nil {
		return err
	}

	verb := permissionGrant
	if action == permissionRevoke {
		verb = permissionRevoke
	}

	for _, permission := range permissions {
		command := fmt.Sprintf("pm %s %s %s", verb, target.AppID, permission)

		out, err := l.ADB.Shell(ctx, deviceID, command)
		if err == nil && !isUnchangeablePermission(out, nil) {
			continue
		}

		if isUnchangeablePermission(out, err) {
			// Install-time permissions cannot be toggled; the app
			// already holds the state the scenario asked for.
			continue
		}

		return fmt.Errorf("%s %s (%s): %w", verb, service, permission, err)
	}

	return nil
}

// TerminateApp stops the app under test.
//
// force-stop rather than a back-press or an intent: the app must be
// gone, not backgrounded, so the next scenario's launch starts cold.
func (l *Lifecycle) TerminateApp(ctx context.Context, deviceID string, target mobile.Target) error {
	if _, err := l.ADB.Shell(ctx, deviceID, "am force-stop "+target.AppID); err != nil {
		return fmt.Errorf("terminate app: %w", err)
	}

	return nil
}

// TerminateDriverRunner stops the on-device instrumentation.
//
// A defensive companion to killing the adb process: the instrumentation
// can briefly outlive its host command and keep the device port bound,
// which would make the next session's driver fail to start.
func (l *Lifecycle) TerminateDriverRunner(ctx context.Context, deviceID string) error {
	if l == nil || l.ADB == nil || deviceID == "" {
		return nil
	}

	// Both packages, and the app one matters most: `am instrument` runs
	// the test code inside the *target* application's process, so
	// stopping only the .test package leaves the driver very much
	// alive — still bound to the device port, still answering health
	// checks, and about to be killed by the next instrumentation
	// mid-request.
	for _, pkg := range []string{DriverPackage, DriverTestPackage} {
		if _, err := l.ADB.Shell(ctx, deviceID, "am force-stop "+pkg); err != nil {
			return fmt.Errorf("terminate driver runner %q: %w", pkg, err)
		}
	}

	return nil
}

// ScreenshotFallback captures a PNG straight from adb, for when the
// driver's own screenshot endpoint is unreachable — which is precisely
// when a failure screenshot matters most.
func (l *Lifecycle) ScreenshotFallback(ctx context.Context, deviceID, path string) error {
	if err := l.ADB.Screencap(ctx, deviceID, path); err != nil {
		return fmt.Errorf("screenshot fallback: %w", err)
	}

	return nil
}

// EnsureDriver installs the driver if needed, forwards a host port and
// starts the instrumentation, returning a client plus the diagnostic
// paths for this session.
func (l *Lifecycle) EnsureDriver(ctx context.Context, device adb.Device, target mobile.Target) (driver.Driver, mobile.DriverHandle, mobile.Diagnostics, error) {
	if l.NewDriver == nil {
		return nil, nil, mobile.Diagnostics{}, fmt.Errorf("driver factory is not configured")
	}

	client := l.NewDriver(target.Driver)

	if target.Driver.External {
		if err := client.Health(ctx); err != nil {
			return nil, nil, mobile.Diagnostics{}, fmt.Errorf("external driver health: %w", err)
		}

		return client, nil, mobile.Diagnostics{}, nil
	}

	if l.Artifacts == nil {
		return nil, nil, mobile.Diagnostics{}, fmt.Errorf(
			"config.mobile.targets.%s.driver: no embedded driver in this build "+
				"(set driver.external = true to connect to an already-running driver)", target.Name)
	}

	prepared, err := l.Artifacts.Prepare(ctx)
	if err != nil {
		return nil, nil, mobile.Diagnostics{}, fmt.Errorf("prepare driver: %w", err)
	}

	// Stop any driver left behind by an earlier run before starting
	// ours.
	//
	// A device can only host one instrumentation per package, so
	// starting a second one kills the first. If a previous run was
	// interrupted, its driver is still bound to the device port: our
	// health check succeeds against it, the first request goes to it,
	// and then our own instrumentation starts and kills it mid-request.
	// The step fails with a bare EOF and the log shows two drivers, one
	// of which never answered. Clearing the ground first makes the run
	// depend on this run's driver only.
	if err := l.TerminateDriverRunner(ctx, device.Serial); err != nil {
		return nil, nil, mobile.Diagnostics{}, fmt.Errorf("stop a previous driver: %w", err)
	}

	if err := l.installDriver(ctx, device.Serial, prepared); err != nil {
		return nil, nil, mobile.Diagnostics{}, err
	}

	if err := l.ADB.Forward(ctx, device.Serial, target.Driver.Port, DriverDevicePort); err != nil {
		return nil, nil, mobile.Diagnostics{}, fmt.Errorf("forward driver port: %w", err)
	}

	logPath := driverLogPath(target.Name)
	diagnostics := mobile.Diagnostics{
		Artifacts: []mobile.Artifact{{Type: mobile.ArtifactTypeDriverLog, Path: logPath}},
	}

	handle, err := l.Instrument.Start(ctx, instrument.Options{
		ADBPath:     l.ADB.Binary(),
		Serial:      device.Serial,
		TestPackage: DriverTestPackage,
		DevicePort:  DriverDevicePort,
		LogPath:     logPath,
	}, client)
	if err != nil {
		l.ADB.RemoveForward(ctx, device.Serial, target.Driver.Port)

		return nil, nil, diagnostics, fmt.Errorf("start android driver: %w", err)
	}

	return client, &driverHandle{
		handle:   handle,
		adb:      l.ADB,
		serial:   device.Serial,
		hostPort: target.Driver.Port,
	}, diagnostics, nil
}

// installDriver installs both driver APKs unless the device already
// carries this exact build.
//
// The check is a marker file holding the driver's source hash: an
// unchanged driver skips two installs, which on an emulator is several
// seconds per scenario batch.
func (l *Lifecycle) installDriver(ctx context.Context, serial string, prepared Prepared) error {
	markerPath := "/data/local/tmp/tales-driver.hash"

	if prepared.SourceHash != "" {
		out, err := l.ADB.Shell(ctx, serial, "cat "+markerPath+" 2>/dev/null")
		if err == nil && strings.TrimSpace(out) == prepared.SourceHash {
			installed, checkErr := l.ADB.IsInstalled(ctx, serial, DriverTestPackage)
			if checkErr == nil && installed {
				return nil
			}
		}
	}

	if err := l.ADB.Install(ctx, serial, prepared.AppAPKPath); err != nil {
		return fmt.Errorf("install driver app: %w", err)
	}

	if err := l.ADB.Install(ctx, serial, prepared.TestAPKPath); err != nil {
		return fmt.Errorf("install driver instrumentation: %w", err)
	}

	if prepared.SourceHash != "" {
		// Best-effort: a failed marker write only costs a reinstall
		// next time.
		_, _ = l.ADB.Shell(ctx, serial, fmt.Sprintf("echo %s > %s", prepared.SourceHash, markerPath))
	}

	return nil
}

// CaptureDeviceLog writes a bounded logcat dump, attached to a failing
// step alongside the screenshot and hierarchy. It implements
// mobile.DeviceLogDumper.
//
// The dump is deliberately unfiltered. It used to select the
// "tales-driver" tag, which is the one thing that cannot explain the
// failures worth explaining: a launcher ANR, an app crash and a slow
// cold start are all reported by other processes, and two Android CI
// runs were lost to exactly those without a line of evidence.
func (l *Lifecycle) CaptureDeviceLog(ctx context.Context, serial, path string) error {
	out, err := l.ADB.Logcat(ctx, serial, "", logcatLines)
	if err != nil {
		return fmt.Errorf("capture logcat: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create logcat dir: %w", err)
	}

	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return fmt.Errorf("write logcat: %w", err)
	}

	return nil
}

// driverHandle stops the instrumentation and releases the host port.
type driverHandle struct {
	handle   *instrument.Handle
	adb      ADBTool
	serial   string
	hostPort int
}

func (h *driverHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}

	err := h.handle.Stop(ctx)

	// Release the forward regardless: a stale mapping would make the
	// next session's health check talk to nothing.
	if h.adb != nil {
		h.adb.RemoveForward(ctx, h.serial, h.hostPort)
	}

	if err != nil {
		return fmt.Errorf("stop driver: %w", err)
	}

	return nil
}

// driverLogPath returns the per-target driver log location, mirroring
// the Apple backend's layout so both platforms' logs sit side by side.
func driverLogPath(targetName string) string {
	safe := unsafeLogSegment.ReplaceAllString(targetName, "_")
	if safe == "" {
		safe = "target"
	}

	return filepath.Join(driverLogsBase, safe, "driver.log")
}
