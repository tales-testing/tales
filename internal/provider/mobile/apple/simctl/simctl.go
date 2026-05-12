// Package simctl wraps a small, curated subset of `xcrun simctl` commands
// behind a Runner-backed API so the rest of the mobile provider can drive
// iOS Simulator lifecycle (boot, install, launch, terminate, ...) without
// shelling out from multiple places. All commands take an apple.Runner so
// unit tests can substitute a fake executor.
package simctl

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
)

// Tool is the simctl facade. Construct one with apple.ExecRunner{} in
// production and a fake Runner in tests.
type Tool struct {
	runner apple.Runner
}

// New returns a Tool that executes commands through the given runner.
func New(runner apple.Runner) *Tool {
	return &Tool{runner: runner}
}

// Device describes a single simulator entry.
type Device struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Runtime     string `json:"runtime"`
	IsAvailable bool   `json:"isAvailable"`
}

// Booted reports whether the simulator is already booted.
func (d Device) Booted() bool {
	return strings.EqualFold(d.State, "booted")
}

// ListDevices runs `xcrun simctl list devices --json` and returns a flat list
// of devices across runtimes.
func (t *Tool) ListDevices(ctx context.Context) ([]Device, error) {
	out, err := t.runner.Run(ctx, "xcrun", "simctl", "list", "devices", "--json")
	if err != nil {
		return nil, withCoreSimulatorHint(fmt.Errorf("list devices: %w", err))
	}

	return parseDevicesJSON(out)
}

// FindDeviceByName returns the deterministic best available iOS simulator
// matching the given device name.
func (t *Tool) FindDeviceByName(ctx context.Context, name string) (Device, error) {
	devices, err := t.ListDevices(ctx)
	if err != nil {
		return Device{}, err
	}

	candidates := make([]Device, 0)

	for _, d := range devices {
		if d.Name != name || !d.IsAvailable || !isIOSRuntime(d.Runtime) {
			continue
		}

		candidates = append(candidates, d)
	}

	if len(candidates) == 0 {
		return Device{}, fmt.Errorf("simulator %q not found among available iOS devices", name)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return preferredOver(candidates[i], candidates[j])
	})

	return candidates[0], nil
}

func preferredOver(candidate, current Device) bool {
	candidateVersion := runtimeVersion(candidate.Runtime)
	currentVersion := runtimeVersion(current.Runtime)

	if cmp := compareVersions(candidateVersion, currentVersion); cmp != 0 {
		return cmp > 0
	}

	if candidate.Booted() != current.Booted() {
		return candidate.Booted()
	}

	if candidate.UDID != current.UDID {
		return candidate.UDID < current.UDID
	}

	return candidate.Runtime < current.Runtime
}

