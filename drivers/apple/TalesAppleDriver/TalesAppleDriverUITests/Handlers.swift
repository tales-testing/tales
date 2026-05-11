import Foundation
import XCTest

/// TalesRouter wires HTTP routes to XCUITest actions.
final class TalesRouter {
    static let shared = TalesRouter()

    func dispatch(request: HTTPRequest) -> HTTPResponse {
        switch (request.method, request.path) {
        case ("GET", "/health"):
            return HTTPResponse.json(["status": "ok"])
        case ("GET", "/hierarchy"):
            return handleHierarchy(request: request)
        case ("POST", "/tap"):
            return handleTap(request: request)
        case ("POST", "/inputText"):
            return handleInputText(request: request)
        case ("POST", "/eraseText"):
            return handleEraseText(request: request)
        case ("GET", "/screenshot"):
            return handleScreenshot(request: request)
        case ("POST", "/launch"):
            return handleLaunch(request: request)
        case ("POST", "/terminate"):
            return handleTerminate(request: request)
        default:
            return HTTPResponse.error("route not found", status: 404)
        }
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
              let x = doubleField(payload["x"]),
              let y = doubleField(payload["y"]) else {
            return HTTPResponse.error("expected {x, y}", status: 400)
        }

        let app = XCUIApplication()
        let normalized = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0))
        let target = normalized.withOffset(CGVector(dx: x, dy: y))
        target.tap()

        return HTTPResponse.json(["ok": true])
    }

    private func handleInputText(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body), let text = payload["text"] as? String else {
            return HTTPResponse.error("expected {text}", status: 400)
        }

        let app = XCUIApplication()
        app.typeText(text)

        return HTTPResponse.json(["ok": true])
    }

    private func handleEraseText(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let count = payload["characters"] as? Int else {
            return HTTPResponse.error("expected {characters}", status: 400)
        }

        if count > 0 {
            let app = XCUIApplication()
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
