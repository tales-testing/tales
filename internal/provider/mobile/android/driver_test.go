package android

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/provider/mobile"
	"github.com/tales-testing/tales/internal/provider/mobile/android/adb"
	"github.com/tales-testing/tales/internal/provider/mobile/android/instrument"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// startADB answers every adb call EnsureDriver makes with success; the
// stubs are logcatADB's.
type startADB struct {
	logcatADB
}

// nopInstrumentSpawner stands in for `adb shell am instrument`: the real
// launcher runs its health wait against the driver, but nothing is
// spawned.
type nopInstrumentSpawner struct{}

func (nopInstrumentSpawner) Spawn(context.Context, string, []string, string) (instrument.Process, error) {
	return nopInstrumentProcess{}, nil
}

type nopInstrumentProcess struct{}

func (nopInstrumentProcess) Stop(context.Context) error { return nil }

type fixedArtifacts struct{}

func (fixedArtifacts) Prepare(context.Context) (Prepared, error) {
	return Prepared{AppAPKPath: "/cache/driver.apk", TestAPKPath: "/cache/driver-test.apk"}, nil
}

// deadlineDriver records how much of the health wait is left when it is
// first polled, which is the startup budget the launcher actually applied.
type deadlineDriver struct {
	driver.NoopDriver

	budget atomic.Int64
}

func (d *deadlineDriver) Health(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		d.budget.CompareAndSwap(0, int64(time.Until(deadline)))
	}

	return nil
}

// driver.startup_timeout must reach the health wait after the
// instrumentation is started. Issue #74: the wait was a fixed 60s, and
// the one knob that looked like it applied, driver.timeout, only bounds
// single requests, so a cold emulator on a shared CI host failed the
// first scenario with no setting able to prevent it.
func TestEnsureDriverBoundsTheHealthWaitByStartupTimeout(t *testing.T) {
	t.Parallel()

	drv := &deadlineDriver{}
	lifecycle := &Lifecycle{
		ADB:        &startADB{},
		Instrument: instrument.New(nopInstrumentSpawner{}),
		Artifacts:  fixedArtifacts{},
		NewDriver:  func(_ mobile.DriverConfig) driver.Driver { return drv },
	}

	target := mobile.Target{
		Name:     "phone",
		Platform: mobile.PlatformAndroid,
		AppID:    "com.example.app",
		Driver: mobile.DriverConfig{
			Host:           mobile.DefaultDriverHost,
			Port:           60928,
			Timeout:        90 * time.Second,
			StartupTimeout: 3 * time.Minute,
		},
	}

	_, handle, _, err := lifecycle.EnsureDriver(context.Background(), adb.Device{Serial: "emulator-5554"}, target)
	if err != nil {
		t.Fatalf("ensure driver: %v", err)
	}

	t.Cleanup(func() { _ = handle.Stop(context.Background()) })

	// The first poll happens right away, so what is left of the budget
	// is the budget itself, give or take scheduling.
	got := time.Duration(drv.budget.Load())
	if got <= 2*time.Minute || got > 3*time.Minute {
		t.Fatalf("health wait budget = %v, want about 3m0s (driver.startup_timeout); the %v default was applied instead",
			got.Round(time.Second), instrument.DefaultHealthTimeout)
	}
}