// Boot boots the given simulator. Booting an already-booted device is treated
// as success because simctl will print a non-fatal warning and exit non-zero.
func (t *Tool) Boot(ctx context.Context, udid string) error {
	if udid == "" {
		return fmt.Errorf("boot: udid is required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "boot", udid); err != nil {
		if isAlreadyBooted(err) {
			return nil
		}

		return withCoreSimulatorHint(fmt.Errorf("boot %s: %w", udid, err))
	}

	return nil
}

// WaitBooted blocks until simctl reports the simulator has completed boot.
func (t *Tool) WaitBooted(ctx context.Context, udid string, timeout time.Duration) error {
	if udid == "" {
		return fmt.Errorf("bootstatus: udid is required")
	}

	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	bootCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := t.runner.Run(bootCtx, "xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
		return withCoreSimulatorHint(fmt.Errorf("bootstatus %s: %w", udid, err))
	}

	return nil
}

// Install installs the given .app bundle onto the simulator.
func (t *Tool) Install(ctx context.Context, udid, appPath string) error {
	if udid == "" || appPath == "" {
		return fmt.Errorf("install: udid and appPath are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "install", udid, appPath); err != nil {
		return withCoreSimulatorHint(fmt.Errorf("install %s on %s: %w", appPath, udid, err))
	}

	return nil
}

// Uninstall removes the application identified by bundleID from the simulator.
// Uninstalling an app that is not installed is treated as success.
func (t *Tool) Uninstall(ctx context.Context, udid, bundleID string) error {
	if udid == "" || bundleID == "" {
		return fmt.Errorf("uninstall: udid and bundleID are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "uninstall", udid, bundleID); err != nil {
		if isNotInstalled(err) {
			return nil
		}

		return withCoreSimulatorHint(fmt.Errorf("uninstall %s on %s: %w", bundleID, udid, err))
	}

	return nil
}

// Launch launches the application identified by bundleID on the simulator.
func (t *Tool) Launch(ctx context.Context, udid, bundleID string) error {
	if udid == "" || bundleID == "" {
		return fmt.Errorf("launch: udid and bundleID are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "launch", udid, bundleID); err != nil {
		return withCoreSimulatorHint(fmt.Errorf("launch %s on %s: %w", bundleID, udid, err))
	}

	return nil
}

// Terminate terminates the application identified by bundleID. Terminating a
// non-running app is treated as success.
func (t *Tool) Terminate(ctx context.Context, udid, bundleID string) error {
	if udid == "" || bundleID == "" {
		return fmt.Errorf("terminate: udid and bundleID are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "terminate", udid, bundleID); err != nil {
		if isNotRunning(err) {
			return nil
		}

		return withCoreSimulatorHint(fmt.Errorf("terminate %s on %s: %w", bundleID, udid, err))
	}

	return nil
}

// Screenshot writes a PNG screenshot of the simulator to the given path.
func (t *Tool) Screenshot(ctx context.Context, udid, path string) error {
	if udid == "" || path == "" {
		return fmt.Errorf("screenshot: udid and path are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "io", udid, "screenshot", path); err != nil {
		return withCoreSimulatorHint(fmt.Errorf("screenshot %s to %s: %w", udid, path, err))
	}

	return nil
}

type rawDeviceList struct {
	Devices map[string][]Device `json:"devices"`
}

func parseDevicesJSON(out []byte) ([]Device, error) {
	var raw rawDeviceList

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode simctl devices: %w", err)
	}

	devices := make([]Device, 0)

	for runtime, list := range raw.Devices {
		for _, d := range list {
			if d.Runtime == "" {
				d.Runtime = runtime
			}

			devices = append(devices, d)
		}
	}

	return devices, nil
}

func isIOSRuntime(runtime string) bool {
	return strings.Contains(runtime, ".SimRuntime.iOS-")
}

var runtimeVersionPattern = regexp.MustCompile(`iOS-([0-9]+(?:-[0-9]+)*)`)

func runtimeVersion(runtime string) []int {
	matches := runtimeVersionPattern.FindStringSubmatch(runtime)
	if len(matches) != 2 {
		return nil
	}

	parts := strings.Split(matches[1], "-")
	version := make([]int, 0, len(parts))

	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}

		version = append(version, n)
	}

	return version
}

func compareVersions(left, right []int) int {
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}

	for i := range maxLen {
		var l, r int
		if i < len(left) {
			l = left[i]
		}

		if i < len(right) {
			r = right[i]
		}

		if l > r {
			return 1
		}

		if l < r {
			return -1
		}
	}

	return 0
}

const coreSimulatorRecoveryHint = `CoreSimulator appears unhealthy or stale after Xcode upgrade.
Run:
  sudo xcodebuild -runFirstLaunch
  xcrun simctl shutdown all
  killall -9 com.apple.CoreSimulator.CoreSimulatorService || true
  xcrun simctl list devices`

func withCoreSimulatorHint(err error) error {
	if err == nil || !looksLikeCoreSimulatorIssue(err) {
		return err
	}

	return fmt.Errorf("%w\n%s", err, coreSimulatorRecoveryHint)
}

func looksLikeCoreSimulatorIssue(err error) bool {
	msg := err.Error()
	needles := []string{
		"CoreSimulatorService connection became invalid",
		"CoreSimulator is out of date",
		"Framework version",
		"existing job version",
		"Failed to initialize simulator device set",
		"Unable to locate device set",
		"simdiskimaged",
	}

	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}

	return false
}

func isAlreadyBooted(err error) bool {
	return errorContains(err, "Booted", "already booted")
}

func isNotInstalled(err error) bool {
	return errorContains(err, "No such application", "is not installed")
}

func isNotRunning(err error) bool {
	return errorContains(err, "not currently running", "found nothing to terminate")
}

func errorContains(err error, needles ...string) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}

	return false
}
