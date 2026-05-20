package apple

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/provider/mobile/apple/embeddeddriver"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

type fakeSimctl struct {
	device         Device
	findErr        error
	bootCalls      atomic.Int32
	waitCalls      atomic.Int32
	installs       []string
	uninstalls     []string
	launches       []string
	terminates     []string
	keychainResets []string
}

func (f *fakeSimctl) FindDeviceByName(_ context.Context, name string) (Device, error) {
	if f.findErr != nil {
		return Device{}, f.findErr
	}

	if f.device.UDID == "" {
		f.device = Device{UDID: "UDID-" + name, Booted: false}
	}

	return f.device, nil
}

func (f *fakeSimctl) Boot(_ context.Context, _ string) error {
	f.bootCalls.Add(1)
	f.device.Booted = true

	return nil
}

func (f *fakeSimctl) WaitBooted(_ context.Context, _ string, _ time.Duration) error {
	f.waitCalls.Add(1)

	return nil
}

func (f *fakeSimctl) Install(_ context.Context, _, appPath string) error {
	f.installs = append(f.installs, appPath)

	return nil
}

func (f *fakeSimctl) Uninstall(_ context.Context, _, bundleID string) error {
	f.uninstalls = append(f.uninstalls, bundleID)

	return nil
}

func (f *fakeSimctl) Launch(_ context.Context, _, bundleID string) error {
	f.launches = append(f.launches, bundleID)

	return nil
}

func (f *fakeSimctl) Terminate(_ context.Context, _, bundleID string) error {
	f.terminates = append(f.terminates, bundleID)

	return nil
}

func (f *fakeSimctl) ResetKeychain(_ context.Context, udid string) error {
	f.keychainResets = append(f.keychainResets, udid)

	return nil
}

func (f *fakeSimctl) Privacy(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (f *fakeSimctl) Screenshot(_ context.Context, _, _ string) error {
	return nil
}

type fakeDriver struct {
	driver.NoopDriver

	healthErr error
	healthHit atomic.Int32
}

func (f *fakeDriver) Health(_ context.Context) error {
	f.healthHit.Add(1)

	return f.healthErr
}

func (f *fakeDriver) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	return &tree.ViewNode{ID: "root"}, nil
}

type fakeXcodebuild struct {
	calls    atomic.Int32
	opts     xcodebuild.Options
	err      error
	failOnce error
}

func (f *fakeXcodebuild) Start(_ context.Context, opts xcodebuild.Options, _ xcodebuild.Pinger) (*xcodebuild.Handle, error) {
	f.calls.Add(1)
	f.opts = opts

	if f.failOnce != nil {
		err := f.failOnce
		f.failOnce = nil

		return nil, err
	}

	if f.err != nil {
		return nil, f.err
	}

	return &xcodebuild.Handle{}, nil
}

func newLifecycleWithDriver(d driver.Driver) (*Lifecycle, *fakeSimctl, *fakeXcodebuild) {
	sim := &fakeSimctl{}
	xc := &fakeXcodebuild{}

	return &Lifecycle{
		Simctl:     sim,
		Xcodebuild: xc,
		NewDriver:  func(_ string) driver.Driver { return d },
	}, sim, xc
}

func sampleTarget(external bool) Target {
	return Target{
		Name:       "iphone",
		Platform:   "ios",
		DeviceName: "iPhone 16",
		AppPath:    "./MyApp.app",
		BundleID:   "com.example.MyApp",
		Driver: DriverConfig{
			Host:     "127.0.0.1",
			Port:     9080,
			External: external,
		},
	}
}

func TestEnsureBootedBootsWhenNotBooted(t *testing.T) {
	t.Parallel()

	lc, sim, _ := newLifecycleWithDriver(&fakeDriver{})

	device, err := lc.EnsureBooted(context.Background(), sampleTarget(false))
	if err != nil {
		t.Fatalf("ensure booted: %v", err)
	}

	if device.UDID == "" {
		t.Fatal("expected non-empty UDID")
	}

	if sim.bootCalls.Load() != 1 {
		t.Fatalf("expected 1 boot, got %d", sim.bootCalls.Load())
	}

	if sim.waitCalls.Load() != 1 {
		t.Fatalf("expected 1 bootstatus wait, got %d", sim.waitCalls.Load())
	}
}

