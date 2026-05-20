package driver

import (
	"context"

	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

// NoopDriver is a Driver whose every method is a harmless no-op. It exists
// so test fakes can embed it and only implement the methods they actually
// exercise: when the Driver interface grows a method, the fakes keep
// compiling instead of every one needing a new stub. Production code never
// uses NoopDriver — the real implementation is the HTTP Client.
type NoopDriver struct{}

// Health reports the driver as ready.
func (NoopDriver) Health(_ context.Context) error { return nil }

// Hierarchy returns no tree.
func (NoopDriver) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	return nil, nil
}

// Tap does nothing.
func (NoopDriver) Tap(_ context.Context, _, _ string, _, _ float64) error { return nil }

// Swipe does nothing.
func (NoopDriver) Swipe(_ context.Context, _ string, _, _, _, _, _ float64) error { return nil }

// LongPress does nothing.
func (NoopDriver) LongPress(_ context.Context, _, _ string, _, _, _ float64) error { return nil }

// DoubleTap does nothing.
func (NoopDriver) DoubleTap(_ context.Context, _, _ string, _, _ float64) error { return nil }

// InputText does nothing.
func (NoopDriver) InputText(_ context.Context, _, _, _ string, _ bool) error { return nil }

// EraseText does nothing.
func (NoopDriver) EraseText(_ context.Context, _ string, _ int) error { return nil }

// Screenshot returns an empty image.
func (NoopDriver) Screenshot(_ context.Context) ([]byte, error) { return []byte{}, nil }
