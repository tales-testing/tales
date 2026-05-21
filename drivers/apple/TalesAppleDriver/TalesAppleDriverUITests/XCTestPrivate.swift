import Foundation
import UIKit

/// Swift wrappers around the private XCTest event-synthesis pipeline:
/// `XCPointerEventPath`, `XCSynthesizedEventRecord` and the daemon proxy
/// exposed by `XCTRunnerDaemonSession`. Hitting these APIs from a UI test
/// bundle is the only way to feed text into a SecureField(.newPassword)
/// without losing characters to iOS's strong-password QuickType banner —
/// the high-level `XCUIApplication.typeText` / `XCUIElement.typeText`
/// paths run through the input listener that the banner interferes with.
///
/// All entry points are obtained via Objective-C runtime lookup so the
/// project does not need ObjC bridging headers for the private classes.
/// The selectors used here are documented across Maestro, Detox and
/// WebDriverAgent — they have been stable since Xcode 12.
///
/// Every class / selector lookup is guarded: if a future Xcode or iOS
/// runtime renames or removes one of these private symbols the wrappers
/// throw `XCTestPrivateError` instead of force-unwrapping nil and
/// crashing the process, so the driver's HTTP server stays alive and the
/// handler can return a structured 500 to Tales.

/// Raised when a private XCTest class or runtime symbol is unavailable.
enum XCTestPrivateError: LocalizedError {
    case classUnavailable(String)

    var errorDescription: String? {
        switch self {
        case .classUnavailable(let symbol):
            return "private XCTest symbol \(symbol) is unavailable on this runtime"
        }
    }
}

struct PointerEventPath {
    static func pathForTextInput(offset: TimeInterval = 0) throws -> Self {
        // initForTextInput is available since Xcode 10.2.
        guard let cls = objc_lookUpClass("XCPointerEventPath"),
              let alloced = cls.alloc() as? NSObject else {
            throw XCTestPrivateError.classUnavailable("XCPointerEventPath")
        }

        let selector = NSSelectorFromString("initForTextInput")
        let imp = alloced.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector) -> NSObject
        let method = unsafeBitCast(imp, to: Method.self)
        let path = method(alloced, selector)

        return Self(path: path, offset: offset)
    }

    /// Creates a touch event path with the finger pressed down at `point`.
    /// `initForTouchAtPoint:offset:` is available since Xcode 10.2.
    static func pathForTouch(at point: CGPoint, offset: TimeInterval = 0) throws -> Self {
        guard let cls = objc_lookUpClass("XCPointerEventPath"),
              let alloced = cls.alloc() as? NSObject else {
            throw XCTestPrivateError.classUnavailable("XCPointerEventPath")
        }

        let selector = NSSelectorFromString("initForTouchAtPoint:offset:")
        let imp = alloced.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, CGPoint, TimeInterval) -> NSObject
        let method = unsafeBitCast(imp, to: Method.self)
        let path = method(alloced, selector, point, offset)

        return Self(path: path, offset: offset)
    }

    let path: NSObject
    var offset: TimeInterval

    private init(path: NSObject, offset: TimeInterval) {
        self.path = path
        self.offset = offset
    }

    /// Appends a `typeText:atOffset:typingSpeed:shouldRedact:` event to
    /// the path. `typingSpeed` is the maximum frequency in chars per
    /// second the daemon will dispatch — 1 is very slow (used to warm
    /// up the input listener), 30 is the comfortable default for the
    /// bulk of the string.
    mutating func type(text: String, typingSpeed: Int, shouldRedact: Bool = false) {
        let selector = NSSelectorFromString("typeText:atOffset:typingSpeed:shouldRedact:")
        let imp = path.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, NSString, TimeInterval, UInt64, Bool) -> Void
        let method = unsafeBitCast(imp, to: Method.self)
        method(path, selector, text as NSString, offset, UInt64(typingSpeed), shouldRedact)
    }

    /// Drags the finger to `point` at the current offset.
    mutating func move(to point: CGPoint) {
        let selector = NSSelectorFromString("moveToPoint:atOffset:")
        let imp = path.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, CGPoint, TimeInterval) -> Void
        let method = unsafeBitCast(imp, to: Method.self)
        method(path, selector, point, offset)
    }

    /// Lifts the finger up at the current offset, ending the touch.
    mutating func liftUp() {
        let selector = NSSelectorFromString("liftUpAtOffset:")
        let imp = path.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, TimeInterval) -> Void
        let method = unsafeBitCast(imp, to: Method.self)
        method(path, selector, offset)
    }
}

