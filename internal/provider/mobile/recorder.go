package mobile

import "context"

// RecordOptions is a resolved scenario-level `record { }` block.
//
// It carries the union of every platform's knobs; each backend consumes
// the ones its recorder understands and rejects the rest by name, so a
// scenario that asks for an iOS-only codec on Android fails with a clear
// message instead of silently recording something else.
type RecordOptions struct {
	// Output is the absolute destination path, already validated against
	// the scenario workspace by the caller.
	Output string

	// Codec, Mask and Display are `simctl io recordVideo` options (iOS).
	Codec   string
	Mask    string
	Display string
	// Force overwrites an existing file at Output (iOS).
	Force bool

	// BitRate and Size are `screenrecord` options (Android).
	BitRate string
	Size    string
}

// Recorder captures the device screen for the duration of one scenario.
//
// Stop returns the path of the finished recording. Implementations must
// let the encoder finalize its container rather than killing it outright,
// otherwise the file is left unplayable.
type Recorder interface {
	Start(ctx context.Context, opts RecordOptions) error
	Stop(ctx context.Context) (string, error)
}

// RecorderFactory builds a Recorder bound to one device. Backends provide
// it; the provider calls it lazily, once a session exists and the device
// handle is known.
type RecorderFactory func(deviceID string) Recorder
