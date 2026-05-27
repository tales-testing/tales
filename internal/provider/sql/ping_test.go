package sql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestPingWithBoundedTimeout_RespectsParentCancel proves that a parent ctx
// already cancelled propagates: the ping returns immediately with an error
// that mentions the bounded budget, not after 10s. This is the property that
// makes --timeout authoritative even when the dial happens inside a provider
// the user did not write.
//
// No ExpectPing is registered here: database/sql short-circuits PingContext
// when the supplied context is already canceled, so the driver mock is
// never invoked. Asserting expectations would fail spuriously and would
// also obscure the actual property under test, which is the wall-clock
// behavior.
func TestPingWithBoundedTimeout_RespectsParentCancel(t *testing.T) {
	t.Parallel()

	db, _, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	parent, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err = pingWithBoundedTimeout(parent, db)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when parent ctx is already cancelled")
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("ping should return immediately on cancelled parent, took %s", elapsed)
	}

	if !strings.Contains(err.Error(), "did not respond") {
		t.Errorf("error should mention the bounded budget, got: %v", err)
	}
}

// TestPingWithBoundedTimeout_PingErrorWraps proves the driver error is wrapped
// with the timeout context, so users see both "did not respond within 10s"
// and the underlying driver message. The wrapping must preserve errors.Is so
// callers can still test for specific driver error types.
func TestPingWithBoundedTimeout_PingErrorWraps(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectPing().WillReturnError(errPingRefused)

	err = pingWithBoundedTimeout(context.Background(), db)
	if err == nil {
		t.Fatal("expected ping error")
	}

	if !strings.Contains(err.Error(), "did not respond") {
		t.Errorf("error should mention the bounded budget, got: %v", err)
	}

	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should propagate the driver message, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("ping expectation was not consumed: %v", err)
	}
}

// errPingRefused mimics a typical "host:port not listening" driver error.
var errPingRefused = &pingErr{msg: "dial tcp 127.0.0.1:33068: connect: connection refused"}

type pingErr struct{ msg string }

func (e *pingErr) Error() string { return e.msg }
