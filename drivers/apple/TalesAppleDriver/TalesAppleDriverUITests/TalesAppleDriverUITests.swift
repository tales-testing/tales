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

        // Block until the runner is killed by xcodebuild teardown.
        let semaphore = DispatchSemaphore(value: 0)
        semaphore.wait()
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
