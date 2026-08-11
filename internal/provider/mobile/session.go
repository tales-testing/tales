package mobile

import (
	"context"
	"fmt"

	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// Session is the per-target state cached by the provider between mobile
// steps. It owns the running driver client and, when Tales started the
// driver itself, the handle used to stop it.
type Session struct {
	Target Target
	// DeviceID is the platform's device handle: a simulator UDID on iOS,
	// an adb serial on Android. Opaque to the provider, which only hands
	// it back to the Lifecycle and the Recorder.
	DeviceID     string
	Driver       driver.Driver
	DriverHandle DriverHandle
	Lifecycle    Lifecycle
	// Diagnostics carries the on-disk paths most useful when the driver
	// process dies mid-scenario. The provider quotes them in
	// transport-level error messages so users land directly on the file
	// that holds the crash report instead of just seeing
	// "connect: connection refused". Empty for external drivers (Tales
	// doesn't own their files).
	Diagnostics Diagnostics
}

// SessionBuilder builds (or rebuilds) a session for one target. Each
// platform backend implements it.
type SessionBuilder interface {
	Build(ctx context.Context, target Target) (*Session, error)
}

// SessionBuilderFunc is a convenience adapter to use a function as a
// SessionBuilder.
type SessionBuilderFunc func(ctx context.Context, target Target) (*Session, error)

// Build implements SessionBuilder.
func (f SessionBuilderFunc) Build(ctx context.Context, target Target) (*Session, error) {
	return f(ctx, target)
}

// Close shuts down the session: terminate the app (best-effort) and stop
// the driver subprocess Tales started, if any.
func (s *Session) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if s.Lifecycle != nil && s.DeviceID != "" && s.Target.BundleID != "" {
		_ = s.Lifecycle.TerminateApp(ctx, s.DeviceID, s.Target)
	}

	if s.DriverHandle != nil {
		if err := s.DriverHandle.Stop(ctx); err != nil {
			return fmt.Errorf("stop driver: %w", err)
		}

		// Belt-and-suspenders: kill the on-device process hosting the
		// driver so it cannot squat the port for the next session. Only
		// runs when Tales started the driver (DriverHandle non-nil) —
		// external drivers are never touched.
		if s.Lifecycle != nil {
			_ = s.Lifecycle.TerminateDriverRunner(ctx, s.DeviceID)
		}
	}

	return nil
}
