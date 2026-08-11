package adb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner scripts adb invocations: each call is matched against the
// recorded responses by the joined argument list, so a test states the
// device output it wants without spawning anything.
type fakeRunner struct {
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	out []byte
	err error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, name+" "+key)

	if resp, ok := f.responses[key]; ok {
		return resp.out, resp.err
	}

	return nil, nil
}

func newTool(responses map[string]fakeResponse) (*Tool, *fakeRunner) {
	runner := &fakeRunner{responses: responses}

	return New("/sdk/platform-tools/adb", runner), runner
}

func TestParseDevicesReadsTheTable(t *testing.T) {
	t.Parallel()

	out := `List of devices attached
emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a avd:tales-e2e
R58M12345XY            unauthorized usb:1-1
`

	devices := parseDevices(out)

	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(devices), devices)
	}

	if devices[0].Serial != "emulator-5554" || !devices[0].Ready() || !devices[0].IsEmulator() {
		t.Fatalf("unexpected first device: %+v", devices[0])
	}

	if devices[0].AVDName != "tales-e2e" {
		t.Fatalf("expected the avd name to be read, got %q", devices[0].AVDName)
	}

	if devices[1].Ready() {
		t.Fatalf("an unauthorized device must not be reported ready: %+v", devices[1])
	}
}

func TestSelectDeviceUsesTheOnlyReadyDevice(t *testing.T) {
	t.Parallel()

	devices := []Device{
		{Serial: "emulator-5554", State: "device"},
		{Serial: "offline-1", State: "offline"},
	}

	device, err := SelectDevice(devices, "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if device.Serial != "emulator-5554" {
		t.Fatalf("selected %q", device.Serial)
	}
}

func TestSelectDeviceRefusesToGuessBetweenSeveral(t *testing.T) {
	t.Parallel()

	// Silently picking one would make a run depend on which device
	// happened to be listed first, which is exactly the kind of
	// irreproducibility Tales is built to avoid.
	devices := []Device{
		{Serial: "emulator-5556", State: "device"},
		{Serial: "emulator-5554", State: "device"},
	}

	_, err := SelectDevice(devices, "")
	if err == nil {
		t.Fatal("expected an error when several devices are ready")
	}

	msg := err.Error()

	for _, want := range []string{"emulator-5554", "emulator-5556", "serial"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error should mention %q, got: %s", want, msg)
		}
	}
}

func TestSelectDeviceHonoursAnExplicitSerial(t *testing.T) {
	t.Parallel()

	devices := []Device{
		{Serial: "emulator-5554", State: "device"},
		{Serial: "emulator-5556", State: "device"},
	}

	device, err := SelectDevice(devices, "emulator-5556")
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if device.Serial != "emulator-5556" {
		t.Fatalf("selected %q", device.Serial)
	}
}

func TestSelectDeviceRejectsAnUnreadyPinnedSerial(t *testing.T) {
	t.Parallel()

	devices := []Device{{Serial: "R58M12345XY", State: "unauthorized"}}

	_, err := SelectDevice(devices, "R58M12345XY")
	if err == nil {
		t.Fatal("expected an error for an unauthorized device")
	}

	if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("error should surface the device state, got: %v", err)
	}
}

func TestSelectDeviceReportsWhenNothingIsAttached(t *testing.T) {
	t.Parallel()

	_, err := SelectDevice(nil, "")
	if err == nil {
		t.Fatal("expected an error when no device is attached")
	}

	if !strings.Contains(err.Error(), "emulator") {
		t.Fatalf("error should hint at starting an emulator, got: %v", err)
	}
}