final class EventRecord {
    enum Style: String {
        case singleFinger = "Single-Finger Touch Action"
    }

    let record: NSObject

    init(orientation: UIInterfaceOrientation, style: Style = .singleFinger) throws {
        guard let cls = objc_lookUpClass("XCSynthesizedEventRecord"),
              let alloced = cls.alloc() as? NSObject else {
            throw XCTestPrivateError.classUnavailable("XCSynthesizedEventRecord")
        }

        let selector = NSSelectorFromString("initWithName:interfaceOrientation:")
        let imp = alloced.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, NSString, UIInterfaceOrientation) -> NSObject
        let method = unsafeBitCast(imp, to: Method.self)
        record = method(alloced, selector, style.rawValue as NSString, orientation)
    }

    @discardableResult
    func add(_ path: PointerEventPath) -> Self {
        let selector = NSSelectorFromString("addPointerEventPath:")
        let imp = record.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, NSObject) -> Void
        let method = unsafeBitCast(imp, to: Method.self)
        method(record, selector, path.path)

        return self
    }

    /// Default press-then-release window for a plain tap.
    static let defaultTapDuration: TimeInterval = 0.1

    /// Adds a single touch: finger down at `point`, lifted up after
    /// `touchUpAfter` seconds (a plain tap when nil, a long-press when a
    /// duration is supplied).
    @discardableResult
    func addTouch(at point: CGPoint, touchUpAfter: TimeInterval? = nil) throws -> Self {
        var path = try PointerEventPath.pathForTouch(at: point)
        path.offset += touchUpAfter ?? Self.defaultTapDuration
        path.liftUp()

        return add(path)
    }

    /// Adds a swipe: finger down at `start`, dragged to `end` over
    /// `duration` seconds, then lifted up.
    @discardableResult
    func addSwipe(start: CGPoint, end: CGPoint, duration: TimeInterval) throws -> Self {
        var path = try PointerEventPath.pathForTouch(at: start)
        path.offset += Self.defaultTapDuration
        path.move(to: end)
        path.offset += duration
        path.liftUp()

        return add(path)
    }
}

final class RunnerDaemonProxy {
    private let proxy: NSObject

    init() throws {
        guard let clazz: AnyClass = NSClassFromString("XCTRunnerDaemonSession") else {
            throw XCTestPrivateError.classUnavailable("XCTRunnerDaemonSession")
        }

        let selector = NSSelectorFromString("sharedSession")
        let imp = clazz.method(for: selector)
        typealias Method = @convention(c) (AnyClass, Selector) -> NSObject
        let method = unsafeBitCast(imp, to: Method.self)
        let session = method(clazz, selector)

        guard let daemonProxy = session
            .perform(NSSelectorFromString("daemonProxy"))?
            .takeUnretainedValue() as? NSObject else {
            throw XCTestPrivateError.classUnavailable("XCTRunnerDaemonSession.daemonProxy")
        }

        proxy = daemonProxy
    }

    /// Synchronously dispatches an event record to testmanagerd and waits
    /// for the daemon completion. Implemented without Swift `async` so
    /// the surrounding HTTP handler stays synchronous; the completion
    /// fires on an XCTest-managed background queue so blocking the
    /// calling thread with a semaphore does not deadlock.
    func synthesizeSync(eventRecord: EventRecord, timeout: TimeInterval = 10) throws {
        let selector = NSSelectorFromString("_XCT_synthesizeEvent:completion:")
        let imp = proxy.method(for: selector)
        typealias Method = @convention(c) (NSObject, Selector, NSObject, @escaping (Error?) -> Void) -> Void
        let method = unsafeBitCast(imp, to: Method.self)

        let semaphore = DispatchSemaphore(value: 0)
        var callbackError: Error?
        method(proxy, selector, eventRecord.record) { error in
            callbackError = error
            semaphore.signal()
        }

        let deadline = DispatchTime.now() + .milliseconds(Int(timeout * 1000))
        if semaphore.wait(timeout: deadline) == .timedOut {
            throw NSError(domain: "TalesAppleDriver", code: -1, userInfo: [
                NSLocalizedDescriptionKey: "synthesize event timed out after \(timeout)s",
            ])
        }

        if let error = callbackError {
            throw error
        }
    }
}
