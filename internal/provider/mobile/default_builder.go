package mobile

import (
	"context"
	"fmt"
	"os"
	"time"

	appledriver "github.com/hyperxlab/tales/drivers/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/embeddeddriver"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/simctl"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/xcodebuild"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
)

// NewApple returns a Provider wired with real Apple tooling: ExecRunner for
// simctl, ExecSpawner for xcodebuild, and the HTTP driver client. Tests
// should call New(WithSessionBuilder(fake)) instead.
func NewApple(opts ...Option) *Provider {
	builder := appleSessionBuilder()
	all := append([]Option{WithSessionBuilder(builder)}, opts...)

	return New(all...)
}

func appleSessionBuilder() SessionBuilder {
	runner := apple.ExecRunner{}
	tool := simctlAdapter{tool: simctl.New(runner)}
	launcher := xcodebuild.New(xcodebuild.ExecSpawner{})
	factory := func(baseURL string) driver.Driver {
		return driver.New(baseURL)
	}

	lifecycle := &apple.Lifecycle{
		Simctl:     tool,
		Xcodebuild: launcher,
		NewDriver:  factory,
		Embedded:   newEmbeddedManager(),
	}

	return SessionBuilderFunc(func(ctx context.Context, target apple.Target) (*Session, error) {
		device, err := lifecycle.EnsureBooted(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("ensure booted: %w", err)
		}

		drv, handle, err := lifecycle.EnsureDriver(ctx, device, target)
		if err != nil {
			return nil, fmt.Errorf("ensure driver: %w", err)
		}

		return &Session{
			Target:       target,
			UDID:         device.UDID,
			Driver:       drv,
			DriverHandle: handle,
			Lifecycle:    lifecycle,
		}, nil
	})
}

// newEmbeddedManager constructs the production embeddeddriver.Manager.
// Source is taken from the appledriver embed.FS; CacheBase resolves to
// the per-user cache (overridable via TALES_DRIVER_CACHE_DIR). If the
// cache base cannot be resolved (a no-HOME environment), the lifecycle
// runs without an embedded manager — embedded mode then surfaces a
// clean error instead of crashing.
func newEmbeddedManager() apple.EmbeddedDriverManager {
	base, err := embeddeddriver.ResolveBase()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tales: embedded driver disabled (cache resolution failed: %v)\n", err)

		return nil
	}

	return &embeddeddriver.Manager{
		Source:     appledriver.FS(),
		SourceRoot: appledriver.SourceRoot,
		CacheBase:  base,
		Builder:    &embeddeddriver.XcodebuildBuilder{Runner: embeddeddriver.ExecBuildRunner{}},
		Runner:     execCommandRunner{},
	}
}

// execCommandRunner adapts os/exec to embeddeddriver.CommandRunner so
// xcode introspection (xcodebuild -version, xcrun --show-sdk-version,
// xcode-select -p, sw_vers) can feed the cache key in production.
type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := apple.ExecRunner{}.Run(ctx, name, args...)
	if err != nil {
		return out, fmt.Errorf("run %s: %w", name, err)
	}

	return out, nil
}

// simctlAdapter narrows the concrete simctl.Tool API into the smaller
// apple.SimctlTool interface used by apple.Lifecycle.
type simctlAdapter struct {
	tool *simctl.Tool
}

func (s simctlAdapter) FindDeviceByName(ctx context.Context, name string) (apple.Device, error) {
	device, err := s.tool.FindDeviceByName(ctx, name)
	if err != nil {
		return apple.Device{}, fmt.Errorf("simctl find device: %w", err)
	}

	return apple.Device{UDID: device.UDID, Name: device.Name, Runtime: device.Runtime, Booted: device.Booted()}, nil
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

func (s simctlAdapter) Screenshot(ctx context.Context, udid, path string) error {
	if err := s.tool.Screenshot(ctx, udid, path); err != nil {
		return fmt.Errorf("simctl screenshot: %w", err)
	}

	return nil
}
