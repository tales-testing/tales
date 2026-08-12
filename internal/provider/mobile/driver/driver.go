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

// Locator names the element a command targets.
//
// At most one field is set on a well-formed request; the parser enforces
// the exclusivity. Drivers resolve them in the order Label, Text, ID,
// matching the Go-side resolver so both ends agree on which element a
// request names. An empty Locator means "use the coordinates", which is
// how device-level and purely positional commands travel.
type Locator struct {
	// ID is the accessibility identifier on iOS, the resource id (or
	// Compose test tag) on Android. Drivers accept the short form as
	// well as Android's fully qualified `pkg:id/name`.
	ID string
	// Label is the accessibility label on iOS, the content description
	// on Android.
	Label string
	// Text is the element's visible text, falling back to its label
	// when it has none. The locator of last resort: visible copy
	// changes far more often than an identifier, but it is the only
	// handle on UI that ships no identifiers at all.
	Text string
}

// IsEmpty reports whether no locator was given, so the command must fall
// back to coordinates.
func (l Locator) IsEmpty() bool {
	return l.ID == "" && l.Label == "" && l.Text == ""
}

// Driver abstracts the low-level mobile UI commands.
type Driver interface {
	// Health pings the driver and returns nil when the driver is ready to
	// accept commands.
	Health(ctx context.Context) error

	// Hierarchy returns the normalized view tree for the given bundle id.
	Hierarchy(ctx context.Context, bundleID string) (*tree.ViewNode, error)

	// Tap performs a single tap. The driver re-resolves the locator
	// against the live UI and falls back to the screen-space (x,y) the
	// provider computed when the locator is empty or no longer matches.
	// Re-resolving matters: between the snapshot those coordinates came
	// from and this call, the screen may have moved.
	Tap(ctx context.Context, bundleID string, locator Locator, x, y float64) error

	// Swipe drags one finger from (startX,startY) to (endX,endY) over the
	// given duration. Coordinates are screen-space; the provider computes
	// them from the target element bounds (or the screen) so this also
	// backs the scroll action.
	Swipe(ctx context.Context, bundleID string, startX, startY, endX, endY, duration float64) error

	// LongPress presses and holds at the resolved element for the given
	// duration in seconds (cf. Tap for locator resolution).
	LongPress(ctx context.Context, bundleID string, locator Locator, x, y, duration float64) error

	// DoubleTap performs two quick taps at the resolved element
	// (cf. Tap for locator resolution).
	DoubleTap(ctx context.Context, bundleID string, locator Locator, x, y float64) error

	// PressKey presses a hardware keyboard key by name (return, enter,
	// tab, space, escape, delete).
	PressKey(ctx context.Context, bundleID, key string) error

	// PressButton presses a device button by name (home, lock).
	PressButton(ctx context.Context, bundleID, button string) error

	// SetOrientation changes the device orientation (portrait,
	// landscape_left, landscape_right, upside_down).
	SetOrientation(ctx context.Context, orientation string) error

	// InputText sets text on the resolved element (cf. Tap). When paste is true the driver taps the element to
	// focus it, then feeds the text through the private XCTest
	// event-synthesis pipeline — this bypasses the iOS input listener that
	// the autofill QuickType bar hooks to intercept keystrokes on
	// SecureField(.newPassword) inputs. When paste is false the driver
	// types into the currently focused element via typeText. A locator
	// is required when paste is true.
	InputText(ctx context.Context, bundleID string, locator Locator, text string, paste bool) error

	// EraseText erases the given number of characters from the focused
	// element.
	EraseText(ctx context.Context, bundleID string, characters int) error

	// DismissKeyboard dismisses the soft keyboard if one is currently up.
	// Idempotent: returns nil whether or not a keyboard was actually
	// dismissed, so scenarios can call it before a snapshot-heavy step
	// without having to query UI state first.
	DismissKeyboard(ctx context.Context, bundleID string) error

	// ScrollTo scrolls the located element into the viewport so a follow-up tap / input_text can hit it. label takes
	// Idempotent: a no-op when the element is already in the safe area.
	// Returns an error when no element matches the locator (the driver
	// surfaces a 404). Internally drags the app window via XCUICoordinate
	// because SwiftUI Form does not expose an XCUIElement scrollView and
	// the typed swipeUp/swipeDown helpers would no-op there.
	ScrollTo(ctx context.Context, bundleID string, locator Locator) error

	// Screenshot captures a PNG-encoded screenshot of the active screen.
	Screenshot(ctx context.Context) ([]byte, error)

	// Launch (re)launches the app under test through the driver so XCTest
	// owns the process. Used by platforms whose driver owns the app
	// lifecycle; backends that can start the app from the host prefer
	// Activate (see mobile.HostAppLauncher).
	Launch(ctx context.Context, bundleID string) error

	// Activate brings an already-running app to the foreground and binds the
	// driver's automation session to it, without owning the launch.
	//
	// It is the counterpart to a host-side cold start: the session must be
	// re-bound after the app restarts, or a later snapshot queries a stale
	// process and hangs, but doing that through a full driver-side launch
	// makes the driver a casualty of every failed launch.
	Activate(ctx context.Context, bundleID string) error

	// Terminate terminates the app under test through the driver
	// (XCUIApplication.terminate()), keeping XCTest's process model in sync
	// with the app lifecycle across scenarios that reuse the same session.
	Terminate(ctx context.Context, bundleID string) error
}
