// Package driver exposes the transport-agnostic interface every mobile UI
// driver must implement. V1 ships an HTTP/JSON client targeted at the
// XCUITest-based TalesAppleDriver, but the interface is intentionally
// transport-agnostic so a future gRPC or local-IPC client can plug in
// without touching the provider call sites.
package driver

import (
	"context"

	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

// Driver abstracts the low-level mobile UI commands.
type Driver interface {
	// Health pings the driver and returns nil when the driver is ready to
	// accept commands.
	Health(ctx context.Context) error

	// Hierarchy returns the normalized view tree for the given bundle id.
	Hierarchy(ctx context.Context, bundleID string) (*tree.ViewNode, error)

	// Tap performs a single tap at the given screen-space coordinates.
	Tap(ctx context.Context, bundleID string, x, y float64) error

	// InputText types into the currently focused element.
	InputText(ctx context.Context, bundleID string, text string) error

	// EraseText erases the given number of characters from the focused
	// element.
	EraseText(ctx context.Context, bundleID string, characters int) error

	// Screenshot captures a PNG-encoded screenshot of the active screen.
	Screenshot(ctx context.Context) ([]byte, error)
}
