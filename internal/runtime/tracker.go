package runtime

import "sync"

// depTracker records which steps have terminated in a non-success
// state — either failed or cleanly skipped — so dependents can be
// cascaded to skipped with an explanation.
//
// Failed steps and skipped steps are tracked separately because the
// downstream UX differs: dependents of a failed step are skipped
// because the upstream is broken, while dependents of a skipped step
// are skipped because the upstream simply did not run.
type depTracker struct {
	mu      sync.RWMutex
	failed  map[string]struct{}
	skipped map[string]struct{}
}

func newDepTracker() *depTracker {
	return &depTracker{
		failed:  map[string]struct{}{},
		skipped: map[string]struct{}{},
	}
}

// markFailed records that a step ended in a failure state.
func (t *depTracker) markFailed(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.failed[name] = struct{}{}
}

// markSkipped records that a step was cleanly skipped (skip_if /
// skip_unless rule or cascade).
func (t *depTracker) markSkipped(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.skipped[name] = struct{}{}
}

// dependencyBlocker returns the first dependency in deps that has
// terminated in a non-success state. The second return value is true
// when the blocker failed and false when it was skipped. The third
// return value is true when any blocker was found.
//
// When multiple dependencies are blocked, the order of iteration of
// `deps` (a Go map) is unspecified — callers should not rely on a
// particular blocker being chosen.
func (t *depTracker) dependencyBlocker(deps map[string]struct{}) (string, bool, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for dep := range deps {
		if _, ok := t.failed[dep]; ok {
			return dep, true, true
		}

		if _, ok := t.skipped[dep]; ok {
			return dep, false, true
		}
	}

	return "", false, false
}
