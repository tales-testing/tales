import Foundation
import XCTest

/// XCUITest entry point. The single test method starts the in-simulator HTTP
/// server and blocks until the test process is killed. From the host Tales
/// runner's point of view, the test "never finishes" while the driver is
/// live; xcodebuild's signal handling is what stops it.
final class TalesAppleDriverUITests: XCTestCase {
    override class var defaultTestSuite: XCTestSuite {
        let suite = XCTestSuite(name: "TalesAppleDriverUITests")
        suite.addTest(TalesAppleDriverUITests(selector: #selector(testRunServer)))
        return suite
    }

    @objc
    func testRunServer() {
        continueAfterFailure = true

        // Install the XCTest runtime tweaks before serving any request:
        // disable the implicit quiescence wait that otherwise hangs on
        // animating apps, and reveal elements behind modal views in
        // accessibility snapshots.
        Quiescence.disableImplicitWait()
        SnapshotParams.apply()

        let port = TalesDriverConfig.port
        let host = TalesDriverConfig.host

        let server = TalesHTTPServer(host: host, port: port)

        do {
            try server.start()
        } catch {
            XCTFail("failed to start tales driver server: \(error)")
            return
        }

        NSLog("[tales-driver] listening on \(host):\(port)")

        // Keep the XCTest process alive while still letting main-queue XCUITest
        // work run. A blocking semaphore here starves DispatchQueue.main and can
        // make snapshot/tap calls hang under newer Xcode releases.
        RunLoop.current.run()
    }
}

enum TalesDriverConfig {
    static var host: String {
        ProcessInfo.processInfo.environment["TALES_DRIVER_HOST"] ?? "127.0.0.1"
    }

    static var port: Int {
        if let raw = ProcessInfo.processInfo.environment["TALES_DRIVER_PORT"], let value = Int(raw) {
            return value
        }
        return 9080
    }
}
