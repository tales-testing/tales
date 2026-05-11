package xcodebuild

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSpawn struct {
	name string
	args []string
}

type fakeSpawner struct {
	calls   []fakeSpawn
	process *fakeProcess
	err     error
}

func (f *fakeSpawner) Spawn(_ context.Context, name string, args []string) (Process, error) {
	f.calls = append(f.calls, fakeSpawn{name: name, args: append([]string(nil), args...)})

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

func TestBuildArgsContainsRequiredFlags(t *testing.T) {
	t.Parallel()

	got := BuildArgs(Options{
		Project:     "drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj",
		Scheme:      "TalesAppleDriverUITests",
		Destination: "platform=iOS Simulator,id=ABC",
	})

	want := []string{
		"test",
		"-project", "drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj",
		"-scheme", "TalesAppleDriverUITests",
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

	handle, err := launcher.Start(context.Background(), Options{
		Project:       "p.xcodeproj",
		Scheme:        "S",
		Destination:   "platform=iOS Simulator,id=ABC",
		HealthTimeout: 2 * time.Second,
		PollInterval:  5 * time.Millisecond,
	}, pinger)
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

func TestStartStopsProcessOnHealthTimeout(t *testing.T) {
	t.Parallel()

	process := &fakeProcess{}
	spawner := &fakeSpawner{process: process}
	pinger := &fakePinger{until: 999, err: errors.New("never ready")}

	launcher := New(spawner)

	_, err := launcher.Start(context.Background(), Options{
		Project:       "p.xcodeproj",
		Scheme:        "S",
		Destination:   "platform=iOS Simulator,id=ABC",
		HealthTimeout: 30 * time.Millisecond,
		PollInterval:  5 * time.Millisecond,
	}, pinger)
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

func TestStartRejectsMissingOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts Options
	}{
		{"missing project", Options{Scheme: "s", Destination: "d"}},
		{"missing scheme", Options{Project: "p", Destination: "d"}},
		{"missing destination", Options{Project: "p", Scheme: "s"}},
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

	_, err := launcher.Start(context.Background(), Options{
		Project: "p", Scheme: "s", Destination: "d",
	}, &fakePinger{until: 1})
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
