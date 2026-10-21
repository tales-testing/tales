package chrome

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// emitOnListen returns a listenFunc that delivers events to the listener
// from another goroutine, as chromedp does from the target's event loop.
func emitOnListen(events ...any) listenFunc {
	return func(_ context.Context, fn func(ev any)) {
		go func() {
			for _, ev := range events {
				fn(ev)
			}
		}()
	}
}

// waitForever models a CDP command whose reply never comes: it only
// returns when its context ends.
func waitForever(ctx context.Context) error {
	<-ctx.Done()

	return ctx.Err()
}

func mainFrameNavigated() *page.EventFrameNavigated {
	return &page.EventFrameNavigated{Frame: &cdp.Frame{ID: "main", URL: "http://127.0.0.1/login.html"}}
}

func TestAbortOnNavigationGivesUpWhenTheMainFrameNavigates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()

	err := abortOnNavigation(ctx, emitOnListen(mainFrameNavigated()), waitForever)
	if !errors.Is(err, driver.ErrNavigated) {
		t.Fatalf("err = %v, want driver.ErrNavigated", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("abort took %s: it waited for the deadline instead of the navigation", elapsed)
	}
}

func TestAbortOnNavigationIgnoresSubFrameNavigations(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	iframe := &page.EventFrameNavigated{Frame: &cdp.Frame{ID: "child", ParentID: "main"}}

	err := abortOnNavigation(ctx, emitOnListen(iframe, &page.EventLoadEventFired{}), waitForever)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
}

func TestAbortOnNavigationKeepsACompletedCall(t *testing.T) {
	t.Parallel()

	err := abortOnNavigation(context.Background(), emitOnListen(mainFrameNavigated()), func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("a call that completed must not be reported as interrupted: %v", err)
	}
}

func TestAbortOnNavigationPassesOtherErrorsThrough(t *testing.T) {
	t.Parallel()

	boom := errors.New("target closed")

	err := abortOnNavigation(context.Background(), emitOnListen(), func(context.Context) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestAbortOnNavigationStopsListeningWhenTheCallReturns(t *testing.T) {
	t.Parallel()

	var listenerCtx context.Context

	listen := func(ctx context.Context, _ func(ev any)) { listenerCtx = ctx }

	if err := abortOnNavigation(context.Background(), listen, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("abortOnNavigation: %v", err)
	}

	// chromedp drops a listener once its context is done.
	if listenerCtx == nil || listenerCtx.Err() == nil {
		t.Fatal("the listener context must be done once the call returns")
	}
}
