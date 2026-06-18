import Foundation
import UIKit
import XCTest

/// TalesRouter wires HTTP routes to XCUITest actions.
final class TalesRouter {
    static let shared = TalesRouter()

    /// Snapshots run on this dedicated queue, never on the HTTP server queue
    /// or the runner main thread, so a snapshot that blocks (the app's own
    /// main thread is busy doing a refresh, so `app.snapshot()` cannot be
    /// served) can be bounded by a timeout instead of wedging the whole
    /// driver. Mirrors the background-queue + semaphore pattern the event
    /// synthesis path already uses to avoid deadlocking the caller.
    private let snapshotQueue = DispatchQueue(label: "tales.driver.snapshot")
    private let snapshotStateLock = NSLock()
    private var snapshotInFlight = false

    /// Upper bound on how long a single `/hierarchy` request waits for a
    /// snapshot before returning a retryable 503. A healthy snapshot takes
    /// tens of milliseconds; anything near this means the app under test is
    /// momentarily unresponsive (mid-transition / blocking its main thread),
    /// and the Tales provider's poll loop simply retries.
    private let snapshotTimeout: TimeInterval = 8

    func dispatch(request: HTTPRequest) -> HTTPResponse {
        // Skip /health: it is polled once a second by Tales' background
        // health-checker and would otherwise drown the driver log under
        // useless lines. Every state-changing route is logged so that
        // when XCTest crashes mid-scenario, the very last
        // `[tales-driver] request:` line is the request that triggered
        // the death. Pair with the response-side timing log below; the
        // gap between the two is the request that XCTest could not
        // complete before tearing down.
        let logRoute = !(request.method == "GET" && request.path == "/health")
        let started = Date()

        if logRoute {
            NSLog("[tales-driver] request: \(request.method) \(request.path)")
        }

        let response: HTTPResponse
        switch (request.method, request.path) {
        case ("GET", "/health"):
            response = HTTPResponse.json(["status": "ok"])
        case ("GET", "/hierarchy"):
            response = self.handleHierarchy(request: request)
        case ("POST", "/tap"):
            response = runOnMain { self.handleTap(request: request) }
        case ("POST", "/swipe"):
            response = runOnMain { self.handleSwipe(request: request) }
        case ("POST", "/longPress"):
            response = runOnMain { self.handleLongPress(request: request) }
        case ("POST", "/doubleTap"):
            response = runOnMain { self.handleDoubleTap(request: request) }
        case ("POST", "/pressKey"):
            response = runOnMain { self.handlePressKey(request: request) }
        case ("POST", "/pressButton"):
            response = runOnMain { self.handlePressButton(request: request) }
        case ("POST", "/orientation"):
            response = runOnMain { self.handleSetOrientation(request: request) }
        case ("POST", "/inputText"):
            response = runOnMain { self.handleInputText(request: request) }
        case ("POST", "/eraseText"):
            response = runOnMain { self.handleEraseText(request: request) }
        case ("POST", "/dismissKeyboard"):
            response = runOnMain { self.handleDismissKeyboard(request: request) }
        case ("GET", "/screenshot"):
            response = runOnMain { self.handleScreenshot(request: request) }
        case ("POST", "/launch"):
            response = runOnMain { self.handleLaunch(request: request) }
        case ("POST", "/terminate"):
            response = runOnMain { self.handleTerminate(request: request) }
        default:
            response = HTTPResponse.error("route not found", status: 404)
        }

        if logRoute {
            let elapsedMs = Int(Date().timeIntervalSince(started) * 1000)
            NSLog("[tales-driver] response: \(request.method) \(request.path) status=\(response.status) elapsed=\(elapsedMs)ms")
        }

        return response
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

        // Single-flight: never run two snapshots at once, and never pile a new
        // one behind a stuck one. If a snapshot is already running, tell the
        // provider to retry — far cheaper than queueing more work onto an app
        // that is momentarily unresponsive.
        snapshotStateLock.lock()
        if snapshotInFlight {
            snapshotStateLock.unlock()

            return HTTPResponse.error("snapshot busy; retry", status: 503)
        }

        snapshotInFlight = true
        snapshotStateLock.unlock()

        let semaphore = DispatchSemaphore(value: 0)
        var captured: HTTPResponse?

        // Run the snapshot off the HTTP server queue (and off the runner main
        // thread) so the wait below can time out while the snapshot keeps
        // running; clearing the in-flight flag is what lets a later poll
        // succeed once the app becomes responsive again.
        snapshotQueue.async { [weak self] in
            let response = self?.captureHierarchy(bundleID: bundleID)
                ?? HTTPResponse.error("router released", status: 500)

            self?.snapshotStateLock.lock()
            self?.snapshotInFlight = false
            self?.snapshotStateLock.unlock()

            captured = response
            semaphore.signal()
        }

        if semaphore.wait(timeout: .now() + snapshotTimeout) == .timedOut {
            // The snapshot is still running on snapshotQueue (the app's main
            // thread is busy). Do not block the HTTP server any longer; return
            // a retryable status. snapshotInFlight stays true until the stuck
            // snapshot finishes, so further /hierarchy calls get 503 and the
            // provider keeps polling — it recovers the moment the app frees up.
            return HTTPResponse.error("snapshot timed out after \(Int(snapshotTimeout))s; retry", status: 503)
        }

        return captured ?? HTTPResponse.error("snapshot produced no response", status: 500)
    }

