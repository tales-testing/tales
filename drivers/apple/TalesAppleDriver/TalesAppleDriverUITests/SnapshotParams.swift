import Foundation
import ObjectiveC.runtime
import XCTest

/// Tunes the accessibility snapshot parameters XCTest uses when it walks
/// the view hierarchy.
///
/// By default XCTest honors modal views: when a system dialog, a sheet,
/// or any modally-presented view is on screen, the snapshot only contains
/// that modal's subtree and everything behind it is omitted. That hides
/// the app content the test usually wants to assert against (and, on the
/// flows Tales exercises, hides the form behind an autofill / permission
/// prompt). Setting `snapshotKeyHonorModalViews` to 0 makes the snapshot
/// include the whole tree regardless of modal presentation.
///
/// Implemented by swizzling `-[XCAXClient_iOS defaultParameters]` via the
/// Objective-C runtime — the same no-bridging-header approach used in
/// `XCTestPrivate.swift` and `Quiescence.swift`. The original parameters
/// are preserved; only the overrides below are merged in.
enum SnapshotParams {
    /// Parameter overrides merged into every accessibility snapshot.
    private static let overrides: [String: Int] = [
        "snapshotKeyHonorModalViews": 0,
    ]

    /// Installs the swizzle once. Safe to call repeatedly.
    static func apply() {
        _ = installed
    }

    private static let installed: Bool = {
        guard let cls = objc_getClass("XCAXClient_iOS") as? AnyClass else {
            NSLog("[tales-driver] SnapshotParams: XCAXClient_iOS class not found")

            return false
        }

        let selector = NSSelectorFromString("defaultParameters")
        guard let method = class_getInstanceMethod(cls, selector) else {
            NSLog("[tales-driver] SnapshotParams: defaultParameters selector not found")

            return false
        }

        typealias OriginalIMP = @convention(c) (NSObject, Selector) -> NSDictionary
        var originalIMP: OriginalIMP?

        let block: @convention(block) (NSObject) -> NSDictionary = { receiver in
            let base = originalIMP?(receiver, selector) ?? NSDictionary()
            guard let merged = base.mutableCopy() as? NSMutableDictionary else {
                return base
            }

            for (key, value) in SnapshotParams.overrides {
                merged[key] = value
            }

            return merged
        }

        let previous = method_setImplementation(method, imp_implementationWithBlock(block))
        originalIMP = unsafeBitCast(previous, to: OriginalIMP.self)
        NSLog("[tales-driver] SnapshotParams: defaultParameters overrides installed")

        return true
    }()
}
