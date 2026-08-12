package android

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/tales-testing/tales/internal/provider/mobile"
	"github.com/tales-testing/tales/internal/provider/mobile/android/adb"
	"github.com/tales-testing/tales/internal/provider/mobile/android/instrument"
	"github.com/tales-testing/tales/internal/provider/mobile/android/screenrecord"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// Options returns the provider options registering the Android backend.
//
//	mobile.New(append(android.Options(), mobile.WithCaptureMode(mode))...)
//
// adb is located lazily, on the first Android step, so a binary running
// only iOS scenarios never has to have the Android SDK installed.
func Options() []mobile.Option {
	return []mobile.Option{
		mobile.WithBackend(mobile.PlatformAndroid, SessionBuilder()),
	}
}

// SessionBuilder returns the production Android session builder.
func SessionBuilder() mobile.SessionBuilder {
	return mobile.SessionBuilderFunc(func(ctx context.Context, target mobile.Target) (*mobile.Session, error) {
		binary, err := Locate(target)
		if err != nil {
			return nil, err
		}

		runner := ExecRunner{}
		tool := adb.New(binary, runner)

		lifecycle := &Lifecycle{
			ADB:        tool,
			Instrument: instrument.New(instrument.ExecSpawner{}),
			Artifacts:  newArtifacts(),
			NewDriver: func(cfg mobile.DriverConfig) driver.Driver {
				return driver.New(cfg.BaseURL(), driver.WithTimeout(cfg.Timeout))
			},
		}

		device, err := lifecycle.SelectDevice(ctx, target)
		if err != nil {
			return nil, err
		}

		lifecycle.serial = device.Serial

		// The API level gates the permission mapping and screenrecord's
		// duration flag, so read it once per session rather than on
		// every step that needs it.
		if level, err := tool.APILevel(ctx, device.Serial); err == nil {
			lifecycle.apiLevel = level
		}

		// Allocate the host side of the forward before starting the
		// driver so the client, the forward and the health URL all agree
		// on one port. The device side is fixed: its port namespace is
		// per-device, so emulators cannot collide there.
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
			DeviceID:     device.Serial,
			Driver:       drv,
			DriverHandle: handle,
			Lifecycle:    lifecycle,
			Diagnostics:  diagnostics,
		}, nil
	})
}

// Locate resolves the adb binary for a target.
func Locate(target mobile.Target) (string, error) {
	binary, err := adb.Locate(target.Driver.ADBPath)
	if err != nil {
		return "", fmt.Errorf("target %q: %w", target.Name, err)
	}

	return binary, nil
}

// RecorderFactory returns the production screenrecord-backed factory.
//
// It needs a located adb, so it is built per session rather than once at
// registration: a binary with no Android SDK must still be able to run
// iOS scenarios that declare a record block.
func RecorderFactory(binary string, tool *adb.Tool, apiLevel int) mobile.RecorderFactory {
	return func(deviceID string) mobile.Recorder {
		return &recorder{
			binary:   binary,
			serial:   deviceID,
			apiLevel: apiLevel,
			inner:    screenrecord.New(screenrecord.ExecSpawner{}, tool),
		}
	}
}

// recorder adapts screenrecord.Recorder to mobile.Recorder, translating
// the neutral options and rejecting the ones that only exist on iOS.
type recorder struct {
	binary   string
	serial   string
	apiLevel int
	inner    *screenrecord.Recorder
}

func (r *recorder) Start(ctx context.Context, opts mobile.RecordOptions) error {
	for name, value := range map[string]string{"codec": opts.Codec, "mask": opts.Mask, "display": opts.Display} {
		if value != "" {
			return fmt.Errorf("record: %q is not supported on android (it is a simctl recordVideo option)", name)
		}
	}

	err := r.inner.Start(ctx, screenrecord.Options{
		ADBPath:  r.binary,
		Serial:   r.serial,
		Output:   opts.Output,
		BitRate:  opts.BitRate,
		Size:     opts.Size,
		APILevel: r.apiLevel,
	})
	if err != nil {
		return fmt.Errorf("screenrecord: %w", err)
	}

	return nil
}

func (r *recorder) Stop(ctx context.Context) (string, error) {
	path, err := r.inner.Stop(ctx)
	if err != nil {
		return path, fmt.Errorf("screenrecord stop: %w", err)
	}

	return path, nil
}

// ExecRunner runs adb through os/exec.
type ExecRunner struct{}

// Run executes name with args and returns its combined output. The
// combined stream matters: adb reports most refusals on stdout with a
// non-zero exit, so dropping either half would lose the reason.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}

	return out, nil
}
