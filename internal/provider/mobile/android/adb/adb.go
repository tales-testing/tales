package adb

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Runner executes an external command and returns its combined output.
//
// Declared here rather than imported from the parent android package so
// this stays a leaf: android wires adb into the backend, so depending
// back on it would cycle. Go interfaces are structural, so the parent's
// ExecRunner satisfies this without declaring anything.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Tool is the adb facade. Construct one with the resolved binary path
// and a Runner; tests pass a fake Runner.
type Tool struct {
	binary string
	runner Runner
}

// New returns a Tool invoking the adb binary at path.
func New(binary string, runner Runner) *Tool {
	return &Tool{binary: binary, runner: runner}
}

// Binary returns the adb executable path this Tool drives.
func (t *Tool) Binary() string { return t.binary }

// Device is one entry from `adb devices -l`.
type Device struct {
	// Serial addresses the device in every later adb call
	// ("emulator-5554", a USB serial, "host:port" for network devices).
	Serial string
	// State is adb's own word for readiness: "device" means usable,
	// "offline" and "unauthorized" do not.
	State string
	// Model and AVDName are best-effort labels from `-l`; AVDName is
	// only present for emulators.
	Model   string
	AVDName string
}

// Ready reports whether the device can accept commands.
func (d Device) Ready() bool { return d.State == "device" }

// IsEmulator reports whether the serial denotes a local emulator.
func (d Device) IsEmulator() bool { return strings.HasPrefix(d.Serial, "emulator-") }

// ListDevices runs `adb devices -l` and returns every attached device,
// including ones that are not ready — the caller decides whether an
// offline device is an error or something to wait on.
func (t *Tool) ListDevices(ctx context.Context) ([]Device, error) {
	out, err := t.runner.Run(ctx, t.binary, "devices", "-l")
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}

	return parseDevices(string(out)), nil
}

// parseDevices reads the `adb devices -l` table. The first line is a
// header and is skipped; each later line is
//
//	<serial> <state> [key:value ...]
func parseDevices(out string) []Device {
	devices := make([]Device, 0, 2)

	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		device := Device{Serial: fields[0], State: fields[1]}

		for _, field := range fields[2:] {
			key, value, found := strings.Cut(field, ":")
			if !found {
				continue
			}

			switch key {
			case "model":
				device.Model = value
			case "avd":
				device.AVDName = value
			}
		}

		devices = append(devices, device)
	}

	return devices
}

// SelectDevice resolves which device to drive.
//
// Resolution is deliberately deterministic, because a run that silently
// picks a different device between invocations is worse than one that
// refuses to guess:
//
//  1. an explicit serial must exist and be ready;
//  2. otherwise, if exactly one device is ready, use it;
//  3. otherwise the choice is ambiguous and the caller is told to pin
//     one with `serial`.
func SelectDevice(devices []Device, serial string) (Device, error) {
	if serial != "" {
		for _, device := range devices {
			if device.Serial != serial {
				continue
			}

			if !device.Ready() {
				return Device{}, fmt.Errorf("device %q is %s, not ready", serial, device.State)
			}

			return device, nil
		}

		return Device{}, fmt.Errorf("device %q is not attached", serial)
	}

	ready := make([]Device, 0, len(devices))

	for _, device := range devices {
		if device.Ready() {
			ready = append(ready, device)
		}
	}

	switch len(ready) {
	case 0:
		return Device{}, fmt.Errorf("no ready Android device; start an emulator or attach a device")
	case 1:
		return ready[0], nil
	}

	sort.Slice(ready, func(i, j int) bool { return ready[i].Serial < ready[j].Serial })

	names := make([]string, 0, len(ready))
	for _, device := range ready {
		names = append(names, device.Serial)
	}

	return Device{}, fmt.Errorf(
		"%d devices are attached (%s); set config.mobile.targets.<name>.serial to pick one",
		len(ready), strings.Join(names, ", "),
	)
}

// WaitForBoot blocks until the device finishes booting.
//
// `adb wait-for-device` only waits for adbd, which answers long before
// the framework is up; installing or launching in that window fails in
// confusing ways. sys.boot_completed is the property that marks a
// usable device.
func (t *Tool) WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultBootTimeout
	}

	bootCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := t.runner.Run(bootCtx, t.binary, "-s", serial, "wait-for-device"); err != nil {
		return fmt.Errorf("wait for device %q: %w", serial, err)
	}

	ticker := time.NewTicker(BootPollInterval)
	defer ticker.Stop()

	for {
		out, err := t.Shell(bootCtx, serial, "getprop sys.boot_completed")
		if err == nil && strings.TrimSpace(out) == "1" {
			return nil
		}

		select {
		case <-bootCtx.Done():
			return fmt.Errorf("device %q did not finish booting within %s", serial, timeout)
		case <-ticker.C:
		}
	}
}

// APILevel returns the device's SDK level, which gates a few commands
// (screenrecord's unlimited duration, the media permission split).
func (t *Tool) APILevel(ctx context.Context, serial string) (int, error) {
	out, err := t.Shell(ctx, serial, "getprop ro.build.version.sdk")
	if err != nil {
		return 0, fmt.Errorf("read api level: %w", err)
	}

	level, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse api level %q: %w", strings.TrimSpace(out), err)
	}

	return level, nil
}

