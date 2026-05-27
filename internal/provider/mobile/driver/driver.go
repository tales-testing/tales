// Package driver exposes the transport-agnostic interface every mobile UI
// driver must implement. V1 ships an HTTP/JSON client targeted at the
// XCUITest-based TalesAppleDriver, but the interface is intentionally
// transport-agnostic so a future gRPC or local-IPC client can plug in
// without touching the provider call sites.
package driver

import (
	"context"

	"github.com/tales-testing/tales/internal/provider/mobile/tree"
)

// Driver abstracts the low-level mobile UI commands.
type Driver interface {
	// Health pings the driver and returns nil when the driver is ready to
	// accept commands.
	Health(ctx context.Context) error

	// Hierarchy returns the normalized view tree for the given bundle id.
	Hierarchy(ctx context.Context, bundleID string) (*tree.ViewNode, error)

	// Tap performs a single tap. When id is non-empty the driver resolves
	// the accessibility identifier first and taps the matched element; (x,y)
	// remain the screen-space fallback used when the element cannot be
	// located or is not hittable.
	Tap(ctx context.Context, bundleID, id string, x, y float64) error

	// Swipe drags one finger from (startX,startY) to (endX,endY) over the
	// given duration. Coordinates are screen-space; the provider computes
	// them from the target element bounds (or the screen) so this also
	// backs the scroll action.
	Swipe(ctx context.Context, bundleID string, startX, startY, endX, endY, duration float64) error

	// LongPress presses and holds at the element identified by id (with
	// (x,y) as the screen-space fallback) for the given duration in
	// seconds.
	LongPress(ctx context.Context, bundleID, id string, x, y, duration float64) error

	// DoubleTap performs two quick taps at the element identified by id
	// (with (x,y) as the screen-space fallback).
	DoubleTap(ctx context.Context, bundleID, id string, x, y float64) error

	// PressKey presses a hardware keyboard key by name (return, enter,
	// tab, space, escape, delete).
	PressKey(ctx context.Context, bundleID, key string) error

	// PressButton presses a device button by name (home, lock).
	PressButton(ctx context.Context, bundleID, button string) error

	// SetOrientation changes the device orientation (portrait,
	// landscape_left, landscape_right, upside_down).
	SetOrientation(ctx context.Context, orientation string) error

	// InputText sets text on the element identified by id. When paste is
	// true the driver taps the element to focus it, then feeds the text
	// through the private XCTest event-synthesis pipeline — this bypasses
	// the iOS input listener that the autofill QuickType bar hooks to
	// intercept keystrokes on SecureField(.newPassword) inputs. When paste
	// is false the driver types into the currently focused element via
	// typeText. id is required when paste is true.
	InputText(ctx context.Context, bundleID, id, text string, paste bool) error

	// EraseText erases the given number of characters from the focused
	// element.
	EraseText(ctx context.Context, bundleID string, characters int) error

	// Screenshot captures a PNG-encoded screenshot of the active screen.
	Screenshot(ctx context.Context) ([]byte, error)
}
