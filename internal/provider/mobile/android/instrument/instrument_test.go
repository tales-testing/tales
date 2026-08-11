package instrument

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProcess struct {
	stops atomic.Int32
}

func (p *fakeProcess) Stop(_ context.Context) error {
	p.stops.Add(1)

	return nil
}

type fakeSpawner struct {
	name    string
	args    []string
	logPath string
	process *fakeProcess
	err     error
}

func (f *fakeSpawner) Spawn(_ context.Context, name string, args []string, logPath string) (Process, error) {
	f.name = name
	f.args = append([]string(nil), args...)
	f.logPath = logPath

	if f.err != nil {
		return nil, f.err
	}

	if f.process == nil {
		f.process = &fakeProcess{}
	}

	return f.process, nil
}

// fakePinger reports unhealthy for the first failures calls, then
// healthy, which models a driver that takes a moment to bind.
type fakePinger struct {
	failures int
	calls    atomic.Int32
	err      error
}

func (p *fakePinger) Health(_ context.Context) error {
	n := int(p.calls.Add(1))

	if n <= p.failures {
		if p.err != nil {
			return p.err
		}

		return errors.New("connection refused")
	}

	return nil
}

func sampleOptions() Options {
	return Options{
		ADBPath:       "/sdk/platform-tools/adb",
		Serial:        "emulator-5554",
		TestPackage:   "org.taleslabs.tales.driver.test",
		DevicePort:    9080,
		LogPath:       "/tmp/driver.log",
		HealthTimeout: 200 * time.Millisecond,
		PollInterval:  10 * time.Millisecond,
	}
}

func TestBuildArgsRendersTheInstrumentationCommand(t *testing.T) {
	t.Parallel()

	args := strings.Join(BuildArgs(sampleOptions()), " ")

	want := "-s emulator-5554 shell am instrument -w -r " +
		"-e class org.taleslabs.tales.driver.TalesDriverTest#runServer " +
		"-e port 9080 " +
		"org.taleslabs.tales.driver.test/androidx.test.runner.AndroidJUnitRunner"

	if args != want {
		t.Fatalf("args =\n  %s\nwant\n  %s", args, want)
	}
}

func TestStartWaitsForTheDriverToAnswer(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	// Two refusals then success: the driver process exists well before
	// its socket is listening, so Start must poll rather than assume.
	pinger := &fakePinger{failures: 2}

	handle, err := New(spawner).Start(context.Background(), sampleOptions(), pinger)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if handle == nil {
		t.Fatal("expected a handle")
	}

	if pinger.calls.Load() < 3 {
		t.Fatalf("expected the health check to be retried, got %d calls", pinger.calls.Load())
	}

	if spawner.logPath != "/tmp/driver.log" {
		t.Fatalf("log path = %q", spawner.logPath)
	}
}

func TestStartStopsTheProcessWhenTheDriverNeverAnswers(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	pinger := &fakePinger{failures: 1_000_000}

	_, err := New(spawner).Start(context.Background(), sampleOptions(), pinger)
	if err == nil {
		t.Fatal("expected an error when the driver never becomes healthy")
	}

	// Leaving the instrumentation running would keep the device port
	// held, so the next attempt would fail for a different reason.
	if got := spawner.process.stops.Load(); got != 1 {
		t.Fatalf("expected the process to be stopped once, got %d", got)
	}
}

func TestStartErrorNamesTheDriverLog(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	pinger := &fakePinger{failures: 1_000_000}

	_, err := New(spawner).Start(context.Background(), sampleOptions(), pinger)
	if err == nil {
		t.Fatal("expected an error")
	}

	// The log holds the instrumentation's own stack trace, which is the
	// only place a bind failure or a crashed runner is visible.
	var startErr *StartError
	if !errors.As(err, &startErr) {
		t.Fatalf("expected a *StartError, got %T", err)
	}

	if startErr.DriverLogPath() != "/tmp/driver.log" {
		t.Fatalf("DriverLogPath() = %q", startErr.DriverLogPath())
	}

	if !strings.Contains(err.Error(), "/tmp/driver.log") {
		t.Fatalf("error should quote the log path, got: %v", err)
	}
}

func TestStartSurfacesTheLastHealthError(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	pinger := &fakePinger{failures: 1_000_000, err: errors.New("dial tcp 127.0.0.1:9080: connect: connection refused")}

	_, err := New(spawner).Start(context.Background(), sampleOptions(), pinger)
	if err == nil {
		t.Fatal("expected an error")
	}

	// "did not become healthy" alone does not say why; the transport
	// error is what distinguishes a crashed driver from a wrong port.
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error should carry the last health failure, got: %v", err)
	}
}

func TestStartReportsSpawnFailures(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{err: errors.New("adb: device offline")}

	_, err := New(spawner).Start(context.Background(), sampleOptions(), &fakePinger{})
	if err == nil {
		t.Fatal("expected an error when the spawn fails")
	}

	if !strings.Contains(err.Error(), "device offline") {
		t.Fatalf("error should carry the spawn failure, got: %v", err)
	}
}

func TestHandleStopIsSafeOnAZeroValue(t *testing.T) {
	t.Parallel()

	var handle *Handle

	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("stopping a nil handle should be a no-op, got %v", err)
	}
}
