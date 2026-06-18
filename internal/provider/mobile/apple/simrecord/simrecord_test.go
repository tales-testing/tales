package simrecord

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSpawn struct {
	name string
	args []string
	env  map[string]string
}

type fakeSpawner struct {
	calls   []fakeSpawn
	process *fakeProcess
	err     error
}

func (f *fakeSpawner) Spawn(_ context.Context, name string, args []string, env map[string]string) (Process, error) {
	envCopy := make(map[string]string, len(env))
	maps.Copy(envCopy, env)

	f.calls = append(f.calls, fakeSpawn{name: name, args: append([]string(nil), args...), env: envCopy})

	if f.err != nil {
		return nil, f.err
	}

	if f.process == nil {
		f.process = &fakeProcess{}
	}

	return f.process, nil
}

type fakeProcess struct {
	stopCount atomic.Int32
	stopErr   error
	// stopDelay simulates a process that takes a while to exit, used to
	// exercise the Stop timeout path.
	stopDelay time.Duration
}

func (p *fakeProcess) Stop(ctx context.Context) error {
	p.stopCount.Add(1)

	if p.stopDelay > 0 {
		select {
		case <-time.After(p.stopDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return p.stopErr
}

func TestBuildArgsEmitsFullCommand(t *testing.T) {
	t.Parallel()

	got := BuildArgs(Options{
		UDID:    "ABC-123",
		Output:  "/tmp/preview.mp4",
		Codec:   CodecH264,
		Mask:    MaskBlack,
		Display: DisplayInternal,
		Force:   true,
	})

	want := []string{
		"simctl", "io", "ABC-123", "recordVideo",
		"--codec", "h264",
		"--mask", "black",
		"--display", "internal",
		"--force",
		"/tmp/preview.mp4",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestBuildArgsOmitsUnsetFlags(t *testing.T) {
	t.Parallel()

	got := BuildArgs(Options{UDID: "X", Output: "/tmp/x.mp4"})

	want := []string{"simctl", "io", "X", "recordVideo", "/tmp/x.mp4"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestStartInvokesSpawnerWithXcrun(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	rec := New(spawner)

	err := rec.Start(context.Background(), Options{
		UDID:   "X",
		Output: "/tmp/x.mp4",
		Codec:  CodecH264,
		Force:  true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if len(spawner.calls) != 1 || spawner.calls[0].name != "xcrun" {
		t.Fatalf("expected xcrun call, got %+v", spawner.calls)
	}

	want := []string{"simctl", "io", "X", "recordVideo", "--codec", "h264", "--force", "/tmp/x.mp4"}
	if !reflect.DeepEqual(spawner.calls[0].args, want) {
		t.Fatalf("args mismatch\nwant: %v\n got: %v", want, spawner.calls[0].args)
	}
}

func TestStartRejectsConcurrentRecording(t *testing.T) {
	t.Parallel()

	rec := New(&fakeSpawner{})

	if err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/a.mp4"}); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/b.mp4"})
	if err == nil {
		t.Fatal("expected second Start to fail")
	}
}

func TestStartValidatesOptions(t *testing.T) {
	t.Parallel()

	rec := New(&fakeSpawner{})

	if err := rec.Start(context.Background(), Options{Output: "/tmp/x.mp4"}); err == nil {
		t.Fatal("expected missing UDID to fail")
	}

	if err := rec.Start(context.Background(), Options{UDID: "X"}); err == nil {
		t.Fatal("expected missing output to fail")
	}
}

func TestStopStopsProcessAndReturnsPath(t *testing.T) {
	t.Parallel()

	process := &fakeProcess{}
	rec := New(&fakeSpawner{process: process})

	if err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/x.mp4"}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got != "/tmp/x.mp4" {
		t.Fatalf("expected output path /tmp/x.mp4, got %q", got)
	}

	if process.stopCount.Load() != 1 {
		t.Fatalf("expected process.Stop to be called once, got %d", process.stopCount.Load())
	}
}

func TestStopWithoutStartIsNoop(t *testing.T) {
	t.Parallel()

	rec := New(&fakeSpawner{})

	got, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got != "" {
		t.Fatalf("expected empty path, got %q", got)
	}
}

func TestStopReturnsPathEvenWhenProcessTimesOut(t *testing.T) {
	t.Parallel()

	process := &fakeProcess{stopDelay: 200 * time.Millisecond}
	rec := New(&fakeSpawner{process: process})

	if err := rec.Start(context.Background(), Options{
		UDID:        "X",
		Output:      "/tmp/timeout.mp4",
		StopTimeout: 10 * time.Millisecond,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got, err := rec.Stop(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if got != "/tmp/timeout.mp4" {
		t.Fatalf("expected output path even on timeout, got %q", got)
	}
}

func TestStartPropagatesSpawnError(t *testing.T) {
	t.Parallel()

	rec := New(&fakeSpawner{err: errors.New("no xcrun")})

	err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/x.mp4"})
	if err == nil {
		t.Fatal("expected spawn error to propagate")
	}
}

func TestStartAcceptsRestartAfterStop(t *testing.T) {
	t.Parallel()

	rec := New(&fakeSpawner{})

	if err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/a.mp4"}); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if _, err := rec.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := rec.Start(context.Background(), Options{UDID: "X", Output: "/tmp/b.mp4"}); err != nil {
		t.Fatalf("second Start should succeed after Stop: %v", err)
	}
}
