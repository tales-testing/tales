import Foundation
import ObjectiveC.runtime
import XCTest

/// Neutralizes XCTest's implicit "wait for app quiescence" step.
///
/// Before every snapshot and every interaction, XCUITest calls
/// `-[XCUIApplicationProcess waitForQuiescenceIncludingAnimationsIdle:]`,
/// which blocks until the target app's runloop *and* its animation engine
/// are simultaneously idle. An app with a continuous animation, a progress
/// spinner, an autofill banner sliding in, or a repeating timer never
/// reaches that state — the call then blocks until XCTest's internal
/// state timeout and surfaces to the Tales host as a `context deadline
/// exceeded` on `/hierarchy` or `/tap`.
///
/// Tales already does explicit, bounded polling at the provider level:
/// every mobile action re-fetches the hierarchy on an interval until its
/// own timeout. The driver therefore does not need — and is actively hurt
/// by — XCTest's open-ended implicit wait. The swizzle below replaces the
/// quiescence wait with a no-op so snapshots and element taps return
/// immediately; the provider's polling supplies the real "wait for the UI
/// to settle" semantics.
///
/// Implemented as a pure-Swift swizzle via the Objective-C runtime, the
/// same no-bridging-header approach used in `XCTestPrivate.swift`. Both
/// known selector spellings are handled: `waitForQuiescenceIncludingAnimationsIdle:`
/// and the newer `waitForQuiescenceIncludingAnimationsIdle:isPreEvent:`.
enum Quiescence {
    /// Installs the swizzle once. Safe to call repeatedly.
    static func disableImplicitWait() {
        _ = installed
    }

    private static let installed: Bool = {
        guard let cls = objc_getClass("XCUIApplicationProcess") as? AnyClass else {
            NSLog("[tales-driver] Quiescence: XCUIApplicationProcess class not found")

            return false
        }

        let plain = NSSelectorFromString("waitForQuiescenceIncludingAnimationsIdle:")
        if let method = class_getInstanceMethod(cls, plain) {
            let block: @convention(block) (NSObject, ObjCBool) -> Void = { _, _ in }
            method_setImplementation(method, imp_implementationWithBlock(block))
            NSLog("[tales-driver] Quiescence: implicit wait disabled (plain selector)")

            return true
        }

        let preEvent = NSSelectorFromString("waitForQuiescenceIncludingAnimationsIdle:isPreEvent:")
        if let method = class_getInstanceMethod(cls, preEvent) {
            let block: @convention(block) (NSObject, ObjCBool, ObjCBool) -> Void = { _, _, _ in }
            method_setImplementation(method, imp_implementationWithBlock(block))
            NSLog("[tales-driver] Quiescence: implicit wait disabled (isPreEvent selector)")

            return true
        }

        NSLog("[tales-driver] Quiescence: no known wait selector on XCUIApplicationProcess")

        return false
    }()
}
