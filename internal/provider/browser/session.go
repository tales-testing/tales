package browser

import (
	"context"
	"sync"

	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// Session is one cached Chrome process (allocator) for a given target.
// Scenarios derive isolated browsing contexts off this allocator via
// SessionBuilder.NewScenarioContext.
type Session struct {
	TargetName string
	Target     Target
	Cancel     context.CancelFunc

	mu        sync.Mutex
	scenarios map[string]*ScenarioBrowserCtx
}

// ScenarioBrowserCtx is one per-scenario browsing context (incognito-style
// isolation). It wraps a chromedp.Context plus its cancel func plus the
// driver bound to it.
type ScenarioBrowserCtx struct {
	Driver driver.Driver
	Cancel context.CancelFunc
}

// SessionBuilder is the factory the provider uses to launch Chrome and
// obtain per-scenario contexts. Tests inject a fake builder; the production
// chromedp implementation lives in subpackage chrome.
type SessionBuilder interface {
	// Build launches Chrome for the given target (returns a Session whose
	// allocator is alive until Cancel is called).
	Build(ctx context.Context, target Target) (*Session, error)
	// NewScenarioContext derives a fresh per-scenario browsing context off
	// the session's allocator. Canceling it must release the underlying
	// resources.
	NewScenarioContext(ctx context.Context, sess *Session, scenario string) (*ScenarioBrowserCtx, error)
}

// SessionBuilderFunc adapts a pair of plain functions to SessionBuilder.
type SessionBuilderFunc struct {
	BuildFn    func(ctx context.Context, target Target) (*Session, error)
	ScenarioFn func(ctx context.Context, sess *Session, scenario string) (*ScenarioBrowserCtx, error)
}

// Build implements SessionBuilder.
func (f SessionBuilderFunc) Build(ctx context.Context, target Target) (*Session, error) {
	return f.BuildFn(ctx, target)
}

// NewScenarioContext implements SessionBuilder.
func (f SessionBuilderFunc) NewScenarioContext(ctx context.Context, sess *Session, scenario string) (*ScenarioBrowserCtx, error) {
	return f.ScenarioFn(ctx, sess, scenario)
}

// closeAll cancels every per-scenario context and the allocator. Safe to
// call multiple times.
func (s *Session) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sc := range s.scenarios {
		if sc.Driver != nil {
			_ = sc.Driver.Close(context.Background())
		}

		if sc.Cancel != nil {
			sc.Cancel()
		}
	}

	s.scenarios = nil

	if s.Cancel != nil {
		s.Cancel()
		s.Cancel = nil
	}
}
