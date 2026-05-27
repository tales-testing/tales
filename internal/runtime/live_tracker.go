package runtime

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/hyperxlab/tales/internal/report"
)

// ActiveScenario is re-exported from report so the EventSink contract reads
// as a single type. The data lives in the report package because runtime
// already imports report and not the other way around.
type ActiveScenario = report.ActiveScenario

// liveScenarioTracker holds the names of scenarios currently executing,
// along with the wall-clock instant each one became active. It powers two
// observability surfaces: the cancel-by-timeout diagnostic (which only
// needs names) and the optional --verbose heartbeat (which needs durations).
//
// The tracker is updated from each scenario goroutine and read from at
// least two watcher goroutines (deadline watcher, heartbeat ticker), so all
// accesses go through the mutex.
type liveScenarioTracker struct {
	mu     sync.Mutex
	active map[string]time.Time
}

func newLiveScenarioTracker() *liveScenarioTracker {
	return &liveScenarioTracker{active: map[string]time.Time{}}
}

func (t *liveScenarioTracker) start(name string) {
	t.mu.Lock()
	t.active[name] = time.Now()
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

// snapshotWithElapsed is the richer form used by the heartbeat: each entry
// carries the wall-clock duration since the scenario started. The result is
// sorted by name for stable output.
func (t *liveScenarioTracker) snapshotWithElapsed(now time.Time) []ActiveScenario {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]ActiveScenario, 0, len(t.active))
	for name, startedAt := range t.active {
		out = append(out, ActiveScenario{Name: name, Elapsed: now.Sub(startedAt)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// startHeartbeat optionally spawns a ticker goroutine that calls
// sink.Heartbeat with the currently in-flight scenarios at every interval.
// It returns a stop function that must be called by the runner so the
// ticker does not outlive the suite. When sink is nil or interval is 0 the
// returned stop is a no-op and no goroutine is spawned.
//
// The ticker exits on three conditions: ctx done, an explicit stop call, or
// any panic propagating from the sink (sinks are expected to be safe but a
// defer-recover would only mask bugs — we let it bubble).
func startHeartbeat(ctx context.Context, sink EventSink, tracker *liveScenarioTracker, interval time.Duration) func() {
	if sink == nil || interval <= 0 {
		return func() {}
	}

	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				emitHeartbeat(sink, tracker.snapshotWithElapsed(now))
			}
		}
	}()

	return func() { close(stop) }
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
