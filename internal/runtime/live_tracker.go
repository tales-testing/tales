package runtime

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// liveScenarioTracker holds the names of scenarios currently executing.
// It exists to power the cancel-by-timeout diagnostic: when --timeout fires
// before runner.Run finishes, the CLI needs to know which scenarios were
// in-flight at that exact moment to point users at the culprits.
//
// The tracker is updated from each scenario goroutine and read from a
// single watcher goroutine, so all accesses go through the mutex.
type liveScenarioTracker struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func newLiveScenarioTracker() *liveScenarioTracker {
	return &liveScenarioTracker{active: map[string]struct{}{}}
}

func (t *liveScenarioTracker) start(name string) {
	t.mu.Lock()
	t.active[name] = struct{}{}
	t.mu.Unlock()
}

func (t *liveScenarioTracker) end(name string) {
	t.mu.Lock()
	delete(t.active, name)
	t.mu.Unlock()
}

// snapshot returns a sorted copy of the names still in flight. Sorting is
// not for correctness but for deterministic CLI output and test assertions.
func (t *liveScenarioTracker) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]string, 0, len(t.active))
	for name := range t.active {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// watchDeadlineFor returns a buffered channel that will receive the list of
// scenarios still in flight at the exact moment ctx hits DeadlineExceeded.
// On any other termination path (context.Canceled, normal completion when
// the runner closes the channel via its parent context's cancel) the
// returned channel receives nil.
//
// The snapshot is taken as soon as ctx.Done fires, BEFORE the scheduler has
// a chance to run the scenario goroutines, so they cannot race to call
// tracker.end first. This is best-effort: a goroutine that was already
// inside its emit/end sequence when ctx fired could still slip through, but
// for the case that matters (a scenario blocked inside provider.Execute) it
// captures the blocked name reliably.
func watchDeadlineFor(ctx context.Context, tracker *liveScenarioTracker) <-chan []string {
	out := make(chan []string, 1)

	go func() {
		<-ctx.Done()

		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out <- tracker.snapshot()

			return
		}

		out <- nil
	}()

	return out
}
