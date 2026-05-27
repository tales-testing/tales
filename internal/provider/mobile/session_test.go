package mobile

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tales-testing/tales/internal/provider/mobile/apple"
)

type fakeDriverHandle struct {
	stops atomic.Int32
	err   error
}

func (f *fakeDriverHandle) Stop(_ context.Context) error {
	f.stops.Add(1)

	return f.err
}

func TestSessionCloseStopsInternalDriverHandle(t *testing.T) {
	t.Parallel()

	handle := &fakeDriverHandle{}
	lc := &fakeLifecycle{udid: "UDID"}
	session := &Session{
		Target:       sampleProviderTarget(),
		UDID:         "UDID",
		Lifecycle:    lc.toAppleLifecycle(),
		DriverHandle: handle,
	}

	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := handle.stops.Load(); got != 1 {
		t.Fatalf("expected driver handle to stop once, got %d", got)
	}
	// Two simctl terminate calls expected:
	//   1. the configured app bundle (graceful shutdown of the SUT).
	//   2. the XCUITest runner bundle (defensive cleanup so the
	//      in-simulator HTTP server cannot squat the driver port).
	if got := lc.terminates.Load(); got != 2 {
		t.Fatalf("expected app + runner termination (2 calls), got %d", got)
	}
}

func TestSessionCloseWithExternalDriverDoesNotRequireHandle(t *testing.T) {
	t.Parallel()

	session := &Session{
		Target:    apple.Target{Name: "iphone", BundleID: "com.example.MyApp"},
		UDID:      "UDID",
		Lifecycle: (&fakeLifecycle{udid: "UDID"}).toAppleLifecycle(),
	}

	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSessionCloseReturnsDriverStopError(t *testing.T) {
	t.Parallel()

	session := &Session{
		Target:       sampleProviderTarget(),
		UDID:         "UDID",
		Lifecycle:    (&fakeLifecycle{udid: "UDID"}).toAppleLifecycle(),
		DriverHandle: &fakeDriverHandle{err: errors.New("boom")},
	}

	err := session.Close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stop driver") {
		t.Fatalf("expected wrapped driver stop error, got %v", err)
	}
}
