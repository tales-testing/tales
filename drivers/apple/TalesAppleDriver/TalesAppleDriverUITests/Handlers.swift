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

            // Focus the field, then paste via a simulated hardware-keyboard
            // Cmd+V. Hardware-keyboard events bypass the soft keyboard and
            // its QuickType / "Use Strong Password" overlay entirely, so
            // SecureField(.newPassword) receives every character of the
            // pasted string. Contextual paste menus are avoided because
            // SecureField commonly blocks them for security reasons and
            // their labels are locale-dependent.
            element.tap()
            UIPasteboard.general.string = text
            element.typeKey("v", modifierFlags: .command)

            return HTTPResponse.json(["ok": true])
        }

        app.typeText(text)

        return HTTPResponse.json(["ok": true])
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
