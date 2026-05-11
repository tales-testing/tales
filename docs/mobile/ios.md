# Mobile / iOS driver

Tales V1 supports a single mobile platform: iOS, automated through Apple's
own `xcrun simctl` lifecycle tools and an in-simulator XCUITest runner that
exposes a small HTTP/JSON API. This document describes the architecture, the
DSL surface, how to run it locally, and the V1 limitations.

## Architecture overview

```
.tales scenario
   │
   │  step "mobile" "..."
   ▼
internal/runtime/mobile.go ── evaluates platform/target/actions/expect/capture
   │
   ▼
internal/provider/mobile (Go)
   ├─ session per target (cached, reused between steps)
   │
   ├─ internal/provider/mobile/apple
   │    ├─ simctl  → xcrun simctl (boot, install, launch, terminate, screenshot)
   │    └─ xcodebuild → xcrun xcodebuild test (TalesAppleDriverUITests)
   │
   └─ internal/provider/mobile/driver (HTTP/JSON)
        │
        ▼
   drivers/apple/TalesAppleDriver (Swift, runs INSIDE the simulator)
        │
        ▼
   XCUIApplication(bundleIdentifier: <SUT>)
```

The Go side never touches XCUITest directly. The Swift driver runs as a UI
test inside the simulator and exposes a small HTTP server that Tales talks
to through `internal/provider/mobile/driver`. The Go side owns the iOS app
lifecycle (boot, install, launch, terminate) through `xcrun simctl` because
that is more deterministic than driving the simulator through XCUITest.

## Dependency policy

- **No Appium.** Tales never spawns or links Appium servers.
- **No Maestro at runtime.** Tales does not shell out to `maestro`. The
  TalesAppleDriver Swift target is inspired by Maestro's general approach
  (a UI test bundle hosting a local HTTP server) but ships with no Maestro
  code.
- **Apple tools only.** Lifecycle and UI automation rely exclusively on
  `xcrun`, `simctl`, `xcodebuild`, and `XCTest` / `XCUITest`.
- **HTTP/JSON for V1.** The driver client is HTTP/JSON. The Go-side
  `driver.Driver` interface is intentionally transport-agnostic so a gRPC
  client can be added later without touching the mobile provider.
- **Zero external Swift dependencies.** The driver uses only
  `Foundation`, `XCTest`, and `Network.framework`.

## Supported DSL (V1)

```hcl
config {
  mobile = {
    targets = {
      iphone = {
        platform    = "ios"                # required, "ios" only
        device_name = "iPhone 17 Pro"      # required, matches `xcrun simctl list devices`
        app         = "./build/MyApp.app"  # required, path to a .app bundle
        bundle_id   = "com.example.MyApp"  # required
        driver = {
          host     = "127.0.0.1"
          port     = 9080
          external = true                  # optional; default: false (Tales starts the driver)
          project  = "drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj"
          scheme   = "TalesAppleDriverUITests"
        }
      }
    }
  }
}

scenario "register" {
  step "mobile" "launch" {
    platform = "ios"
    target   = "iphone"
    launch { clear_state = true }          # optional
    expect {
      visible { id = "welcome.register"; timeout = "20s" }
    }
  }

  step "mobile" "register" {
    platform = "ios"
    target   = "iphone"
    actions {
      tap        { id = "welcome.register" }
      input_text { id = "register.email";    value = generate("user_email") }
      input_text { id = "register.password"; value = generate("user_password"); secure = true }
      clear_text { id = "register.email" }
      tap        { id = "register.submit" }
    }
    expect {
      visible     { id = "register.verification_code"; timeout = "10s" }
      not_visible { id = "login.error";                timeout = "5s"  }
    }
    capture {
      email = value("register.email")
      title = text("home.title")
    }
  }

  teardown {
    step "mobile" "terminate" {
      platform = "ios"
      target   = "iphone"
      terminate {}
    }
  }
}
```

- `id` is the element's `accessibilityIdentifier` exposed by the SUT.
- `value("id")` returns `XCUIElement.value`; `text("id")` falls back to the
  element label when the text is empty.
- `secure = true` masks the input value in console / JSONL / JUnit reports.

## Unsupported in V1