func TestInstallPassesTheFlagsTheDriverNeeds(t *testing.T) {
	t.Parallel()

	tool, runner := newTool(nil)

	if err := tool.Install(context.Background(), "emulator-5554", "/tmp/driver.apk"); err != nil {
		t.Fatalf("install: %v", err)
	}

	call := runner.calls[0]

	// -r to reinstall over a previous run, -t because the
	// instrumentation APK is test-only, -g so a pre-granted permission
	// does not surface as a dialog mid-scenario.
	for _, flag := range []string{"install", "-r", "-t", "-g", "/tmp/driver.apk"} {
		if !strings.Contains(call, flag) {
			t.Fatalf("install call missing %q: %s", flag, call)
		}
	}
}

func TestUninstallTreatsAMissingPackageAsSuccess(t *testing.T) {
	t.Parallel()

	tool, _ := newTool(map[string]fakeResponse{
		"-s emulator-5554 uninstall org.example.app": {
			out: []byte("Failure [DELETE_FAILED_INTERNAL_ERROR]"),
			err: errors.New("exit status 1"),
		},
	})

	// Callers clean up unconditionally, so "it was not there" must not
	// be an error.
	if err := tool.Uninstall(context.Background(), "emulator-5554", "org.example.app"); err != nil {
		t.Fatalf("expected a missing package to be tolerated, got %v", err)
	}
}

func TestIsInstalledMatchesTheExactPackage(t *testing.T) {
	t.Parallel()

	// `pm list packages foo` is a prefix match, so the reply routinely
	// contains neighbours; only an exact line counts.
	tool, _ := newTool(map[string]fakeResponse{
		"-s emulator-5554 shell pm list packages org.example.app": {
			out: []byte("package:org.example.app.debug\npackage:org.example.app\n"),
		},
		"-s emulator-5554 shell pm list packages org.example.absent": {
			out: []byte("package:org.example.absentee\n"),
		},
	})

	present, err := tool.IsInstalled(context.Background(), "emulator-5554", "org.example.app")
	if err != nil {
		t.Fatalf("is installed: %v", err)
	}

	if !present {
		t.Fatal("expected org.example.app to be reported installed")
	}

	absent, err := tool.IsInstalled(context.Background(), "emulator-5554", "org.example.absent")
	if err != nil {
		t.Fatalf("is installed: %v", err)
	}

	if absent {
		t.Fatal("a prefix match must not count as installed")
	}
}

func TestForwardBuildsTheTCPMapping(t *testing.T) {
	t.Parallel()

	tool, runner := newTool(nil)

	if err := tool.Forward(context.Background(), "emulator-5554", 41000, 9080); err != nil {
		t.Fatalf("forward: %v", err)
	}

	if !strings.Contains(runner.calls[0], "forward tcp:41000 tcp:9080") {
		t.Fatalf("unexpected forward call: %s", runner.calls[0])
	}
}

func TestRemoveForwardToleratesAMissingMapping(t *testing.T) {
	t.Parallel()

	tool, _ := newTool(map[string]fakeResponse{
		"-s emulator-5554 forward --remove tcp:41000": {err: errors.New("exit status 1")},
	})

	// This runs on the cleanup path, where a previous crash may already
	// have taken the mapping with it, so removal reports nothing at all
	// rather than turning a green run red at teardown.
	tool.RemoveForward(context.Background(), "emulator-5554", 41000)
}

func TestAPILevelParsesTheProperty(t *testing.T) {
	t.Parallel()

	tool, _ := newTool(map[string]fakeResponse{
		"-s emulator-5554 shell getprop ro.build.version.sdk": {out: []byte("34\n")},
	})

	level, err := tool.APILevel(context.Background(), "emulator-5554")
	if err != nil {
		t.Fatalf("api level: %v", err)
	}

	if level != 34 {
		t.Fatalf("api level = %d, want 34", level)
	}
}

func TestAPILevelReportsUnparseableOutput(t *testing.T) {
	t.Parallel()

	tool, _ := newTool(map[string]fakeResponse{
		"-s emulator-5554 shell getprop ro.build.version.sdk": {out: []byte("\n")},
	})

	if _, err := tool.APILevel(context.Background(), "emulator-5554"); err == nil {
		t.Fatal("expected an error for a device that did not report its API level")
	}
}