    /// Takes the accessibility snapshot and encodes it. Always runs on
    /// `snapshotQueue`. A failure is reported as a retryable 503 (not 500) so
    /// a transient "main thread busy" / mid-transition error makes the provider
    /// poll again instead of treating the step as a hard failure.
    ///
    /// The snapshot is scoped to the app's first window
    /// (`app.windows.firstMatch.snapshot()`) rather than the full application
    /// (`app.snapshot()`). The application snapshot pulls in every connected
    /// accessory the runner can reach, including the iOS keyboard daemon
    /// (a separate process) whose accessibility tree on a focused SwiftUI
    /// TextField is enormous: predictive bar, every key, modifiers,
    /// hardware-key passthroughs. On a tall SwiftUI form with the keyboard
    /// up, that subtree alone can push a single `XCUIElement.snapshot()`
    /// past 8s and trip the bounded snapshot guard — exactly the stall
    /// reported on iOS 26.5 `BusinessOnboardingView`. Scoping to the
    /// window stays inside the app's own process and avoids the keyboard
    /// daemon tree entirely. Modal sheets and SwiftUI `.sheet`-presented
    /// content all live inside that same window, so locators on them
    /// keep resolving. UIAlertController-style alerts also live under
    /// the same window via the presentation hierarchy.
    private func captureHierarchy(bundleID: String) -> HTTPResponse {
        let app = XCUIApplication(bundleIdentifier: bundleID)

        // app.snapshot() can transiently fail while the UI is mid-transition
        // (XCTest reports the accessibility tree as briefly unavailable).
        // A bounded retry smooths that over instead of surfacing an error that
        // the Tales provider would otherwise treat as a hard failure.
        var lastError: Error?
        for attempt in 0..<hierarchySnapshotAttempts {
            do {
                let root = HierarchyEncoder.encode(snapshot: try captureRootSnapshot(app: app))

                return HTTPResponse.json(root)
            } catch {
                lastError = error

                if attempt < hierarchySnapshotAttempts - 1 {
                    Thread.sleep(forTimeInterval: hierarchySnapshotRetryDelay)
                }
            }
        }

        return HTTPResponse.error("snapshot failed: \(lastError.map { "\($0)" } ?? "unknown")", status: 503)
    }

