package xcodebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSpawn struct {
	name    string
	args    []string
	logPath string
	env     map[string]string
}

type fakeSpawner struct {
	calls   []fakeSpawn
	process *fakeProcess
	err     error
}

func (f *fakeSpawner) Spawn(_ context.Context, name string, args []string, logPath string, env map[string]string) (Process, error) {
	envCopy := make(map[string]string, len(env))
	for key, value := range env {
		envCopy[key] = value
	}

	f.calls = append(f.calls, fakeSpawn{name: name, args: append([]string(nil), args...), logPath: logPath, env: envCopy})

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
}

func (p *fakeProcess) Stop(_ context.Context) error {
	p.stopCount.Add(1)

	return nil
}

type fakePinger struct {
	calls atomic.Int32
	until int32
	err   error
}

func (p *fakePinger) Health(_ context.Context) error {
	count := p.calls.Add(1)
	if count >= p.until {
		return p.err
	}

	return errors.New("not ready")
}

const sampleXCTestRunPath = "/cache/key123/derived-data/Build/Products/Driver.xctestrun"

func sampleOptions(overrides ...func(*Options)) Options {
	opts := Options{
		XCTestRunPath: sampleXCTestRunPath,
		Destination:   "platform=iOS Simulator,id=ABC",
		HealthTimeout: time.Second,
		PollInterval:  time.Millisecond,
	}

	for _, override := range overrides {
		override(&opts)
	}

	return opts
}

func TestBuildArgsEmitsTestWithoutBuilding(t *testing.T) {
	t.Parallel()

	got := BuildArgs(Options{
		XCTestRunPath: sampleXCTestRunPath,
		Destination:   "platform=iOS Simulator,id=ABC",
	})

	want := []string{
		"test-without-building",
		"-xctestrun", sampleXCTestRunPath,
		"-destination", "platform=iOS Simulator,id=ABC",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestStartReturnsHandleWhenHealthy(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	pinger := &fakePinger{until: 2} // succeed on the second call

	launcher := New(spawner)

	handle, err := launcher.Start(context.Background(), sampleOptions(func(o *Options) {
		o.HealthTimeout = 2 * time.Second
		o.PollInterval = 5 * time.Millisecond
	}), pinger)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if handle == nil {
		t.Fatal("expected non-nil handle")
	}

	if len(spawner.calls) != 1 || spawner.calls[0].name != "xcodebuild" {
		t.Fatalf("expected xcodebuild call, got %+v", spawner.calls)
	}

	if got := pinger.calls.Load(); got < 2 {
		t.Fatalf("expected at least 2 health calls, got %d", got)
	}
}

func TestStartPassesLogPathToSpawner(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	launcher := New(spawner)

	_, err := launcher.Start(context.Background(), sampleOptions(func(o *Options) {
		o.LogPath = "build/artifacts/mobile/driver/iphone/driver.log"
	}), &fakePinger{until: 1})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if got := spawner.calls[0].logPath; got != "build/artifacts/mobile/driver/iphone/driver.log" {
		t.Fatalf("log path=%q", got)
	}
}

func TestStartPassesEnvironmentToSpawner(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	launcher := New(spawner)

	_, err := launcher.Start(context.Background(), sampleOptions(func(o *Options) {
		o.Env = map[string]string{
			"TALES_DRIVER_HOST": "127.0.0.1",
			"TALES_DRIVER_PORT": "9090",
		}
	}), &fakePinger{until: 1})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if got := spawner.calls[0].env["TALES_DRIVER_PORT"]; got != "9090" {
		t.Fatalf("driver port env=%q", got)
	}
}

func TestStartStopsProcessOnHealthTimeout(t *testing.T) {
	t.Parallel()

	process := &fakeProcess{}
	spawner := &fakeSpawner{process: process}
	pinger := &fakePinger{until: 999, err: errors.New("never ready")}

	launcher := New(spawner)

	_, err := launcher.Start(context.Background(), sampleOptions(func(o *Options) {
		o.HealthTimeout = 30 * time.Millisecond
		o.PollInterval = 5 * time.Millisecond
	}), pinger)
	if err == nil {
		t.Fatal("expected error on health timeout")
	}

	if !strings.Contains(err.Error(), "did not become healthy") {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := process.stopCount.Load(); got != 1 {
		t.Fatalf("expected process to be stopped once, got %d", got)
	}
}

func TestStartTimeoutIncludesLogPathAndDoctorHint(t *testing.T) {
	t.Parallel()

	launcher := New(&fakeSpawner{process: &fakeProcess{}})

	_, err := launcher.Start(context.Background(), sampleOptions(func(o *Options) {
		o.HealthURL = "http://127.0.0.1:9080/health"
		o.LogPath = "build/artifacts/mobile/driver/iphone/driver.log"
		o.HealthTimeout = 30 * time.Millisecond
		o.PollInterval = 5 * time.Millisecond
	}), &fakePinger{until: 999, err: errors.New("never ready")})
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	for _, want := range []string{"http://127.0.0.1:9080/health", "build/artifacts/mobile/driver/iphone/driver.log", "make doctor-ios"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in error: %v", want, err)
		}
	}
}

func TestStartRejectsMissingOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts Options
	}{
		{"missing xctestrun", Options{Destination: "d"}},
		{"missing destination", Options{XCTestRunPath: sampleXCTestRunPath}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			launcher := New(&fakeSpawner{})
			_, err := launcher.Start(context.Background(), tc.opts, &fakePinger{until: 1})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStartPropagatesSpawnError(t *testing.T) {
	t.Parallel()

	launcher := New(&fakeSpawner{err: errors.New("no xcodebuild")})

	_, err := launcher.Start(context.Background(), sampleOptions(), &fakePinger{until: 1})
	if err == nil || !strings.Contains(err.Error(), "spawn xcodebuild") {
		t.Fatalf("expected spawn error, got %v", err)
	}
}

func TestHandleStopIsSafeOnNil(t *testing.T) {
	t.Parallel()

	var handle *Handle
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("expected nil error from nil handle, got %v", err)
	}
}

func TestExecSpawnerCreatesDriverLog(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "driver", "driver.log")
	process, err := ExecSpawner{}.Spawn(context.Background(), "sh", []string{"-c", "echo stdout; echo stderr >&2; sleep 5"}, logPath, nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		if strings.Contains(string(data), "stdout") && strings.Contains(string(data), "stderr") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for log output, got %q", string(data))
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err := process.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	log := string(data)
	if !strings.Contains(log, "stdout") || !strings.Contains(log, "stderr") {
		t.Fatalf("expected stdout and stderr in log, got %q", log)
	}
}
