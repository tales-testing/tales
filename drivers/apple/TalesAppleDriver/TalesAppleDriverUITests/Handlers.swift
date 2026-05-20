import Foundation
import UIKit
import XCTest

/// TalesRouter wires HTTP routes to XCUITest actions.
final class TalesRouter {
    static let shared = TalesRouter()

    func dispatch(request: HTTPRequest) -> HTTPResponse {
        switch (request.method, request.path) {
        case ("GET", "/health"):
            return HTTPResponse.json(["status": "ok"])
        case ("GET", "/hierarchy"):
            return runOnMain { self.handleHierarchy(request: request) }
        case ("POST", "/tap"):
            return runOnMain { self.handleTap(request: request) }
        case ("POST", "/inputText"):
            return runOnMain { self.handleInputText(request: request) }
        case ("POST", "/eraseText"):
            return runOnMain { self.handleEraseText(request: request) }
        case ("GET", "/screenshot"):
            return runOnMain { self.handleScreenshot(request: request) }
        case ("POST", "/launch"):
            return runOnMain { self.handleLaunch(request: request) }
        case ("POST", "/terminate"):
            return runOnMain { self.handleTerminate(request: request) }
        default:
            return HTTPResponse.error("route not found", status: 404)
        }
    }

    private func runOnMain(_ work: @escaping () -> HTTPResponse) -> HTTPResponse {
        if Thread.isMainThread {
            return work()
        }

        return DispatchQueue.main.sync(execute: work)
    }