func TestEnsureBootedSkipsBootIfAlreadyBooted(t *testing.T) {
	t.Parallel()

	lc, sim, _ := newLifecycleWithDriver(&fakeDriver{})
	sim.device = Device{UDID: "AAA", Booted: true}

	if _, err := lc.EnsureBooted(context.Background(), sampleTarget(false)); err != nil {
		t.Fatalf("ensure booted: %v", err)
	}

	if got := sim.bootCalls.Load(); got != 0 {
		t.Fatalf("expected 0 boots for already-booted device, got %d", got)
	}

	if got := sim.waitCalls.Load(); got != 1 {
		t.Fatalf("expected 1 bootstatus wait for already-booted device, got %d", got)
	}
}

func TestClearAppStateUninstallsAndReinstalls(t *testing.T) {
	t.Parallel()

	lc, sim, _ := newLifecycleWithDriver(&fakeDriver{})

	if err := lc.ClearAppState(context.Background(), "AAA", sampleTarget(false)); err != nil {
		t.Fatalf("clear state: %v", err)
	}

	if len(sim.terminates) != 1 || sim.terminates[0] != "com.example.MyApp" {
		t.Fatalf("expected terminate, got %v", sim.terminates)
	}

	if len(sim.uninstalls) != 1 || sim.uninstalls[0] != "com.example.MyApp" {
		t.Fatalf("expected uninstall, got %v", sim.uninstalls)
	}

	if len(sim.keychainResets) != 1 || sim.keychainResets[0] != "AAA" {
		t.Fatalf("expected keychain reset on AAA, got %v", sim.keychainResets)
	}

	if len(sim.installs) != 1 || sim.installs[0] != "./MyApp.app" {
		t.Fatalf("expected install, got %v", sim.installs)
	}
}

func TestEnsureDriverExternalSkipsXcodebuild(t *testing.T) {
	t.Parallel()

	drv := &fakeDriver{}
	lc, _, xc := newLifecycleWithDriver(drv)

	client, handle, err := lc.EnsureDriver(context.Background(), Device{UDID: "AAA"}, sampleTarget(true))
	if err != nil {
		t.Fatalf("ensure driver: %v", err)
	}

	if client == nil {
		t.Fatal("expected driver client")
	}

	if handle != nil {
		t.Fatal("expected nil handle in external mode")
	}

	if got := xc.calls.Load(); got != 0 {
		t.Fatalf("expected no xcodebuild call in external mode, got %d", got)
	}

	if got := drv.healthHit.Load(); got != 1 {
		t.Fatalf("expected one health hit, got %d", got)
	}
}

func TestEnsureDriverExternalFailsOnHealth(t *testing.T) {
	t.Parallel()

	drv := &fakeDriver{healthErr: errors.New("connection refused")}
	lc, _, _ := newLifecycleWithDriver(drv)

	_, _, err := lc.EnsureDriver(context.Background(), Device{UDID: "AAA"}, sampleTarget(true))
	if err == nil || !strings.Contains(err.Error(), "external driver health") {
		t.Fatalf("expected external driver health error, got %v", err)
	}
}

func TestEnsureDriverEmbeddedModeRequiresManager(t *testing.T) {
	t.Parallel()

	drv := &fakeDriver{}
	lc, _, xc := newLifecycleWithDriver(drv)

	_, _, err := lc.EnsureDriver(context.Background(), Device{UDID: "AAA"}, sampleTarget(false))
	if err == nil || !strings.Contains(err.Error(), "embedded driver manager") {
		t.Fatalf("expected embedded-manager error, got %v", err)
	}

	if got := xc.calls.Load(); got != 0 {
		t.Fatalf("expected no xcodebuild call when manager is missing, got %d", got)
	}
}

