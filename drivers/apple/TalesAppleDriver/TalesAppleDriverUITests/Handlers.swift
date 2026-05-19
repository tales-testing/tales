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
            if element.exists && element.isHittable {
                // SwiftUI Toggle reports as .switch but its bounding box
                // includes the label on the left, which may carry
                // hit-testable children (Link, Button) that absorb a center
                // tap. Tap the right portion to land on the UISwitch
                // consistently.
                if element.elementType == .switch {
                    element.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
                } else {
                    element.tap()
                }

                return HTTPResponse.json(["ok": true])
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

            // Strategy 1: tap the QuickType "Paste" key that iOS adds to
            // the keyboard accessory bar when the pasteboard holds fresh
            // content. Works for most text fields including SecureField on
            // standard content types, and is locale-aware via the label
            // list below.
            if tapPasteCandidate(in: app.keyboards.buttons) {
                return HTTPResponse.json(["ok": true])
            }

            // Strategy 2: long-press the field to surface the system edit
            // menu, then tap Paste. This catches the case where QuickType
            // is suppressed (e.g. when the Strong Password sheet has
            // claimed the accessory view).
            element.press(forDuration: 1.0)
            if tapPasteCandidate(in: app.menuItems) {
                return HTTPResponse.json(["ok": true])
            }

            // Strategy 3: type via the soft keyboard as a best-effort
            // fallback. The autofill banner may still eat the first
            // keystrokes on a second focus, but at least the first focus
            // of the session lands characters in the field.
            app.typeText(text)

            return HTTPResponse.json(["ok": true])
        }

        app.typeText(text)

        return HTTPResponse.json(["ok": true])
    }

    /// Locales covered by iOS contextual / QuickType paste actions.
    private static let pasteLabels = [
        "Paste", "Coller", "Pegar", "Einfügen", "Incolla", "Inserir",
        "Vložit", "Beillesztés", "Plak", "Wklej",
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
