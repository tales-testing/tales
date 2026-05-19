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
        let snapshot: XCUIElementSnapshot
        do {
            snapshot = try app.snapshot()
        } catch {
            return HTTPResponse.error("snapshot failed: \(error)", status: 500)
        }

        let payload = HierarchyEncoder.encode(snapshot: snapshot)
        return HTTPResponse.json(payload)
    }

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

            // Pasting into SecureField(.newPassword) is fragile because iOS
            // installs a "Use Strong Password" QuickType overlay that
            // intercepts soft-keyboard keystrokes and Cmd+V from a
            // disconnected hardware keyboard is silently dropped. Stage the
            // pasteboard once, then walk a cascade of strategies that
            // covers the realistic input paths, ending on a typeText
            // fallback so the field never ends up completely empty.
            UIPasteboard.general.string = text
            element.tap()

            // Wait for the soft keyboard to be fully presented before
            // probing any strategy. On a second focus from a previously
            // focused field, iOS animates the keyboard transition and any
            // keystroke sent during that window is silently swallowed —
            // the classic stable "N chars out of M" truncation pattern.
            _ = app.keyboards.firstMatch.waitForExistence(timeout: 2.0)

            // Best-effort: if the iOS "Use Strong Password" sheet has
            // claimed the keyboard accessory, dismiss it so paste / type
            // strategies can interact with the field directly. Labels are
            // locale-dependent so this is intentionally a quick scan with
            // a short timeout — the cascade still works without it.
            dismissPasswordAssistant(in: app)

            // Strategy 1: tap the QuickType "Paste" key that iOS adds to
            // the keyboard accessory bar when the pasteboard holds fresh
            // content. Works for most text fields including SecureField on
            // standard content types, and is locale-aware via the label
            // list below.
            if tapPasteCandidate(in: app.keyboards.buttons) {
                dismissKeyboardIfPresent(in: app)

                return HTTPResponse.json(["ok": true])
            }

            // Strategy 2: long-press the field to surface the system edit
            // menu, then tap Paste. This catches the case where QuickType
            // is suppressed (e.g. when the Strong Password sheet has
            // claimed the accessory view).
            element.press(forDuration: 1.0)
            if tapPasteCandidate(in: app.menuItems) {
                dismissKeyboardIfPresent(in: app)

                return HTTPResponse.json(["ok": true])
            }

            // Strategy 3: type via the soft keyboard as a best-effort
            // fallback. The keyboard wait above already covered the focus
            // transition so the full string should land on the field.
            app.typeText(text)
            dismissKeyboardIfPresent(in: app)

            return HTTPResponse.json(["ok": true])
        }

        app.typeText(text)
        dismissKeyboardIfPresent(in: app)

        return HTTPResponse.json(["ok": true])
    }

    /// Locales covered by iOS contextual / QuickType paste actions.
    private static let pasteLabels = [
        "Paste", "Coller", "Pegar", "Einfügen", "Incolla", "Inserir",
        "Vložit", "Beillesztés", "Plak", "Wklej",
    ]

    /// Labels iOS uses on the "Use Strong Password" assistant sheet to
    /// let the user opt out and enter their own password. Tapping any of
    /// these dismisses the assistant and frees the keyboard accessory.
    private static let passwordAssistantDismissLabels = [
        "Choose My Own Password",
        "Saisir mon mot de passe",
        "Selbst auswählen",
        "Inserisci tu la password",
        "Escolher minha própria senha",
        "Elegir mi propia contraseña",
        "Not Now",
        "Pas maintenant",
        "Nicht jetzt",
        "Non ora",
        "Agora não",
        "Ahora no",
    ]

    /// Walks `pasteLabels` against an XCUIElementQuery (keyboards.buttons
    /// or menuItems) and taps the first hittable match. Returns false if
    /// none surfaces within a short settle window.
    private func tapPasteCandidate(in query: XCUIElementQuery) -> Bool {
        // Wait briefly on the canonical English label so the keyboard /
        // menu has time to render before we scan the locale fallbacks.
        _ = query["Paste"].waitForExistence(timeout: 1.0)

        for label in TalesRouter.pasteLabels {
            let item = query[label]
            if item.exists && item.isHittable {
                item.tap()
                return true
            }
        }

        return false
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

    private func dismissPasswordAssistant(in app: XCUIApplication) {
        for label in TalesRouter.passwordAssistantDismissLabels {
            let btn = app.buttons[label]
            if btn.exists && btn.isHittable {
                btn.tap()

                return
            }
        }
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