    /// Returns the snapshot the provider should walk to find elements.
    /// Prefers a window-scoped snapshot to skip the keyboard daemon tree
    /// (see captureHierarchy). Falls back to the full-application snapshot
    /// when no window is exposed yet (e.g. between `terminate` and the
    /// next `launch`, or while the app is mid-launch and the runner has
    /// not attached a window to it yet).
    private func captureRootSnapshot(app: XCUIApplication) throws -> XCUIElementSnapshot {
        let window = app.windows.firstMatch
        if window.exists {
            return try window.snapshot()
        }

        return try app.snapshot()
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
        let label = (payload["label"] as? String) ?? ""

        // Prefer label-based resolution when set — iOS system controllers
        // (PHPickerViewController, UIDocumentPickerViewController, share
        // sheet, mail composer, ...) expose `accessibilityLabel` but leave
        // `accessibilityIdentifier` empty, which the id path below cannot
        // reach. NSPredicate `label == %@` mirrors the XCUITest native
        // matcher; firstMatch is implicit and gracefully accepts cells that
        // share a label across system sheets.
        if !label.isEmpty {
            let element = app.descendants(matching: .any)
                .matching(NSPredicate(format: "label == %@", label))
                .firstMatch
            if element.exists {
                if keyboardObscures(element, in: app) {
                    dismissKeyboardIfPresent(in: app)
                }

                if waitForHittable(element, timeout: 1.5) {
                    tapResolvedElement(element)

                    return HTTPResponse.json(["ok": true])
                }
            }
        }

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

    private func handleSwipe(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let startX = doubleField(payload["startX"]),
              let startY = doubleField(payload["startY"]),
              let endX = doubleField(payload["endX"]),
              let endY = doubleField(payload["endY"]) else {
            return HTTPResponse.error("expected {startX, startY, endX, endY}", status: 400)
        }

        let duration = doubleField(payload["duration"]) ?? 0.3

        // Swipes go straight through the event-synthesis pipeline: it is
        // coordinate-driven (the provider computes start/end from the
        // element bounds) and bypasses the input listener entirely.
        do {
            try synthesizeSwipe(
                start: CGPoint(x: startX, y: startY),
                end: CGPoint(x: endX, y: endY),
                duration: duration
            )
        } catch {
            return HTTPResponse.error("swipe failed: \(error.localizedDescription)", status: 500)
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handleLongPress(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let x = doubleField(payload["x"]),
              let y = doubleField(payload["y"]) else {
            return HTTPResponse.error("expected {bundleId, x, y}", status: 400)
        }

        let id = (payload["id"] as? String) ?? ""
        let label = (payload["label"] as? String) ?? ""
        let duration = doubleField(payload["duration"]) ?? 1.0

        // Label-based long press: same rationale as handleTap.
        if !label.isEmpty {
            let app = XCUIApplication(bundleIdentifier: bundleID)
            let element = app.descendants(matching: .any)
                .matching(NSPredicate(format: "label == %@", label))
                .firstMatch
            if element.exists, waitForHittable(element, timeout: 1.5) {
                element.press(forDuration: duration)

                return HTTPResponse.json(["ok": true])
            }
        }

        // Element-based press when the id resolves to a hittable element —
        // precise and consistent with handleTap. Falls back to a
        // coordinate touch held for `duration` otherwise.
        if !id.isEmpty {
            let app = XCUIApplication(bundleIdentifier: bundleID)
            let element = app.descendants(matching: .any).matching(identifier: id).firstMatch
            if element.exists, waitForHittable(element, timeout: 1.5) {
                element.press(forDuration: duration)

                return HTTPResponse.json(["ok": true])
            }
        }

        do {
            try synthesizeTouch(at: CGPoint(x: x, y: y), touchUpAfter: duration)
        } catch {
            return HTTPResponse.error("long press failed: \(error.localizedDescription)", status: 500)
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handleDoubleTap(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let x = doubleField(payload["x"]),
              let y = doubleField(payload["y"]) else {
            return HTTPResponse.error("expected {bundleId, x, y}", status: 400)
        }

        let id = (payload["id"] as? String) ?? ""
        let label = (payload["label"] as? String) ?? ""

        // Label-based double tap: same rationale as handleTap.
        if !label.isEmpty {
            let app = XCUIApplication(bundleIdentifier: bundleID)
            let element = app.descendants(matching: .any)
                .matching(NSPredicate(format: "label == %@", label))
                .firstMatch
            if element.exists, waitForHittable(element, timeout: 1.5) {
                element.doubleTap()

                return HTTPResponse.json(["ok": true])
            }
        }

        if !id.isEmpty {
            let app = XCUIApplication(bundleIdentifier: bundleID)
            let element = app.descendants(matching: .any).matching(identifier: id).firstMatch
            if element.exists, waitForHittable(element, timeout: 1.5) {
                element.doubleTap()

                return HTTPResponse.json(["ok": true])
            }
        }

        do {
            try synthesizeTouch(at: CGPoint(x: x, y: y), touchUpAfter: nil)
            try synthesizeTouch(at: CGPoint(x: x, y: y), touchUpAfter: nil)
        } catch {
            return HTTPResponse.error("double tap failed: \(error.localizedDescription)", status: 500)
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handlePressKey(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let key = payload["key"] as? String else {
            return HTTPResponse.error("expected {key}", status: 400)
        }

        guard let keyChar = Self.keyboardKeys[key] else {
            return HTTPResponse.error("unsupported key \(key)", status: 400)
        }

        // For return / enter, prefer tapping the soft keyboard's submit
        // button (Return / Done / Send / ...) when a keyboard is up. The
        // text-input synthesis path with XCUIKeyboardKey.return is unsafe
        // while a SwiftUI TextField is the first responder on iOS 26.x:
        // it crashes the XCTest runner with no stack trace, killing the
        // driver socket and breaking the rest of the scenario (Tales bug
        // report: iOS 26.5 form snapshot stall + press_key crash). The
        // keyboard-button path is also semantically more correct: it
        // fires the field's `submitLabel` action and dismisses the
        // keyboard in one move, which is what callers actually want.
        if let bundleID = payload["bundleId"] as? String,
           Self.keyboardSubmitKeys.contains(key),
           tapKeyboardSubmitButton(bundleID: bundleID) {
            return HTTPResponse.json(["ok": true])
        }

        // A keyboard key is fed through the same text-input synthesis path
        // as typing — the XCUIKeyboardKey raw value is the character the
        // daemon expects.
        do {
            try synthesizeOnGlobalQueue { record in
                var path = try PointerEventPath.pathForTextInput()
                path.type(text: keyChar, typingSpeed: 30)
                record.add(path)
            }
        } catch {
            return HTTPResponse.error("press key failed: \(error.localizedDescription)", status: 500)
        }

        return HTTPResponse.json(["ok": true])
    }

    /// Taps the soft keyboard's submit / return button when one is up.
    /// Returns true when a tap was actually performed so the caller can
    /// skip the dangerous synth-path fallback. Mirrors
    /// dismissKeyboardIfPresent's multi-locale label sweep but does NOT
    /// wait for the keyboard to disappear afterwards — callers who want
    /// dismissal use /dismissKeyboard.
    private func tapKeyboardSubmitButton(bundleID: String) -> Bool {
        let app = XCUIApplication(bundleIdentifier: bundleID)
        guard app.keyboards.firstMatch.exists else { return false }

        for label in TalesRouter.keyboardDismissLabels {
            let btn = app.keyboards.buttons[label]
            if btn.exists && btn.isHittable {
                btn.tap()

                return true
            }
        }

        return false
    }

    /// Keys that should prefer the keyboard's submit button when a soft
    /// keyboard is up (see handlePressKey).
    private static let keyboardSubmitKeys: Set<String> = ["return", "enter"]

    private func handlePressButton(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let button = payload["button"] as? String else {
            return HTTPResponse.error("expected {button}", status: 400)
        }

        switch button {
        case "home":
            XCUIDevice.shared.press(.home)
        case "lock":
            // No public API for the lock button; the selector is stable.
            XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        default:
            return HTTPResponse.error("unsupported button \(button)", status: 400)
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handleSetOrientation(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let orientation = payload["orientation"] as? String else {
            return HTTPResponse.error("expected {orientation}", status: 400)
        }

        guard let deviceOrientation = Self.deviceOrientations[orientation] else {
            return HTTPResponse.error("unsupported orientation \(orientation)", status: 400)
        }

        XCUIDevice.shared.orientation = deviceOrientation

        return HTTPResponse.json(["ok": true])
    }

    /// Maps Tales key names to the XCUIKeyboardKey raw values the event
    /// synthesizer expects (the raw value is the key's character, not the
    /// enum case name).
    private static let keyboardKeys: [String: String] = [
        "return": XCUIKeyboardKey.return.rawValue,
        "enter": XCUIKeyboardKey.enter.rawValue,
        "tab": XCUIKeyboardKey.tab.rawValue,
        "space": XCUIKeyboardKey.space.rawValue,
        "escape": XCUIKeyboardKey.escape.rawValue,
        "delete": XCUIKeyboardKey.delete.rawValue,
    ]

    /// Maps Tales orientation names to UIDeviceOrientation.
    private static let deviceOrientations: [String: UIDeviceOrientation] = [
        "portrait": .portrait,
        "landscape_left": .landscapeLeft,
        "landscape_right": .landscapeRight,
        "upside_down": .portraitUpsideDown,
    ]

    /// Runs one swipe event record through the daemon on the global
    /// queue so the semaphore wait never blocks the completion thread.
    private func synthesizeSwipe(start: CGPoint, end: CGPoint, duration: TimeInterval) throws {
        try synthesizeOnGlobalQueue { record in
            try record.addSwipe(start: start, end: end, duration: duration)
        }
    }

    /// Runs one touch event record (tap or held long-press) through the
    /// daemon on the global queue.
    private func synthesizeTouch(at point: CGPoint, touchUpAfter: TimeInterval?) throws {
        try synthesizeOnGlobalQueue { record in
            try record.addTouch(at: point, touchUpAfter: touchUpAfter)
        }
    }

    private func synthesizeOnGlobalQueue(_ build: @escaping (EventRecord) throws -> Void) throws {
        var caught: Error?
        DispatchQueue.global(qos: .userInitiated).sync {
            do {
                let record = try EventRecord(orientation: .portrait)
                try build(record)
                try RunnerDaemonProxy().synthesizeSync(eventRecord: record)
            } catch {
                caught = error
            }
        }

        if let caught {
            throw caught
        }
    }

    private func handleInputText(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let text = payload["text"] as? String else {
            return HTTPResponse.error("expected {bundleId, text}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        let id = (payload["id"] as? String) ?? ""
        let label = (payload["label"] as? String) ?? ""
        let paste = (payload["paste"] as? Bool) ?? false

        if paste {
            guard !id.isEmpty || !label.isEmpty else {
                return HTTPResponse.error("paste mode requires an element id or label", status: 400)
            }

            // Label takes precedence over id, matching the other handlers.
            let element: XCUIElement
            if !label.isEmpty {
                element = app.descendants(matching: .any)
                    .matching(NSPredicate(format: "label == %@", label))
                    .firstMatch
            } else {
                element = app.descendants(matching: .any).matching(identifier: id).firstMatch
            }

            guard element.exists else {
                let locator = label.isEmpty ? id : label
                return HTTPResponse.error("element \(locator) not found", status: 404)
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

            // Verify-and-retry: after each synthesis pass, read the field
            // value back. SecureField exposes one bullet per stored
            // character, so a short read-back means iOS dropped keystrokes
            // mid-type (the "Use Strong Password" UI stealing focus on a
            // second .newPassword field, a deterministic 3/16-style
            // truncation). On a short result, clear the field and retry —
            // the autofill UI is already installed by then, so the retry
            // types against a stable field. Bounded so a genuinely
            // value-less field cannot loop forever.
            let expectedLength = text.count
            var landed = -1

            for attempt in 1...inputTextMaxAttempts {
                if attempt > 1 {
                    clearFocusedField(app, characters: max(landed, 0) + inputTextClearPadding)
                    element.tap()
                    Thread.sleep(forTimeInterval: 0.3)
                }

                do {
                    try typeWithEventSynthesis(text)
                } catch {
                    return HTTPResponse.error("synthesize text failed: \(error.localizedDescription)", status: 500)
                }

                landed = valueCharacterCount(element)
                let locatorLog = label.isEmpty ? "id=\(id)" : "label=\"\(label)\""
                NSLog("[tales-driver] inputText \(locatorLog) attempt=\(attempt) expected=\(expectedLength) landed=\(landed)")

                if landed < 0 || landed >= expectedLength {
                    break
                }
            }

            dismissKeyboardIfPresent(in: app)

            // landed < 0 means the field exposes no readable value, so the
            // result is genuinely unverifiable — report ok rather than a
            // false failure. A non-negative landed shorter than the input
            // means iOS dropped keystrokes through every retry; surface
            // that as a real error instead of silently claiming success.
            if landed >= 0 && landed < expectedLength {
                return HTTPResponse.error(
                    "input text truncated: \(landed) of \(expectedLength) characters landed after \(inputTextMaxAttempts) attempts",
                    status: 500
                )
            }

            return HTTPResponse.json(["ok": true])
        }

        app.typeText(text)
        dismissKeyboardIfPresent(in: app)

        return HTTPResponse.json(["ok": true])
    }

    /// Maximum synthesis attempts for one input_text in paste mode.
    private let inputTextMaxAttempts = 3
    /// Extra delete keys sent when clearing before a retry, to cover any
    /// characters the previous pass landed beyond the read-back count.
    private let inputTextClearPadding = 4

    /// Returns the number of characters currently stored in the element,
    /// or -1 when the element exposes no readable value. A SecureField
    /// reports one bullet per character, so the count is reliable for
    /// both secure and plain text fields.
    private func valueCharacterCount(_ element: XCUIElement) -> Int {
        guard element.exists, let value = element.value as? String else {
            return -1
        }

        return value.count
    }

    /// Clears the currently focused field by synthesizing `characters`
    /// delete keystrokes through the event pipeline (the same path used
    /// for typing, so it bypasses the input listener).
    private func clearFocusedField(_ app: XCUIApplication, characters: Int) {
        guard characters > 0 else { return }

        let deletes = String(repeating: XCUIKeyboardKey.delete.rawValue, count: characters)
        try? synthesizeText(deletes, typingSpeed: 30)
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
                var path = try PointerEventPath.pathForTextInput()
                path.type(text: text, typingSpeed: typingSpeed)

                let orientation = UIInterfaceOrientation.portrait
                let record = try EventRecord(orientation: orientation)
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

    /// Dismisses the soft keyboard via the existing dismissKeyboardIfPresent
    /// helper. Idempotent: returns ok even when no keyboard is up so
    /// scenarios can pre-emptively call dismiss_keyboard before a snapshot
    /// without having to reason about UI state.
    private func handleDismissKeyboard(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String else {
            return HTTPResponse.error("expected {bundleId}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        dismissKeyboardIfPresent(in: app)

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
