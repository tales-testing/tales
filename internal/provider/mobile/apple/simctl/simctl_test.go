package simctl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCall struct {
	name string
	args []string
}

type fakeRunner struct {
	calls   []fakeCall
	outputs map[string][]byte
	errors  map[string]error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}
}

func (f *fakeRunner) key(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := f.key(name, args)
	f.calls = append(f.calls, fakeCall{name: name, args: args})

	if err, ok := f.errors[key]; ok {
		return f.outputs[key], err
	}

	if out, ok := f.outputs[key]; ok {
		return out, nil
	}

	return []byte{}, nil
}

const sampleDeviceList = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-17-0": [
      {"udid":"AAA","name":"iPhone 16","state":"Shutdown","isAvailable":true},
      {"udid":"BBB","name":"iPhone 16","state":"Booted","isAvailable":true}
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-16-4": [
      {"udid":"CCC","name":"iPhone 14","state":"Shutdown","isAvailable":false}
    ]
  }
}`

const duplicateDeviceList = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-26-4": [
      {"udid":"OLD","name":"iPhone 17","state":"Booted","isAvailable":true},
      {"udid":"UNAVAILABLE","name":"iPhone 17","state":"Shutdown","isAvailable":false}
    ],
    "com.apple.CoreSimulator.SimRuntime.tvOS-26-4": [
      {"udid":"TV","name":"iPhone 17","state":"Booted","isAvailable":true}
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-26-5": [
      {"udid":"NEW-Z","name":"iPhone 17","state":"Shutdown","isAvailable":true},
      {"udid":"NEW-A","name":"iPhone 17","state":"Shutdown","isAvailable":true}
    ]
  }
}`

const bootedSameRuntimeList = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-26-5": [
      {"udid":"SHUTDOWN","name":"iPhone 17","state":"Shutdown","isAvailable":true},
      {"udid":"BOOTED","name":"iPhone 17","state":"Booted","isAvailable":true}
    ]
  }
}`

func TestListDevicesParsesJSON(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(sampleDeviceList)

	tool := New(fake)

	devices, err := tool.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
}

func TestFindDeviceByNamePrefersBooted(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(sampleDeviceList)

	tool := New(fake)

	device, err := tool.FindDeviceByName(context.Background(), "iPhone 16")
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	if device.UDID != "BBB" || !device.Booted() {
		t.Fatalf("expected booted BBB, got %+v", device)
	}
}

func TestFindDeviceByNamePrefersNewestIOSRuntime(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(duplicateDeviceList)

	device, err := New(fake).FindDeviceByName(context.Background(), "iPhone 17")
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	if device.UDID != "NEW-A" {
		t.Fatalf("expected newest iOS runtime with deterministic UDID tie-break, got %+v", device)
	}
}

func TestFindDeviceByNamePrefersBootedWithinSameRuntime(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(bootedSameRuntimeList)

	device, err := New(fake).FindDeviceByName(context.Background(), "iPhone 17")
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	if device.UDID != "BOOTED" {
		t.Fatalf("expected booted device in same runtime, got %+v", device)
	}
}

func TestFindDeviceByNameIgnoresUnavailableDevices(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(`{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-5":[{"udid":"BAD","name":"iPhone 17","state":"Shutdown","isAvailable":false}]}}`)

	_, err := New(fake).FindDeviceByName(context.Background(), "iPhone 17")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing error for unavailable-only device, got %v", err)
	}
}

func TestFindDeviceByNameUnavailable(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(sampleDeviceList)

	tool := New(fake)

	_, err := tool.FindDeviceByName(context.Background(), "Galaxy")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestBootArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	tool := New(fake)

	if err := tool.Boot(context.Background(), "AAA"); err != nil {
		t.Fatalf("boot: %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0].name != "xcrun" {
		t.Fatalf("unexpected calls: %+v", fake.calls)
	}

	wantArgs := []string{"simctl", "boot", "AAA"}
	if !equalArgs(fake.calls[0].args, wantArgs) {
		t.Fatalf("expected %v, got %v", wantArgs, fake.calls[0].args)
	}
}

func TestBootAlreadyBootedIsSuccess(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.errors[fake.key("xcrun", []string{"simctl", "boot", "AAA"})] = errors.New("simctl boot AAA: Booted (already booted)")
	tool := New(fake)

	if err := tool.Boot(context.Background(), "AAA"); err != nil {
		t.Fatalf("expected nil for already-booted, got %v", err)
	}
}

func TestWaitBootedArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	tool := New(fake)

	if err := tool.WaitBooted(context.Background(), "AAA", time.Second); err != nil {
		t.Fatalf("bootstatus: %v", err)
	}

	if !equalArgs(fake.calls[0].args, []string{"simctl", "bootstatus", "AAA", "-b"}) {
		t.Fatalf("unexpected args: %+v", fake.calls[0].args)
	}
}

func TestCoreSimulatorStaleErrorAddsHint(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.errors[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = errors.New("CoreSimulatorService connection became invalid")
	tool := New(fake)

	_, err := tool.ListDevices(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sudo xcodebuild -runFirstLaunch") {
		t.Fatalf("expected recovery hint, got %v", err)
	}
}

func TestInstallArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	tool := New(fake)

	if err := tool.Install(context.Background(), "AAA", "./MyApp.app"); err != nil {
		t.Fatalf("install: %v", err)
	}

	if len(fake.calls) != 1 || !equalArgs(fake.calls[0].args, []string{"simctl", "install", "AAA", "./MyApp.app"}) {
		t.Fatalf("unexpected calls: %+v", fake.calls)
	}
}

func TestLaunchArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	tool := New(fake)

	if err := tool.Launch(context.Background(), "AAA", "com.example.MyApp"); err != nil {
		t.Fatalf("launch: %v", err)
	}

	if !equalArgs(fake.calls[0].args, []string{"simctl", "launch", "AAA", "com.example.MyApp"}) {
		t.Fatalf("unexpected args: %+v", fake.calls[0].args)
	}
}

func TestTerminateArgsAndNotRunningIsSuccess(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.errors[fake.key("xcrun", []string{"simctl", "terminate", "AAA", "com.example.MyApp"})] = errors.New("found nothing to terminate")
	tool := New(fake)

	if err := tool.Terminate(context.Background(), "AAA", "com.example.MyApp"); err != nil {
		t.Fatalf("expected nil for not-running, got %v", err)
	}
}

func TestUninstallNotInstalledIsSuccess(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.errors[fake.key("xcrun", []string{"simctl", "uninstall", "AAA", "com.example.MyApp"})] = errors.New("No such application")
	tool := New(fake)

	if err := tool.Uninstall(context.Background(), "AAA", "com.example.MyApp"); err != nil {
		t.Fatalf("expected nil for not-installed, got %v", err)
	}
}

func TestScreenshotArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	tool := New(fake)

	if err := tool.Screenshot(context.Background(), "AAA", "/tmp/x.png"); err != nil {
		t.Fatalf("screenshot: %v", err)
	}

	if !equalArgs(fake.calls[0].args, []string{"simctl", "io", "AAA", "screenshot", "/tmp/x.png"}) {
		t.Fatalf("unexpected args: %+v", fake.calls[0].args)
	}
}

func TestListDevicesRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	fake := newFakeRunner()
	fake.outputs[fake.key("xcrun", []string{"simctl", "list", "devices", "--json"})] = []byte(`not json`)
	tool := New(fake)

	if _, err := tool.ListDevices(context.Background()); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
