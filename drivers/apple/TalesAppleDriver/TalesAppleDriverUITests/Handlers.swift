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
        case ("POST", "/scrollTo"):
            response = runOnMain { self.handleScrollTo(request: request) }
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
        let label = locatorLabel(from: payload)

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
        let label = locatorLabel(from: payload)
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
        let label = locatorLabel(from: payload)

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
            // Name the platform and list what it does support: the DSL
            // accepts the union of both platforms' buttons (back and
            // recent_apps exist only on Android), so this is the message
            // an author of a shared scenario reads.
            return HTTPResponse.error(
                "button \"\(button)\" is not supported on ios (supported: home, lock)",
                status: 400
            )
        }

        return HTTPResponse.json(["ok": true])
    }

    private func handleSetOrientation(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let orientation = payload["orientation"] as? String else {
            return HTTPResponse.error("expected {orientation}", status: 400)
        }

        guard let deviceOrientation = Self.deviceOrientations[orientation] else {
            let supported = Self.deviceOrientations.keys.sorted().joined(separator: ", ")

            return HTTPResponse.error(
                "unsupported orientation \"\(orientation)\" (supported: \(supported))",
                status: 400
            )
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
        // The content to type arrives as "value", not "text": "text"
        // names the text *locator* on every route, and reusing it here
        // made an input_text action send its own content as the element
        // to search for. The DSL calls this field value, so does the wire.
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String,
              let text = payload["value"] as? String else {
            return HTTPResponse.error("expected {bundleId, value}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        let id = (payload["id"] as? String) ?? ""
        let label = locatorLabel(from: payload)
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
            //
            // Hard precondition: the synth path must NEVER run unless a
            // keyboard is actually up. If the tap fails to focus (the
            // element is offscreen, covered, or wrapped in a container
            // the tap landed on instead), the synth path raises
            // "Failed to synthesize event: Neither element nor any
            // descendant has keyboard focus" — an unrecoverable XCTest
            // API violation that tears down the test runner mid-scenario
            // and Tales then sees `connect: connection refused` on every
            // subsequent request. We bail with a 500 instead, so the
            // runner stays alive and the user gets an actionable error.
            if !focusElementForTextInput(element, in: app) {
                let locator = label.isEmpty ? "id=\(id)" : "label=\"\(label)\""
                return HTTPResponse.error(
                    "input text: \(locator) did not gain keyboard focus after tap+scroll. The element may be offscreen, covered, or non-focusable. Add a scroll_to action before input_text, or verify the locator targets a focusable text field.",
                    status: 500
                )
            }

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
                    // Re-tap to restore focus, and re-check the keyboard
                    // before entering the synth path. Same rationale as
                    // the pre-loop focus check: a retry that lost focus
                    // would otherwise drop us into the synth path with
                    // no first responder and tear the runner down.
                    if !focusElementForTextInput(element, in: app) {
                        break
                    }
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

        // Non-paste branch (regular TextField). Same XCTest API
        // violation pattern as paste mode: app.typeText() requires a
        // first responder, and crashes the runner with
        // `Failed to synthesize event: Neither element nor any
        // descendant has keyboard focus` when none is set. The
        // sealway BusinessOnboardingView repro hits this on offscreen
        // form fields where the Go-side focus tap missed. Guard the
        // synth path the same way as the paste branch: if a locator
        // is provided, resolve it, focus-and-scroll if needed, and
        // bail with 500 instead of entering app.typeText with no
        // first responder. When no locator is provided (the legacy
        // "type into whatever is focused" use case, e.g. after a
        // press_key brought up a search bar), keep the existing
        // app.typeText path so we do not regress that intent.
        if !id.isEmpty || !label.isEmpty {
            let element = resolveLocatorElement(in: app, id: id, label: label, text: "")
            guard element.exists else {
                let locator = label.isEmpty ? id : label
                return HTTPResponse.error("element \(locator) not found", status: 404)
            }

            if !focusElementForTextInput(element, in: app) {
                let locator = label.isEmpty ? "id=\(id)" : "label=\"\(label)\""
                return HTTPResponse.error(
                    "input text: \(locator) did not gain keyboard focus after tap+scroll. The element may be offscreen, covered, or non-focusable. Add a scroll_to action before input_text, or verify the locator targets a focusable text field.",
                    status: 500
                )
            }
        }

        app.typeText(text)
        dismissKeyboardIfPresent(in: app)

        return HTTPResponse.json(["ok": true])
    }

    /// Resolves an XCUIElement reference from the (id, label) pair,
    /// label-first so callers that pass both end up with the label
    /// matcher (same precedence as every other locator-aware
    /// handler). Returns a placeholder XCUIElement whose `.exists` is
    /// false when nothing matches; the caller is expected to guard on
    /// `.exists` before using it.
    /// Reads the label-shaped locator out of a request payload.
    ///
    /// `label` and `text` collapse to one value on iOS because an
    /// element's visible copy *is* its accessibility label here; see
    /// resolveLocatorElement for why that keeps the locator portable.
    /// `label` wins when both are present, though the parser makes that
    /// combination impossible.
    private func locatorLabel(from payload: [String: Any]) -> String {
        if let label = payload["label"] as? String, !label.isEmpty {
            return label
        }

        return (payload["text"] as? String) ?? ""
    }

    /// Resolves the element a request names.
    ///
    /// `text` matches on `label` here, the same predicate the `label`
    /// locator uses. That is not a shortcut: on iOS an element's visible
    /// copy *is* its accessibility label, so a button reading "Done"
    /// carries it as the label and exposes no separate text attribute.
    /// The Go-side resolver mirrors this by falling back to the label
    /// when a node has no text, which is what lets one `text = "Done"`
    /// locator reach the same control on iOS and on Android, where the
    /// caption arrives as text instead.
    private func resolveLocatorElement(in app: XCUIApplication, id: String, label: String, text: String) -> XCUIElement {
        let byLabel = !label.isEmpty ? label : text

        if !byLabel.isEmpty {
            return app.descendants(matching: .any)
                .matching(NSPredicate(format: "label == %@", byLabel))
                .firstMatch
        }

        return app.descendants(matching: .any).matching(identifier: id).firstMatch
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

    /// Taps `element` to focus it, then waits for the keyboard. If the
    /// keyboard does not appear, attempts one scroll-into-view followed by
    /// a single retap. Returns true when a keyboard is up after the
    /// sequence, false otherwise. The caller MUST bail with a 500 when
    /// this returns false: entering the synth path without a focused
    /// element triggers an XCTest API violation that tears down the
    /// runner (see handleInputText).
    private func focusElementForTextInput(_ element: XCUIElement, in app: XCUIApplication) -> Bool {
        element.tap()

        if app.keyboards.firstMatch.waitForExistence(timeout: 2.0) {
            return true
        }

        // No keyboard means the tap did not focus a text input. The
        // most common cause on a tall SwiftUI form is the element being
        // offscreen: XCUITest reports it as `exists` (it is in the a11y
        // tree) and isHittable can even be true for the bounding rect,
        // but a tap at its frame center misses because the actual input
        // affordance is scrolled past the viewport. One scroll-into-
        // view + retap is enough to recover in practice; if the second
        // attempt still fails the element is genuinely non-focusable
        // (covered by a modal, disabled, ...) and the caller surfaces
        // an actionable error to the user.
        _ = scrollElementIntoView(element, in: app)
        element.tap()

        return app.keyboards.firstMatch.waitForExistence(timeout: 2.0)
    }

    /// Scrolls `element` into the viewport by dragging the app's window
    /// in the direction that brings the element closer to the safe
    /// center area. Returns true when a drag was actually performed,
    /// false when no scroll was needed (element already in safe area)
    /// or when the geometry could not be resolved.
    ///
    /// Implementation detail: we drag the window via XCUICoordinate
    /// rather than calling swipeUp/swipeDown on a `XCUIElement.scrollView`
    /// because SwiftUI `Form` does NOT expose an XCUIElement of type
    /// scrollView, so the typed swipe helpers no-op. The window-level
    /// drag works for every scrollable container (Form, ScrollView,
    /// List, etc.) because UIKit routes the touch sequence to whatever
    /// scrolls under the coordinate.
    @discardableResult
    private func scrollElementIntoView(_ element: XCUIElement, in app: XCUIApplication) -> Bool {
        let elementFrame = element.frame
        guard !elementFrame.isEmpty else { return false }

        let window = app.windows.firstMatch
        guard window.exists else { return false }

        let windowFrame = window.frame
        guard !windowFrame.isEmpty else { return false }

        // Leave generous safe-area margins: a soft keyboard occupies
        // roughly the bottom 40% of an iPhone screen on portrait, so
        // anything in the bottom ~330pt risks being covered when typing.
        // The top inset keeps the element clear of the status / nav bar.
        let safeTopInset: CGFloat = 100
        let safeBottomInset: CGFloat = 60
        let safeBottomY = windowFrame.maxY - safeBottomInset

        var deltaY: CGFloat = 0

        if elementFrame.maxY > safeBottomY {
            // Element is below the safe bottom: scroll content UP so
            // the element rises into view. Drag direction is up
            // (negative dy), distance = how far below the safe area.
            deltaY = -(elementFrame.maxY - safeBottomY + 24)
        } else if elementFrame.minY < safeTopInset {
            // Element is above the safe top: scroll content DOWN so
            // the element drops into view.
            deltaY = safeTopInset - elementFrame.minY + 24
        }

        if deltaY == 0 {
            return false
        }

        // Drag from the window's vertical mid-point by `deltaY` pixels.
        // Coordinates are normalized; we apply absolute pixel offsets
        // via withOffset so the drag is independent of device size.
        let startY = windowFrame.midY
        let endY = startY + deltaY
        let centerX = windowFrame.midX

        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0))
            .withOffset(CGVector(dx: centerX, dy: startY))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0))
            .withOffset(CGVector(dx: centerX, dy: endY))

        // Short hold before drag, then drag. The hold gives SwiftUI
        // gesture recognizers time to attach to the scroll view; a
        // 0-duration press behaves like a flick and can cancel.
        start.press(forDuration: 0.05, thenDragTo: end)

        // Let the scroll settle before the caller queries the element
        // frame again or re-taps.
        Thread.sleep(forTimeInterval: 0.2)

        return true
    }

    /// Resolves an element by label-first then identifier, and scrolls
    /// it into the viewport. Idempotent: a no-op when the element is
    /// already in the safe area. Returns 404 when no element matches
    /// the locator.
    private func handleScrollTo(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String else {
            return HTTPResponse.error("expected {bundleId}", status: 400)
        }

        let id = (payload["id"] as? String) ?? ""
        let label = locatorLabel(from: payload)

        guard !id.isEmpty || !label.isEmpty else {
            return HTTPResponse.error("scroll_to requires an element id or label", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
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

        _ = scrollElementIntoView(element, in: app)

        return HTTPResponse.json(["ok": true])
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

    /// Launches the app and refuses to report success for a launch that
    /// did not happen.
    ///
    /// `XCUIApplication.launch()` does not throw when the simulator
    /// declines to open the app: it records XCTest failures ("The request
    /// to open ... failed", "does not have a process ID", "has not loaded
    /// accessibility"), spends a minute waiting for accessibility on a
    /// process that never existed, captures a spindump, and returns
    /// normally. The driver used to answer 200 to that, so Tales polled a
    /// non-existent app until its action timeout and reported a missing
    /// element — a message pointing nowhere near the cause. Observed on a
    /// shared CI runner, where it cost a whole suite.
    ///
    /// `app.state` is the ground truth, so it is checked, and a launch
    /// that failed fast is retried once: the simulator refusing to open an
    /// app is typically transient. The retry is skipped when the first
    /// attempt was slow, because a second one would outlast the client's
    /// timeout and turn a reportable failure into an abandoned request,
    /// which is the desynchronization this whole path exists to avoid.
    private func handleLaunch(request: HTTPRequest) -> HTTPResponse {
        guard let payload = jsonObject(request.body),
              let bundleID = payload["bundleId"] as? String else {
            return HTTPResponse.error("expected {bundleId}", status: 400)
        }

        let app = XCUIApplication(bundleIdentifier: bundleID)
        let started = Date()
        var attempt = 0

        while true {
            app.launch()
            attempt += 1

            if waitForForeground(app: app) {
                return HTTPResponse.json(["ok": true])
            }

            let elapsed = Date().timeIntervalSince(started)
            let exhausted = attempt >= launchAttempts
            let tooSlowToRetry = elapsed >= launchRetryBudget

            NSLog("[tales-driver] launch attempt \(attempt) left \(bundleID) in state \(stateName(app.state)) after \(Int(elapsed))s")

            if exhausted || tooSlowToRetry {
                let reason = tooSlowToRetry && !exhausted ? "not retried, first attempt took \(Int(elapsed))s" : "\(attempt) attempt(s)"

                return HTTPResponse.error(
                    "app \(bundleID) did not reach the foreground (state: \(app.state.rawValue), \(reason)); "
                        + "the simulator declined to open it",
                    status: 500
                )
            }
        }
    }

    /// Polls `app.state` briefly after a launch returns.
    ///
    /// A successful launch is normally already in the foreground here; the
    /// grace period only covers a device slow enough to still be settling,
    /// so a working-but-slow launch is not reported as a failed one.
    private func waitForForeground(app: XCUIApplication) -> Bool {
        let deadline = Date().addingTimeInterval(launchForegroundGrace)

        repeat {
            if app.state == .runningForeground {
                return true
            }

            Thread.sleep(forTimeInterval: 0.25)
        } while Date() < deadline

        return app.state == .runningForeground
    }

    /// XCUIApplication.State is an ObjC enum; its rawValue alone in an
    /// error message would make the reader look up what 1 means.
    private func stateName(_ state: XCUIApplication.State) -> String {
        switch state {
        case .unknown: return "unknown"
        case .notRunning: return "notRunning"
        case .runningBackgroundSuspended: return "runningBackgroundSuspended"
        case .runningBackground: return "runningBackground"
        case .runningForeground: return "runningForeground"
        @unknown default: return "state(\(state.rawValue))"
        }
    }

    private let launchAttempts = 2
    private let launchForegroundGrace: TimeInterval = 5
    private let launchRetryBudget: TimeInterval = 60

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
