import SwiftUI

/// TalesAppleDriverHost is a stub iOS app whose only purpose is to satisfy
/// the iOS UI Test bundle host requirement. The UI Test bundle drives a
/// separate user application via XCUIApplication(bundleIdentifier:), so the
/// host app body is intentionally empty.
@main
struct TalesAppleDriverHostApp: App {
    var body: some Scene {
        WindowGroup {
            Text("Tales driver host")
                .padding()
        }
    }
}
