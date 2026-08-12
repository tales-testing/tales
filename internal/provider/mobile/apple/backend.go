package apple

import (
	"context"
	"fmt"
	"time"

	appledriver "github.com/tales-testing/tales/drivers/apple"
	"github.com/tales-testing/tales/internal/provider/mobile"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/embeddeddriver"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/simctl"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/simrecord"
	"github.com/tales-testing/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// Options returns the provider options registering the iOS backend with
// real Apple tooling: ExecRunner for simctl, ExecSpawner for xcodebuild,
// and the HTTP driver client.
//
//	mobile.New(append(Options(), mobile.WithCaptureMode(mode))...)
//
// Tests bypass this and register a fake with mobile.WithSessionBuilder.
func Options() []mobile.Option {
	return []mobile.Option{
		mobile.WithBackend(mobile.PlatformIOS, SessionBuilder()),
		mobile.WithRecorderFactory(RecorderFactory()),
	}
}

// SessionBuilder returns the production iOS session builder.
func SessionBuilder() mobile.SessionBuilder {
	runner := ExecRunner{}
	tool := simctlAdapter{tool: simctl.New(runner)}
	launcher := xcodebuild.New(xcodebuild.ExecSpawner{})
	factory := func(cfg mobile.DriverConfig) driver.Driver {
		return driver.New(cfg.BaseURL(), driver.WithTimeout(cfg.Timeout))
	}

	lifecycle := &Lifecycle{
		Simctl:     tool,
		Xcodebuild: launcher,
		NewDriver:  factory,
		Embedded:   newEmbeddedManager(),
	}

	return mobile.SessionBuilderFunc(func(ctx context.Context, target mobile.Target) (*mobile.Session, error) {
		device, err := lifecycle.EnsureBooted(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("ensure booted: %w", err)
		}

		// Resolve the driver port before starting the driver so the Go
		// client, the TALES_DRIVER_PORT env, and the health URL all agree.
		// In embedded mode with no explicit port this picks a free host port
		// so multiple simulators do not collide on the shared loopback.
		resolved, err := mobile.ResolveDriverEndpoint(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("resolve driver endpoint: %w", err)
		}

		drv, handle, diagnostics, err := lifecycle.EnsureDriver(ctx, device, resolved)
		if err != nil {
			return nil, fmt.Errorf("ensure driver: %w", err)
		}

		return &mobile.Session{
			Target:       resolved,
			DeviceID:     device.UDID,
			Driver:       drv,
			DriverHandle: handle,
			Lifecycle:    lifecycle,
			Diagnostics:  diagnostics,
		}, nil
	})
}

// RecorderFactory returns the production simctl-backed recorder factory.
func RecorderFactory() mobile.RecorderFactory {
	return func(deviceID string) mobile.Recorder {
		return &recorder{udid: deviceID, inner: simrecord.New(simrecord.ExecSpawner{})}
	}
}

// recorder adapts simrecord.Recorder to mobile.Recorder, translating the
// neutral RecordOptions into simctl's own flags and rejecting the options
// that only exist on another platform.
type recorder struct {
	udid  string
	inner *simrecord.Recorder
}

func (r *recorder) Start(ctx context.Context, opts mobile.RecordOptions) error {
	if opts.BitRate != "" {
		return fmt.Errorf(`record: "bit_rate" is not supported on ios (it is an Android screenrecord option)`)
	}

	if opts.Size != "" {
		return fmt.Errorf(`record: "size" is not supported on ios (it is an Android screenrecord option)`)
	}

	err := r.inner.Start(ctx, simrecord.Options{
		UDID:    r.udid,
		Output:  opts.Output,
		Codec:   opts.Codec,
		Mask:    opts.Mask,
		Display: opts.Display,
		Force:   opts.Force,
	})
	if err != nil {
		return fmt.Errorf("simctl recordVideo: %w", err)
	}

	return nil
}

func (r *recorder) Stop(ctx context.Context) (string, error) {
	path, err := r.inner.Stop(ctx)
	if err != nil {
		return path, fmt.Errorf("simctl recordVideo stop: %w", err)
	}

	return path, nil
}

// newEmbeddedManager constructs the production embeddeddriver.Manager.
// Source is taken from the appledriver embed.FS; CacheBase resolves to
// the per-user cache (overridable via TALES_DRIVER_CACHE_DIR). If
// cache-base resolution fails (no HOME, sandboxed env, etc.), a
// brokenManager is returned that surfaces the real cause on every call
// so users get an actionable error instead of "rebuild Tales".
func newEmbeddedManager() EmbeddedDriverManager {
	base, err := embeddeddriver.ResolveBase()
	if err != nil {
		return brokenManager{cause: err}
	}

	return &embeddeddriver.Manager{
		Source:     appledriver.FS(),
		SourceRoot: appledriver.SourceRoot,
		CacheBase:  base,
		Builder:    &embeddeddriver.XcodebuildBuilder{Runner: embeddeddriver.ExecBuildRunner{}},
		Runner:     execCommandRunner{},
	}
}

// brokenManager satisfies EmbeddedDriverManager but returns the
// init-time cause from every operation. It is wired in when the cache
// base cannot be resolved, so embedded-mode targets fail with the real
// underlying error (e.g. "cannot resolve user cache dir: $HOME is
// undefined") instead of a generic "embedded driver not configured".
type brokenManager struct {
	cause error
}

func (b brokenManager) Prepare(_ context.Context, _, _ string) (embeddeddriver.Prepared, error) {
	return embeddeddriver.Prepared{}, fmt.Errorf("embedded driver cache is unavailable: %w (try setting TALES_DRIVER_CACHE_DIR to a writable directory)", b.cause)
}

func (b brokenManager) InvalidateBuild(_ string) error {
	return fmt.Errorf("embedded driver cache is unavailable: %w", b.cause)
}

// execCommandRunner adapts os/exec to embeddeddriver.CommandRunner so
// xcode introspection (xcodebuild -version, xcrun --show-sdk-version,
// xcode-select -p, sw_vers) can feed the cache key in production.
type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := ExecRunner{}.Run(ctx, name, args...)
	if err != nil {
		return out, fmt.Errorf("run %s: %w", name, err)
	}

	return out, nil
}

