package screenrecord

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProcess struct {
	waits atomic.Int32
}

func (p *fakeProcess) Wait(_ context.Context) error {
	p.waits.Add(1)

	return nil
}

type fakeSpawner struct {
	args    []string
	process *fakeProcess
	err     error
}

func (f *fakeSpawner) Spawn(_ context.Context, _ string, args []string) (Process, error) {
	f.args = append([]string(nil), args...)

	if f.err != nil {
		return nil, f.err
	}

	if f.process == nil {
		f.process = &fakeProcess{}
	}

	return f.process, nil
}

type fakeShell struct {
	commands  []string
	pulls     [][2]string
	shellErr  error
	pullErr   error
	failOnRun string
}

func (f *fakeShell) Shell(_ context.Context, _, command string) (string, error) {
	f.commands = append(f.commands, command)

	if f.failOnRun != "" && strings.Contains(command, f.failOnRun) {
		return "", errors.New("device shell failed")
	}

	return "", f.shellErr
}

func (f *fakeShell) Pull(_ context.Context, _, remotePath, localPath string) error {
	f.pulls = append(f.pulls, [2]string{remotePath, localPath})

	return f.pullErr
}

func sampleOptions() Options {
	return Options{
		ADBPath:     "/sdk/platform-tools/adb",
		Serial:      "emulator-5554",
		Output:      "/work/preview.mp4",
		APILevel:    34,
		StopTimeout: time.Second,
	}
}

func TestBuildArgsUsesTheDefaultBitRate(t *testing.T) {
	t.Parallel()

	opts := sampleOptions()
	opts.BitRate = DefaultBitRate
	opts.DeviceFile = "/sdcard/tales-preview.mp4"

	args := strings.Join(BuildArgs(opts), " ")

	want := "-s emulator-5554 shell screenrecord --bit-rate 4M --time-limit 0 /sdcard/tales-preview.mp4"
	if args != want {
		t.Fatalf("args =\n  %s\nwant\n  %s", args, want)
	}
}

func TestBuildArgsOmitsUnlimitedDurationBelowAPI34(t *testing.T) {
	t.Parallel()

	// Older devices cap at three minutes and reject --time-limit 0
	// outright, so passing it would fail the capture rather than
	// silently truncate it.
	opts := sampleOptions()
	opts.APILevel = 33
	opts.BitRate = DefaultBitRate
	opts.DeviceFile = "/sdcard/tales-preview.mp4"

	if strings.Contains(strings.Join(BuildArgs(opts), " "), "--time-limit") {
		t.Fatalf("--time-limit must not be passed below API %d: %v", UnlimitedDurationMinAPI, BuildArgs(opts))
	}
}

func TestBuildArgsPassesSizeWhenSet(t *testing.T) {
	t.Parallel()

	opts := sampleOptions()
	opts.Size = "720x1280"
	opts.DeviceFile = "/sdcard/tales-preview.mp4"

	if !strings.Contains(strings.Join(BuildArgs(opts), " "), "--size 720x1280") {
		t.Fatalf("expected --size to be forwarded: %v", BuildArgs(opts))
	}
}

func TestDeviceFileKeepsTheOutputBaseName(t *testing.T) {
	t.Parallel()

	// A pulled file should be recognizable from a device shell, which
	// is where a user looks when a recording goes missing.
	if got := DeviceFileFor("/work/artifacts/preview.mp4"); got != "/sdcard/tales-preview.mp4" {
		t.Fatalf("device file = %q", got)
	}
}

func TestStopInterruptsPullsAndCleansUp(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	shell := &fakeShell{}
	recorder := New(spawner, shell)

	opts := sampleOptions()
	opts.StopTimeout = 100 * time.Millisecond

	if err := recorder.Start(context.Background(), opts); err != nil {
		t.Fatalf("start: %v", err)
	}

	path, err := recorder.Stop(context.Background())
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	if path != opts.Output {
		t.Fatalf("stop returned %q, want %q", path, opts.Output)
	}

	// SIGINT, not SIGKILL: screenrecord writes the MP4 trailer on
	// interrupt and a killed process leaves an unplayable file.
	if len(shell.commands) == 0 || !strings.Contains(shell.commands[0], "killall -INT screenrecord") {
		t.Fatalf("expected an interrupt first, got %v", shell.commands)
	}

	if len(shell.pulls) != 1 || shell.pulls[0][1] != opts.Output {
		t.Fatalf("expected the recording to be pulled to the output path, got %v", shell.pulls)
	}

	// A leftover file would accumulate across runs.
	joined := strings.Join(shell.commands, " | ")
	if !strings.Contains(joined, "rm -f") {
		t.Fatalf("expected the device file to be removed, got %v", shell.commands)
	}
}

func TestStopReturnsThePathEvenWhenThePullFails(t *testing.T) {
	t.Parallel()

	spawner := &fakeSpawner{}
	shell := &fakeShell{pullErr: errors.New("adb: no such file")}
	recorder := New(spawner, shell)

	opts := sampleOptions()
	opts.StopTimeout = 100 * time.Millisecond

	if err := recorder.Start(context.Background(), opts); err != nil {
		t.Fatalf("start: %v", err)
	}

	path, err := recorder.Stop(context.Background())
	if err == nil {
		t.Fatal("expected the pull failure to be reported")
	}

	// The caller surfaces a partial capture rather than dropping it
	// silently, so the path has to come back regardless.
	if path != opts.Output {
		t.Fatalf("expected the output path even on failure, got %q", path)
	}
}

func TestStopOnAnIdleRecorderIsANoop(t *testing.T) {
	t.Parallel()

	recorder := New(&fakeSpawner{}, &fakeShell{})

	path, err := recorder.Stop(context.Background())
	if err != nil || path != "" {
		t.Fatalf("expected a silent no-op, got path=%q err=%v", path, err)
	}
}

func TestStartRejectsASecondConcurrentRecording(t *testing.T) {
	t.Parallel()

	recorder := New(&fakeSpawner{}, &fakeShell{})

	if err := recorder.Start(context.Background(), sampleOptions()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := recorder.Start(context.Background(), sampleOptions()); err == nil {
		t.Fatal("expected the second start to be rejected")
	}
}

func TestStartRequiresAnOutputPath(t *testing.T) {
	t.Parallel()

	recorder := New(&fakeSpawner{}, &fakeShell{})

	opts := sampleOptions()
	opts.Output = ""

	if err := recorder.Start(context.Background(), opts); err == nil {
		t.Fatal("expected an error when no output path is given")
	}
}
