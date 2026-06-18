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

	// Tap performs a single tap. The driver resolves an element in the
	// following priority: label (matched by accessibilityLabel via XCUITest
	// NSPredicate firstMatch), id (matched by accessibilityIdentifier
	// firstMatch), or finally the screen-space (x,y) fallback. label and
	// id may both be empty when matching by coordinates only.
	Tap(ctx context.Context, bundleID, id, label string, x, y float64) error

	// Swipe drags one finger from (startX,startY) to (endX,endY) over the
	// given duration. Coordinates are screen-space; the provider computes
	// them from the target element bounds (or the screen) so this also
	// backs the scroll action.
	Swipe(ctx context.Context, bundleID string, startX, startY, endX, endY, duration float64) error

	// LongPress presses and holds at the resolved element for the given
	// duration in seconds. Label takes precedence over id, with (x,y) as
	// the coordinate fallback (cf. Tap).
	LongPress(ctx context.Context, bundleID, id, label string, x, y, duration float64) error

	// DoubleTap performs two quick taps at the resolved element. Label
	// takes precedence over id, with (x,y) as the coordinate fallback
	// (cf. Tap).
	DoubleTap(ctx context.Context, bundleID, id, label string, x, y float64) error

	// PressKey presses a hardware keyboard key by name (return, enter,
	// tab, space, escape, delete).
	PressKey(ctx context.Context, bundleID, key string) error

	// PressButton presses a device button by name (home, lock).
	PressButton(ctx context.Context, bundleID, button string) error

	// SetOrientation changes the device orientation (portrait,
	// landscape_left, landscape_right, upside_down).
	SetOrientation(ctx context.Context, orientation string) error

	// InputText sets text on the resolved element. Label takes precedence
	// over id (cf. Tap). When paste is true the driver taps the element to
	// focus it, then feeds the text through the private XCTest
	// event-synthesis pipeline — this bypasses the iOS input listener that
	// the autofill QuickType bar hooks to intercept keystrokes on
	// SecureField(.newPassword) inputs. When paste is false the driver
	// types into the currently focused element via typeText. A locator
	// (label or id) is required when paste is true.
	InputText(ctx context.Context, bundleID, id, label, text string, paste bool) error

	// EraseText erases the given number of characters from the focused
	// element.
	EraseText(ctx context.Context, bundleID string, characters int) error

	// DismissKeyboard dismisses the soft keyboard if one is currently up.
	// Idempotent: returns nil whether or not a keyboard was actually
	// dismissed, so scenarios can call it before a snapshot-heavy step
	// without having to query UI state first.
	DismissKeyboard(ctx context.Context, bundleID string) error

	// ScrollTo scrolls the element identified by id or label into the
	// viewport so a follow-up tap / input_text can hit it. label takes
	// precedence over id, matching every other element-targeted call.
	// Idempotent: a no-op when the element is already in the safe area.
	// Returns an error when no element matches the locator (the driver
	// surfaces a 404). Internally drags the app window via XCUICoordinate
	// because SwiftUI Form does not expose an XCUIElement scrollView and
	// the typed swipeUp/swipeDown helpers would no-op there.
	ScrollTo(ctx context.Context, bundleID, id, label string) error

	// Screenshot captures a PNG-encoded screenshot of the active screen.
	Screenshot(ctx context.Context) ([]byte, error)

	// Launch (re)launches the app under test through the driver so XCTest
	// owns the process. Routing the launch through XCUIApplication.launch()
	// (rather than an out-of-band simctl launch) re-establishes XCTest's
	// automation session with the freshly launched process; otherwise a
	// later snapshot would query a stale, terminated process and hang.
	Launch(ctx context.Context, bundleID string) error

	// Terminate terminates the app under test through the driver
	// (XCUIApplication.terminate()), keeping XCTest's process model in sync
	// with the app lifecycle across scenarios that reuse the same session.
	Terminate(ctx context.Context, bundleID string) error
}