// simctlAdapter narrows the concrete simctl.Tool API into the smaller
// SimctlTool interface used by Lifecycle.
type simctlAdapter struct {
	tool *simctl.Tool
}

func (s simctlAdapter) FindDeviceByName(ctx context.Context, name string) (Device, error) {
	device, err := s.tool.FindDeviceByName(ctx, name)
	if err != nil {
		return Device{}, fmt.Errorf("simctl find device: %w", err)
	}

	return Device{UDID: device.UDID, Name: device.Name, Runtime: device.Runtime, Booted: device.Booted()}, nil
}

func (s simctlAdapter) Boot(ctx context.Context, udid string) error {
	if err := s.tool.Boot(ctx, udid); err != nil {
		return fmt.Errorf("simctl boot: %w", err)
	}

	return nil
}

func (s simctlAdapter) WaitBooted(ctx context.Context, udid string, timeout time.Duration) error {
	if err := s.tool.WaitBooted(ctx, udid, timeout); err != nil {
		return fmt.Errorf("simctl bootstatus: %w", err)
	}

	return nil
}

func (s simctlAdapter) Install(ctx context.Context, udid, appPath string) error {
	if err := s.tool.Install(ctx, udid, appPath); err != nil {
		return fmt.Errorf("simctl install: %w", err)
	}

	return nil
}

func (s simctlAdapter) Uninstall(ctx context.Context, udid, bundleID string) error {
	if err := s.tool.Uninstall(ctx, udid, bundleID); err != nil {
		return fmt.Errorf("simctl uninstall: %w", err)
	}

	return nil
}

func (s simctlAdapter) Launch(ctx context.Context, udid, bundleID string) error {
	if err := s.tool.Launch(ctx, udid, bundleID); err != nil {
		return fmt.Errorf("simctl launch: %w", err)
	}

	return nil
}

func (s simctlAdapter) Terminate(ctx context.Context, udid, bundleID string) error {
	if err := s.tool.Terminate(ctx, udid, bundleID); err != nil {
		return fmt.Errorf("simctl terminate: %w", err)
	}

	return nil
}

func (s simctlAdapter) ResetKeychain(ctx context.Context, udid string) error {
	if err := s.tool.ResetKeychain(ctx, udid); err != nil {
		return fmt.Errorf("simctl reset keychain: %w", err)
	}

	return nil
}

func (s simctlAdapter) Privacy(ctx context.Context, udid, action, service, bundleID string) error {
	if err := s.tool.Privacy(ctx, udid, action, service, bundleID); err != nil {
		return fmt.Errorf("simctl privacy: %w", err)
	}

	return nil
}

func (s simctlAdapter) Screenshot(ctx context.Context, udid, path string) error {
	if err := s.tool.Screenshot(ctx, udid, path); err != nil {
		return fmt.Errorf("simctl screenshot: %w", err)
	}

	return nil
}