- Android (the parser rejects `platform = "android"` with a clean error).
- gRPC transport.
- Appium or Maestro integration.
- Real device support beyond what `simctl` happens to allow.
- Selectors other than accessibility id (text, XPath, predicates, image,
  OCR).
- Gestures beyond tap / input_text / clear_text (swipe, long-press, pinch).
- Video recording, performance metrics, push notifications, permissions
  automation.

The roadmap reserves `platform = "android"` for a follow-up branch that
will plug a UIAutomator backend behind the same `driver.Driver` interface.

## Accessibility identifier requirement

The mobile provider looks up elements exclusively by their
`accessibilityIdentifier`. The application under test MUST expose stable
identifiers on every interactive element. In SwiftUI:

```swift
Button("Create account") { /* ... */ }
    .accessibilityIdentifier("welcome.register")
```

In UIKit:

```swift
button.accessibilityIdentifier = "welcome.register"
```

Without a stable identifier Tales fails the step with a clear
`element not found: id "<id>"` error and writes a hierarchy artifact for
inspection.

## Running locally

### Prerequisites

- macOS with Xcode (tested with Xcode 26.4 and iOS 26.4 SDK).
- A booted (or at least available) iOS Simulator.
- A `.app` bundle of the SUT compiled for the simulator.

### Manual driver run

```bash
# 1. Build the driver bundle once
xcodebuild \
  -project drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj \
  -scheme TalesAppleDriverUITests \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
  build-for-testing

# 2. Boot the simulator
xcrun simctl boot "iPhone 17 Pro" || true

# 3. Start the driver and keep the test process alive
xcodebuild \
  -project drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj \
  -scheme TalesAppleDriverUITests \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
  test-without-building

# 4. In another shell, run Tales with the sample suite
IOS_APP_PATH=./build/MyApp.app \
IOS_BUNDLE_ID=com.example.MyApp \
IOS_DEVICE_NAME='iPhone 17 Pro' \
  tales test ./e2e/ios --seed 1234
```

### Make target

`make e2e-ios` does step (1) for you when run on macOS with
`IOS_APP_PATH` / `IOS_BUNDLE_ID` set. It prints the exact commands for
steps (3) and (4).

### External driver mode

Setting `driver.external = true` tells Tales NOT to start the driver via
`xcodebuild`; it only waits for `/health` to answer on the configured host
and port. Use this when you are iterating on the Swift handlers (run the
driver from Xcode manually and let Tales reconnect on every run).

## Known limitations

- **Approximate visibility.** `visible` is approximated by
  `(frame.width > 0 && frame.height > 0) || isSelected || hasFocus`. An
  element overlapped by another view may still report visible.
- **Coordinate convention.** Taps use screen-space coordinates relative to
  the active window. Multi-window setups (e.g. iPad Slide Over) may need
  manual coordinate adjustments.
- **clear_text fallback.** When the element's `value` is empty the
  provider erases `64` characters by default, sufficient for most fields
  but not for arbitrary text views.
- **No retry on transient driver disconnects.** The HTTP client has a
  10-second per-request timeout but does not reconnect on lost TCP
  sessions. Tales relies on the standard step-level `retry { attempts =
  N }` block for resilience.
- **The driver UI test bundle ships with no team / "Sign to Run Locally".**
  Update `DEVELOPMENT_TEAM` in `project.pbxproj` if you need code signing
  for a real device.

## Troubleshooting

- **`element not found: id "X"`.** Either the SUT does not expose the
  accessibility identifier on that element, or the screen has not finished
  rendering. Use `expect { visible { id = "X"; timeout = "10s" } }` before
  acting on it.
- **`multiple elements share the same id`.** Two elements expose the same
  `accessibilityIdentifier`. Make them unique on the SUT side; Tales never
  guesses which one to interact with.
- **`driver /health returned status "..."`.** The Swift driver has not
  finished booting yet. Increase `HealthTimeout` (currently 60s default,
  configurable through `xcodebuild.Options`).
- **Driver process leaks.** Tales calls `Provider.Close()` at suite end to
  stop the xcodebuild process. If you still see leaks, check that the
  scenario has at least one `step "mobile"` so the provider is exercised.