// Install installs (or reinstalls) an APK.
//
// -r reinstalls over an existing copy, -t allows test-only packages
// (which the instrumentation APK is), and -g pre-grants the runtime
// permissions declared in the manifest so a scenario is not derailed by
// a permission dialog it never asked about.
func (t *Tool) Install(ctx context.Context, serial, apkPath string) error {
	if _, err := t.runner.Run(ctx, t.binary, "-s", serial, "install", "-r", "-t", "-g", apkPath); err != nil {
		return fmt.Errorf("install %q: %w", apkPath, err)
	}

	return nil
}

// Uninstall removes a package. Uninstalling one that is not installed is
// treated as success, so callers can clean up unconditionally.
func (t *Tool) Uninstall(ctx context.Context, serial, packageName string) error {
	out, err := t.runner.Run(ctx, t.binary, "-s", serial, "uninstall", packageName)
	if err != nil {
		if isNotInstalled(string(out), err) {
			return nil
		}

		return fmt.Errorf("uninstall %q: %w", packageName, err)
	}

	return nil
}

// IsInstalled reports whether a package is present on the device.
func (t *Tool) IsInstalled(ctx context.Context, serial, packageName string) (bool, error) {
	out, err := t.Shell(ctx, serial, "pm list packages "+packageName)
	if err != nil {
		return false, fmt.Errorf("list packages: %w", err)
	}

	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "package:"+packageName {
			return true, nil
		}
	}

	return false, nil
}

// Shell runs a command in the device shell and returns its output.
func (t *Tool) Shell(ctx context.Context, serial, command string) (string, error) {
	out, err := t.runner.Run(ctx, t.binary, "-s", serial, "shell", command)
	if err != nil {
		return string(out), fmt.Errorf("shell %q: %w", command, err)
	}

	return string(out), nil
}

// Forward maps a host TCP port onto a device TCP port.
func (t *Tool) Forward(ctx context.Context, serial string, hostPort, devicePort int) error {
	local := fmt.Sprintf("tcp:%d", hostPort)
	remote := fmt.Sprintf("tcp:%d", devicePort)

	if _, err := t.runner.Run(ctx, t.binary, "-s", serial, "forward", local, remote); err != nil {
		return fmt.Errorf("forward %s -> %s: %w", local, remote, err)
	}

	return nil
}

// RemoveForward drops a host port mapping.
//
// Failure is deliberately ignored and never returned: this runs on the
// cleanup path, where the mapping may already be gone — a previous crash
// took it with it, or the device detached. Reporting that as an error
// would turn a successful run into a failed one at teardown.
func (t *Tool) RemoveForward(ctx context.Context, serial string, hostPort int) {
	local := fmt.Sprintf("tcp:%d", hostPort)

	_, _ = t.runner.Run(ctx, t.binary, "-s", serial, "forward", "--remove", local)
}

// Pull copies a file off the device.
func (t *Tool) Pull(ctx context.Context, serial, remotePath, localPath string) error {
	if _, err := t.runner.Run(ctx, t.binary, "-s", serial, "pull", remotePath, localPath); err != nil {
		return fmt.Errorf("pull %q: %w", remotePath, err)
	}

	return nil
}

// Screencap writes a PNG screenshot of the device to localPath.
//
// `exec-out` is used rather than `shell` because the latter mangles the
// binary stream with line-ending translation on some platforms.
func (t *Tool) Screencap(ctx context.Context, serial, localPath string) error {
	out, err := t.runner.Run(ctx, t.binary, "-s", serial, "exec-out", "screencap", "-p")
	if err != nil {
		return fmt.Errorf("screencap: %w", err)
	}

	if err := os.WriteFile(localPath, out, 0o600); err != nil {
		return fmt.Errorf("write screenshot: %w", err)
	}

	return nil
}

// Logcat dumps the device log for a tag and returns it. Used as a
// post-mortem artifact when the driver dies mid-scenario.
func (t *Tool) Logcat(ctx context.Context, serial, tag string, maxLines int) (string, error) {
	args := []string{"-s", serial, "logcat", "-d", "-t", strconv.Itoa(maxLines)}
	if tag != "" {
		args = append(args, "-s", tag)
	}

	out, err := t.runner.Run(ctx, t.binary, args...)
	if err != nil {
		return string(out), fmt.Errorf("logcat: %w", err)
	}

	return string(out), nil
}

// Defaults for device readiness polling.
const (
	// DefaultBootTimeout bounds the wait for sys.boot_completed. A cold
	// emulator start on a loaded CI machine can take minutes.
	DefaultBootTimeout = 3 * time.Minute
	// BootPollInterval is how often the boot property is re-read.
	BootPollInterval = time.Second
)

// isNotInstalled recognizes adb's "package not installed" refusal, which
// arrives as a non-zero exit with the reason on stdout.
func isNotInstalled(out string, err error) bool {
	haystack := strings.ToLower(out + " " + err.Error())

	return strings.Contains(haystack, "not installed") ||
		strings.Contains(haystack, "delete_failed_internal_error")
}
