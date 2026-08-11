// Package screenrecord drives `adb shell screenrecord` to capture a
// scenario's screen, then pulls the finished file off the device.
//
// It mirrors simrecord on the Apple side, with one structural
// difference: simctl writes straight to the host, whereas screenrecord
// writes on the device and the file has to be pulled afterwards. That
// makes Stop a three-step operation — signal, wait for the muxer, pull —
// and each step has a failure mode worth reporting distinctly.
package screenrecord

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"
)

// Defaults for the capture and its teardown.
const (
	// DefaultBitRate is screenrecord's own default made explicit. The
	// recordings are debugging aids, not deliverables, so the smaller
	// file is worth more than the extra fidelity.
	DefaultBitRate = "4M"
	// DefaultStopTimeout bounds the wait for the muxer to finalize the
	// container after the interrupt.
	DefaultStopTimeout = 15 * time.Second
	// MuxerFlushDelay is how long screenrecord is given to write the
	// MP4 trailer after SIGINT. Without it the pulled file has no moov
	// atom and no player will open it.
	MuxerFlushDelay = 3 * time.Second
	// UnlimitedDurationMinAPI is the first API level where
	// `--time-limit 0` means "no limit". Below it screenrecord caps at
	// three minutes and rejects 0.
	UnlimitedDurationMinAPI = 34
	// DeviceDir is where recordings are written on the device.
	DeviceDir = "/sdcard"
)

// Process is a running screenrecord invocation.
type Process interface {
	// Wait blocks until the process exits.
	Wait(ctx context.Context) error
}

// Spawner starts screenrecord in the background.
type Spawner interface {
	Spawn(ctx context.Context, name string, args []string) (Process, error)
}

// Shell runs a one-shot command on the device and pulls files off it.
// *adb.Tool satisfies it.
type Shell interface {
	Shell(ctx context.Context, serial, command string) (string, error)
	Pull(ctx context.Context, serial, remotePath, localPath string) error
}

// Options drives one recording.
type Options struct {
	// ADBPath and Serial address the device.
	ADBPath string
	Serial  string
	// Output is the host path the finished MP4 is pulled to.
	Output string
	// DeviceFile is the on-device path. Derived from Output when empty.
	DeviceFile string
	// BitRate is screenrecord's --bit-rate ("4M", "100000").
	BitRate string
	// Size is screenrecord's --size ("720x1280"). Empty keeps the
	// device's native resolution.
	Size string
	// APILevel gates --time-limit 0.
	APILevel int
	// StopTimeout overrides DefaultStopTimeout.
	StopTimeout time.Duration
}

// Recorder owns one screenrecord invocation.
type Recorder struct {
	spawner Spawner
	shell   Shell

	mu      sync.Mutex
	process Process
	opts    Options
	started bool
}

// New returns a Recorder driving the given spawner and device shell.
func New(spawner Spawner, shell Shell) *Recorder {
	return &Recorder{spawner: spawner, shell: shell}
}

// Start begins recording. It returns an error if a recording is already
// in flight on this Recorder.
func (r *Recorder) Start(ctx context.Context, opts Options) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return errors.New("screenrecord: a recording is already in progress")
	}

	if opts.Output == "" {
		return errors.New("screenrecord: output path is required")
	}

	if opts.DeviceFile == "" {
		opts.DeviceFile = DeviceFileFor(opts.Output)
	}

	if opts.BitRate == "" {
		opts.BitRate = DefaultBitRate
	}

	process, err := r.spawner.Spawn(ctx, opts.ADBPath, BuildArgs(opts))
	if err != nil {
		return fmt.Errorf("start screenrecord: %w", err)
	}

	r.process = process
	r.opts = opts
	r.started = true

	return nil
}

// Stop interrupts the recording, waits for the container to be
// finalized, and pulls the file to Options.Output.
//
// It returns the host path even on failure, so the caller can surface a
// partial capture rather than silently dropping it.
func (r *Recorder) Stop(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return "", nil
	}

	r.started = false
	opts := r.opts
	process := r.process
	r.process = nil

	timeout := opts.StopTimeout
	if timeout <= 0 {
		timeout = DefaultStopTimeout
	}

	stopCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// SIGINT, never SIGKILL: screenrecord writes the MP4 trailer on
	// interrupt, and a killed process leaves a file no player opens.
	if _, err := r.shell.Shell(stopCtx, opts.Serial, "killall -INT screenrecord"); err != nil {
		return opts.Output, fmt.Errorf("interrupt screenrecord: %w", err)
	}

	if process != nil {
		_ = process.Wait(stopCtx)
	}

	// screenrecord's exit does not guarantee the trailer has reached
	// storage; give the muxer a moment before reading the file.
	select {
	case <-time.After(MuxerFlushDelay):
	case <-stopCtx.Done():
	}

	if err := r.shell.Pull(stopCtx, opts.Serial, opts.DeviceFile, opts.Output); err != nil {
		return opts.Output, fmt.Errorf("pull recording: %w", err)
	}

	// Best-effort cleanup: a leftover file would accumulate across runs,
	// but failing to delete it must not fail an otherwise good scenario.
	_, _ = r.shell.Shell(stopCtx, opts.Serial, "rm -f "+opts.DeviceFile)

	return opts.Output, nil
}

// BuildArgs renders the adb argument list for one capture.
func BuildArgs(opts Options) []string {
	args := []string{"-s", opts.Serial, "shell", "screenrecord"}

	if opts.BitRate != "" {
		args = append(args, "--bit-rate", opts.BitRate)
	}

	if opts.Size != "" {
		args = append(args, "--size", opts.Size)
	}

	// Below API 34 screenrecord caps at three minutes and rejects
	// --time-limit 0 outright, so the flag is only safe to pass on
	// devices that understand it.
	if opts.APILevel >= UnlimitedDurationMinAPI {
		args = append(args, "--time-limit", "0")
	}

	return append(args, opts.DeviceFile)
}

// DeviceFileFor derives the on-device path from the host output path,
// keeping the base name so a pulled file is recognizable in a shell.
func DeviceFileFor(output string) string {
	name := path.Base(output)
	if name == "" || name == "." || name == "/" {
		name = "recording.mp4"
	}

	return DeviceDir + "/tales-" + name
}
