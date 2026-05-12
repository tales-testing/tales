package mobile

import (
	"context"
	"fmt"

	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
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
	}

	return nil
}