    private func handleHierarchy(request: HTTPRequest) -> HTTPResponse {
        guard let bundleID = request.query["bundleId"] else {
            return HTTPResponse.error("bundleId is required", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)

        // app.snapshot() can transiently fail while the UI is mid-transition
        // (XCTest reports the accessibility tree as briefly unavailable).
        // A bounded retry smooths that over instead of surfacing a 500 that
        // the Tales provider would otherwise treat as a hard error.
        var lastError: Error?
        for attempt in 0..<hierarchySnapshotAttempts {
            do {
                let snapshot = try app.snapshot()

                return HTTPResponse.json(HierarchyEncoder.encode(snapshot: snapshot))
            } catch {
                lastError = error

                if attempt < hierarchySnapshotAttempts - 1 {
                    Thread.sleep(forTimeInterval: hierarchySnapshotRetryDelay)
                }
            }
        }

        return HTTPResponse.error("snapshot failed: \(lastError.map { "\($0)" } ?? "unknown")", status: 500)
    }

    private let hierarchySnapshotAttempts = 3
    private let hierarchySnapshotRetryDelay: TimeInterval = 0.25

    private func handleTap(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let x = doubleField(payload["x"]),
              let y = doubleField(payload["y"]) else {
            return HTTPResponse.error("expected {bundleId, x, y}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        let id = (payload["id"] as? String) ?? ""

        // Prefer element-based tap when an accessibility id is provided.
        // The coordinate fallback below keeps backward compatibility with
        // callers that send only (x, y) and matches the legacy behavior for
        // elements that exist but are not yet hittable.
        if !id.isEmpty {
            let element = app.descendants(matching: .any).matching(identifier: id).firstMatch
            if element.exists {
                // If the soft keyboard is up and covers (or overlaps) the
                // target, dismiss it first so the tap can actually reach
                // the element. Mirrors the real iOS behavior where users
                // tap outside a text field to dismiss the keyboard before
                // interacting with the obscured controls below.
                if keyboardObscures(element, in: app) {
                    dismissKeyboardIfPresent(in: app)
                }

                // Wait for the element to be hittable. SwiftUI animates
                // scroll position when the keyboard appears or dismisses,
                // and a tap fired during that animation can land on the
                // element's stale frame and miss silently — exactly the
                // pattern observed on a sequence of Toggle taps right
                // after a SecureField input.
                if waitForHittable(element, timeout: 1.5) {
                    tapResolvedElement(element)

                    return HTTPResponse.json(["ok": true])
                }
            }
        }

        // Anchor on the target app because Xcode 26 removed XCUIScreen.coordinate.
        // The provider sends screen-space coordinates derived from that app's
        // snapshot, so the app origin keeps taps stable without external drivers.
        let origin = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0))
        let target = origin.withOffset(CGVector(dx: x, dy: y))
        target.tap()

        return HTTPResponse.json(["ok": true])
    }

    private func handleInputText(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let text = payload["text"] as? String else {
            return HTTPResponse.error("expected {bundleId, text}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        let id = (payload["id"] as? String) ?? ""
        let paste = (payload["paste"] as? Bool) ?? false

        if paste {
            guard !id.isEmpty else {
                return HTTPResponse.error("paste mode requires an element id", status: 400)
            }

            let element = app.descendants(matching: .any).matching(identifier: id).firstMatch
            guard element.exists else {
                return HTTPResponse.error("element \(id) not found", status: 404)
            }

            // SecureField(.newPassword) is the canonical case where every
            // high-level XCUITest typing API loses characters: the input
            // listener that autocorrection and the "Use Strong Password"
            // banner both hook eats multi-character bursts. Pasting via
            // UIPasteboard also fails because iOS often disables clipboard
            // paste on .newPassword for security.
            //
            // Tap to focus, wait for the keyboard, then go straight to
            // the synthesize path — it dispatches XCSynthesizedEventRecord
            // through the daemon proxy, fully bypassing the input listener.
            // No pasteboard fiddling, no contextual menu probing — both
            // were observed to either fail silently or paste partially on
            // iOS 26.
            element.tap()
            _ = app.keyboards.firstMatch.waitForExistence(timeout: 2.0)

            do {
                try typeWithEventSynthesis(text)
            } catch {
                return HTTPResponse.error("synthesize text failed: \(error.localizedDescription)", status: 500)
            }

            dismissKeyboardIfPresent(in: app)

            return HTTPResponse.json(["ok": true])
        }

        app.typeText(text)
        dismissKeyboardIfPresent(in: app)

        return HTTPResponse.json(["ok": true])
    }

    /// Types `text` by feeding XCSynthesizedEventRecord straight to the
    /// testmanagerd daemon via the private RunnerDaemonProxy. The
    /// high-level XCUIApplication.typeText path runs through the iOS
    /// input listener that interferes with autocorrection and the
    /// strong-password QuickType banner — under those conditions multi-
    /// character bursts deterministically lose 11-13 keystrokes on
    /// SecureField(.newPassword). Going through the event-synthesis
    /// pipeline bypasses that listener entirely.
    ///
    /// Mirrors Maestro's TextInputHelper strategy: dispatch the first
    /// character at typing speed 1 (very slow), wait 500ms for the input
    /// listener to settle around the new field, then dispatch the
    /// remainder at typing speed 30.
    private func typeWithEventSynthesis(_ text: String) throws {
        guard !text.isEmpty else { return }

        let chars = Array(text)
        let firstChar = String(chars[0])
        let remainder = chars.count > 1 ? String(chars[1...]) : ""

        try synthesizeText(firstChar, typingSpeed: 1)

        if !remainder.isEmpty {
            Thread.sleep(forTimeInterval: 0.5)
            try synthesizeText(remainder, typingSpeed: 30)
        }
    }

    /// One shot of event-record synthesis. Runs the sync daemon call on
    /// the global queue so the semaphore wait never blocks the same
    /// thread the completion would target. Errors propagate so the HTTP
    /// handler can return a real 500 to Tales instead of pretending the
    /// input succeeded.
    private func synthesizeText(_ text: String, typingSpeed: Int) throws {
        var caught: Error?
        DispatchQueue.global(qos: .userInitiated).sync {
            do {
                var path = PointerEventPath.pathForTextInput()
                path.type(text: text, typingSpeed: typingSpeed)

                let orientation = UIInterfaceOrientation.portrait
                let record = EventRecord(orientation: orientation)
                record.add(path)

                try RunnerDaemonProxy().synthesizeSync(eventRecord: record)
            } catch {
                caught = error
            }
        }

        if let caught {
            throw caught
        }
    }

    /// Routes a tap to the most specific affordance inside an element.
    /// SwiftUI Toggle exposes itself as `.switch` but contains a nested
    /// `.switch` child for the actual UISwitch when the label embeds
    /// interactive views (Link, Button, etc.). Targeting the nested
    /// switch sidesteps every hit-test ambiguity caused by labels,
    /// wrappers (CardView, padding), or custom toggle styles. The
    /// right-edge offset only kicks in when no nested switch exists.
    private func tapResolvedElement(_ element: XCUIElement) {
        if element.elementType == .switch {
            let innerSwitch = element.descendants(matching: .switch).firstMatch
            if innerSwitch.exists && innerSwitch.isHittable {
                innerSwitch.tap()

                return
            }

            element.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 0.5))
                .withOffset(CGVector(dx: -30, dy: 0))
                .tap()

            return
        }

        element.tap()
    }

    /// Labels iOS uses on the keyboard's return / done / continue key.
    private static let keyboardDismissLabels = [
        "Return", "Done", "Continue", "Send", "Search", "Go", "Next",
        "retour", "Terminé", "Continuer", "Envoyer", "Rechercher", "Suivant",
        "Listo", "Fertig", "Fatto", "Pronto", "Hotovo",
    ]

    /// Dismisses the soft keyboard when present. Tries the Return / Done
    /// key first (multi-locale), then falls back to tapping the top of
    /// the screen which is safely above any iOS keyboard. Subsequent
    /// taps on controls that were obscured by the keyboard can then
    /// land on the actual element rather than the keyboard surface.
    private func dismissKeyboardIfPresent(in app: XCUIApplication) {
        let keyboard = app.keyboards.firstMatch
        guard keyboard.exists else { return }

        for label in TalesRouter.keyboardDismissLabels {
            let btn = app.keyboards.buttons[label]
            if btn.exists && btn.isHittable {
                btn.tap()
                waitForNonExistence(keyboard, timeout: 1.0)

                return
            }
        }

        // Fallback: tap the very top of the screen (safely above any
        // soft keyboard) to resign the first responder.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.05)).tap()
        waitForNonExistence(keyboard, timeout: 1.0)
    }

    /// Reports whether the soft keyboard's frame overlaps the element,
    /// meaning a tap on the element's natural hit point would land on
    /// the keyboard instead.
    private func keyboardObscures(_ element: XCUIElement, in app: XCUIApplication) -> Bool {
        let keyboard = app.keyboards.firstMatch
        guard keyboard.exists else { return false }

        return element.frame.intersects(keyboard.frame)
    }

    /// Polls `isHittable` until true or until the timeout elapses. Used
    /// to give SwiftUI scroll / keyboard transitions time to settle
    /// before tapping — XCUITest snapshots may otherwise carry a stale
    /// frame and the tap misses the visible affordance.
    private func waitForHittable(_ element: XCUIElement, timeout: TimeInterval) -> Bool {
        if element.isHittable {
            return true
        }

        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if element.isHittable {
                return true
            }

            Thread.sleep(forTimeInterval: 0.05)
        }

        return element.isHittable
    }

