package chrome

import "context"

// mergeContext returns a context that is a descendant of bound (so its
// chromedp browser binding is preserved) and is canceled when ctx is
// canceled. If ctx has a deadline, the merged context inherits that
// deadline so per-action timeouts apply.
func mergeContext(ctx, bound context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		merged, cancel := context.WithDeadline(bound, deadline)
		stop := watchCancellation(ctx, merged, cancel)

		return merged, func() {
			stop()
			cancel()
		}
	}

	merged, cancel := context.WithCancel(bound)
	stop := watchCancellation(ctx, merged, cancel)

	return merged, func() {
		stop()
		cancel()
	}
}

// watchCancellation arranges for cancel() to fire when source is canceled,
// without leaking the watcher goroutine if merged finishes first. The
// returned stop() cleans up the watcher when the caller is done.
func watchCancellation(source, merged context.Context, cancel context.CancelFunc) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-source.Done():
			cancel()
		case <-merged.Done():
		case <-done:
		}
	}()

	stopped := false

	return func() {
		if stopped {
			return
		}

		stopped = true

		close(done)
	}
}
