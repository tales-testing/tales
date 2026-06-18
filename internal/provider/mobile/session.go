package mobile

import (
	"context"
	"fmt"

	"github.com/tales-testing/tales/internal/provider/mobile/apple"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
)

// Session is the per-target state cached by the provider between mobile
// steps. It owns the running driver client and the optional xcodebuild
// handle Tales started.
type Session struct {
	Target       apple.Target
	UDID         string
	Driver       driver.Driver
	DriverHandle apple.DriverHandle
	Lifecycle    *apple.Lifecycle
	// Diagnostics carries the on-disk paths most useful when the driver
	// process dies mid-scenario (driver.log, xcresult bundle dir, build
	// log). The provider quotes them in transport-level error messages
	// so users land directly on the file that holds the XCTest crash
	// report instead of just seeing "connect: connection refused".
	// Empty for external drivers (Tales doesn't own their files).
	Diagnostics apple.DriverDiagnostics
}

// SessionBuilder builds (or rebuilds) a session for one target.
type SessionBuilder interface {
	Build(ctx context.Context, target apple.Target) (*Session, error)
}

// SessionBuilderFunc is a convenience adapter to use a function as a
// SessionBuilder.
type SessionBuilderFunc func(ctx context.Context, target apple.Target) (*Session, error)

// Build implements SessionBuilder.
func (f SessionBuilderFunc) Build(ctx context.Context, target apple.Target) (*Session, error) {
	return f(ctx, target)
}

// Close shuts down the session: terminate the app (best-effort) and stop the
// xcodebuild subprocess Tales started, if any.
func (s *Session) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if s.Lifecycle != nil && s.UDID != "" && s.Target.BundleID != "" {
		_ = s.Lifecycle.TerminateApp(ctx, s.UDID, s.Target)
	}

	if s.DriverHandle != nil {
		if err := s.DriverHandle.Stop(ctx); err != nil {
			return fmt.Errorf("stop driver: %w", err)
		}

		// Belt-and-suspenders: kill the in-simulator XCUITest runner so
		// it cannot squat the driver port for the next session. Only
		// runs when Tales started the driver (DriverHandle non-nil) —
		// external drivers are never touched.
		if s.Lifecycle != nil {
			_ = s.Lifecycle.TerminateDriverRunner(ctx, s.UDID)
		}
	}

	return nil
}