    /// Polls `exists` until false or until the timeout elapses. Used to
    /// confirm the soft keyboard has finished dismissing before the
    /// next action runs.
    private func waitForNonExistence(_ element: XCUIElement, timeout: TimeInterval) {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if !element.exists {
                return
            }

            Thread.sleep(forTimeInterval: 0.05)
        }
    }

    private func handleEraseText(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let count = payload["characters"] as? Int else {
            return HTTPResponse.error("expected {bundleId, characters}", status: 400)
        }

        if count > 0 {
            let app = XCUIApplication(bundleIdentifier: bundleID)
            let deleteKey = String(repeating: XCUIKeyboardKey.delete.rawValue, count: count)
            app.typeText(deleteKey)
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handleScreenshot(request: HTTPRequest) -> HTTPResponse {
        let screenshot = XCUIScreen.main.screenshot()
        return HTTPResponse.png(screenshot.pngRepresentation)
    }

    private func handleLaunch(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String else {
            return HTTPResponse.error("expected {bundleId}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        app.launch()
        return HTTPResponse.json(["ok": true])
    }

    private func handleTerminate(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String else {
            return HTTPResponse.error("expected {bundleId}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        app.terminate()
        return HTTPResponse.json(["ok": true])
    }

    private func jsonObject(_ data: Data) -> [String: Any]? {
        guard let raw = try? JSONSerialization.jsonObject(with: data, options: []) as? [String: Any] else {
            return nil
        }
        return raw
    }

    private func doubleField(_ value: Any?) -> Double? {
        switch value {
        case let v as Double: return v
        case let v as Int: return Double(v)
        case let v as NSNumber: return v.doubleValue
        default: return nil
        }
    }
}
