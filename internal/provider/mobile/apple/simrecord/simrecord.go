// Package simrecord drives `xcrun simctl io <UDID> recordVideo` to capture a
// screen recording of an iOS simulator. The recorder is a long-running
// subprocess; Stop sends SIGINT and waits for the process to flush a valid
// MP4 container. SIGKILL is intentionally not used: it leaves the file
// without a moov atom and renders the recording unplayable.
package simrecord

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultStopTimeout is how long Stop waits for the simctl process to exit
// after SIGINT before declaring a hard failure. The recording is preserved
// either way.
const DefaultStopTimeout = 10 * time.Second

// Codec values accepted by `simctl io recordVideo`.
const (
	CodecH264 = "h264"
	CodecHEVC = "hevc"
)

// Mask values accepted by `simctl io recordVideo`.
const (
	MaskIgnored = "ignored"
	MaskAlpha   = "alpha"
	MaskBlack   = "black"
)

// Display values accepted by `simctl io recordVideo`.
const (
	DisplayInternal = "internal"
	DisplayExternal = "external"
)

// Spawner starts an external command and returns a Process handle. Replace
// in tests with a fake spawner.
type Spawner interface {
	Spawn(ctx context.Context, name string, args []string, env map[string]string) (Process, error)
}

// Process represents a running simctl recordVideo subprocess.
type Process interface {
	// Stop sends SIGINT and waits for the process to exit so the MP4 is
	// flushed cleanly. It returns an error only when the process does not
	// exit before the caller's context elapses; the partially-written file
	// remains on disk for the user to recover.
	Stop(ctx context.Context) error
}

// Options drives a single `xcrun simctl io <UDID> recordVideo <output>`
// invocation. UDID and Output are required; the remaining fields are
// passthroughs validated by simctl.
type Options struct {
	UDID    string
	Output  string
	Codec   string
	Mask    string
	Display string
	Force   bool
	// StopTimeout overrides DefaultStopTimeout when non-zero.
	StopTimeout time.Duration
	// Env is forwarded to the spawner; nil keeps the parent environment.
	Env map[string]string
}

// Recorder owns one active recording lifecycle. It is safe for concurrent
// calls to Start / Stop, but only one recording may be active at a time.
type Recorder struct {
	spawner Spawner

	mu      sync.Mutex
	process Process
	output  string
	timeout time.Duration
}

// New returns a Recorder driven by the given Spawner. The recommended
// production spawner is ExecSpawner{} which shells out via os/exec.
func New(spawner Spawner) *Recorder {
	return &Recorder{spawner: spawner}
}

// Start launches `xcrun simctl io <UDID> recordVideo <output>` and returns
// once the subprocess has been spawned. Output validation (e.g. ensuring the
// directory exists, enforcing the workspace boundary) is the caller's job.
// Start is rejected if a recording is already running on this Recorder.
func (r *Recorder) Start(ctx context.Context, opts Options) error {
	if err := validateOptions(opts); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.process != nil {
		return fmt.Errorf("simrecord: a recording is already running for %s", r.output)
	}

	args := BuildArgs(opts)

	process, err := r.spawner.Spawn(ctx, "xcrun", args, opts.Env)
	if err != nil {
		return fmt.Errorf("spawn simctl recordVideo: %w", err)
	}

	r.process = process
	r.output = opts.Output
	r.timeout = opts.StopTimeout

	if r.timeout <= 0 {
		r.timeout = DefaultStopTimeout
	}

	return nil
}

// Stop interrupts the recording with SIGINT, waits for the process to flush
// a valid MP4, and returns the output path. Stop is a no-op (returns "" and
// nil) when no recording is running. After Stop, the Recorder is ready for
// a new Start.
func (r *Recorder) Stop(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.process == nil {
		return "", nil
	}

	stopCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	err := r.process.Stop(stopCtx)
	output := r.output

	r.process = nil
	r.output = ""
	r.timeout = 0

	if err != nil {
		return output, fmt.Errorf("stop simctl recordVideo: %w", err)
	}

	return output, nil
}

// BuildArgs produces the simctl argv for the given options. Exported for
// test inspection. Order is: simctl io <udid> recordVideo [flags] <output>.
// Flags are emitted only when set so simctl falls back to its own defaults.
func BuildArgs(opts Options) []string {
	args := make([]string, 0, 12)
	args = append(args, "simctl", "io", opts.UDID, "recordVideo")

	if opts.Codec != "" {
		args = append(args, "--codec", opts.Codec)
	}

	if opts.Mask != "" {
		args = append(args, "--mask", opts.Mask)
	}

	if opts.Display != "" {
		args = append(args, "--display", opts.Display)
	}

	if opts.Force {
		args = append(args, "--force")
	}

	args = append(args, opts.Output)

	return args
}

func validateOptions(opts Options) error {
	if opts.UDID == "" {
		return errors.New("simrecord: udid is required")
	}

	if opts.Output == "" {
		return errors.New("simrecord: output path is required")
	}

	return nil
}
