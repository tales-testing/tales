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
	"strings"

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
		return nil, fmt.Errorf("list devices: %w", err)
	}

	return parseDevicesJSON(out)
}

// FindDeviceByName returns the most-available device matching the given name.
// Booted devices are preferred over shut-down ones; available devices over
// unavailable ones. An error is returned when no device matches.
func (t *Tool) FindDeviceByName(ctx context.Context, name string) (Device, error) {
	devices, err := t.ListDevices(ctx)
	if err != nil {
		return Device{}, err
	}

	var (
		best  Device
		found bool
	)

	for _, d := range devices {
		if d.Name != name {
			continue
		}

		if !found {
			best = d
			found = true

			continue
		}

		if preferredOver(d, best) {
			best = d
		}
	}

	if !found {
		return Device{}, fmt.Errorf("simulator %q not found", name)
	}

	return best, nil
}

func preferredOver(candidate, current Device) bool {
	if candidate.Booted() && !current.Booted() {
		return true
	}

	if candidate.IsAvailable && !current.IsAvailable {
		return true
	}

	return false
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

		return fmt.Errorf("boot %s: %w", udid, err)
	}

	return nil
}

// Install installs the given .app bundle onto the simulator.
func (t *Tool) Install(ctx context.Context, udid, appPath string) error {
	if udid == "" || appPath == "" {
		return fmt.Errorf("install: udid and appPath are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "install", udid, appPath); err != nil {
		return fmt.Errorf("install %s on %s: %w", appPath, udid, err)
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

		return fmt.Errorf("uninstall %s on %s: %w", bundleID, udid, err)
	}

	return nil
}

// Launch launches the application identified by bundleID on the simulator.
func (t *Tool) Launch(ctx context.Context, udid, bundleID string) error {
	if udid == "" || bundleID == "" {
		return fmt.Errorf("launch: udid and bundleID are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "launch", udid, bundleID); err != nil {
		return fmt.Errorf("launch %s on %s: %w", bundleID, udid, err)
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

		return fmt.Errorf("terminate %s on %s: %w", bundleID, udid, err)
	}

	return nil
}

// Screenshot writes a PNG screenshot of the simulator to the given path.
func (t *Tool) Screenshot(ctx context.Context, udid, path string) error {
	if udid == "" || path == "" {
		return fmt.Errorf("screenshot: udid and path are required")
	}

	if _, err := t.runner.Run(ctx, "xcrun", "simctl", "io", udid, "screenshot", path); err != nil {
		return fmt.Errorf("screenshot %s to %s: %w", udid, path, err)
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