type fakeEmbedded struct {
	prepareCalls atomic.Int32
	invalidates  atomic.Int32
	prepared     embeddeddriver.Prepared
	prepareErr   error
}

func (f *fakeEmbedded) Prepare(_ context.Context, _, _ string) (embeddeddriver.Prepared, error) {
	f.prepareCalls.Add(1)

	if f.prepareErr != nil {
		return embeddeddriver.Prepared{}, f.prepareErr
	}

	return f.prepared, nil
}

func (f *fakeEmbedded) InvalidateBuild(_ string) error {
	f.invalidates.Add(1)

	return nil
}

func TestEnsureDriverEmbeddedModeStartsTestWithoutBuilding(t *testing.T) {
	t.Parallel()

	drv := &fakeDriver{}
	lc, _, xc := newLifecycleWithDriver(drv)

	em := &fakeEmbedded{prepared: embeddeddriver.Prepared{
		XCTestRunPath: "/cache/key123/derived-data/Build/Products/Driver.xctestrun",
		CacheKey:      "key123",
	}}
	lc.Embedded = em

	_, handle, err := lc.EnsureDriver(context.Background(), Device{UDID: "AAA", Runtime: "iOS-18-0"}, sampleTarget(false))
	if err != nil {
		t.Fatalf("ensure driver: %v", err)
	}

	if handle == nil {
		t.Fatal("expected non-nil handle in embedded mode")
	}

	if got := em.prepareCalls.Load(); got != 1 {
		t.Fatalf("expected one Prepare call, got %d", got)
	}

	if got := xc.calls.Load(); got != 1 {
		t.Fatalf("expected one xcodebuild call, got %d", got)
	}

	if xc.opts.XCTestRunPath != em.prepared.XCTestRunPath {
		t.Fatalf("expected xctestrun path to flow into xcodebuild opts, got %q", xc.opts.XCTestRunPath)
	}

	if !strings.Contains(xc.opts.Destination, "AAA") {
		t.Fatalf("expected destination to include UDID, got %q", xc.opts.Destination)
	}

	if !strings.Contains(xc.opts.LogPath, "build/artifacts/mobile/driver/iphone/driver.log") {
		t.Fatalf("expected driver log path in options, got %+v", xc.opts)
	}

	if xc.opts.HealthURL != "http://127.0.0.1:9080/health" {
		t.Fatalf("expected health URL in options, got %+v", xc.opts)
	}

	if xc.opts.Env["TALES_DRIVER_HOST"] != "127.0.0.1" || xc.opts.Env["TALES_DRIVER_PORT"] != "9080" {
		t.Fatalf("expected driver host/port env, got %+v", xc.opts.Env)
	}
}

func TestEnsureDriverEmbeddedModeRetriesOnHealthFailure(t *testing.T) {
	t.Parallel()

	drv := &fakeDriver{}
	lc, _, xc := newLifecycleWithDriver(drv)
	// Fail the first Start, succeed on the retry.
	xc.failOnce = errors.New("driver did not become healthy: timeout")

	em := &fakeEmbedded{prepared: embeddeddriver.Prepared{
		XCTestRunPath: "/cache/key123/Build/Products/Driver.xctestrun",
		CacheKey:      "key123",
	}}
	lc.Embedded = em

	_, handle, err := lc.EnsureDriver(context.Background(), Device{UDID: "AAA", Runtime: "iOS-18-0"}, sampleTarget(false))
	if err != nil {
		t.Fatalf("ensure driver: %v", err)
	}

	if handle == nil {
		t.Fatal("expected non-nil handle after retry")
	}

	if got := xc.calls.Load(); got != 2 {
		t.Fatalf("expected 2 xcodebuild calls (initial + retry), got %d", got)
	}

	if got := em.invalidates.Load(); got != 1 {
		t.Fatalf("expected one cache invalidation, got %d", got)
	}

	if got := em.prepareCalls.Load(); got != 2 {
		t.Fatalf("expected 2 Prepare calls (initial + rebuild), got %d", got)
	}
}
