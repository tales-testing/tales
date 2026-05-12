# Mobile / iOS driver

Tales V1 supports iOS automation through Apple official tooling and a
repository-owned Swift/XCUITest HTTP driver. There is no Appium server, no
Maestro runtime, no IDB requirement, and no external WebDriverAgent dependency.

## Architecture

```text
.tales scenario
  -> step "mobile"
  -> internal/runtime/mobile.go
  -> internal/provider/mobile (Go)
  -> xcrun simctl + xcodebuild
  -> drivers/apple/TalesAppleDriver (Swift/XCUITest HTTP driver)
  -> XCUIApplication(bundleIdentifier: <SUT>)
```

The Go provider owns simulator lifecycle, app installation/launch/termination,
step serialization per mobile target, implicit waits, and artifact collection.
The Swift driver is maintained in this repository and exposes a small HTTP/JSON
surface for hierarchy, tap, input text, clear text, and screenshot operations.

## Dependency Policy

Allowed runtime dependencies:

- `xcrun`
- `xcrun simctl`
- `xcodebuild`
- `XCTest` / `XCUITest`
- Swift code owned by this repository

Explicitly not used:

- Appium
- Maestro runtime or CLI
- IDB as a required runtime dependency
- external WebDriverAgent
- Selenium
- Playwright for mobile automation
- third-party mobile automation runtimes

Maestro-style architecture can be useful inspiration, but Tales does not vendor
or execute Maestro code.

## Demo App

The demo app lives under:

```text
e2e/ios/demoapp/
```

It is a minimal SwiftUI app with bundle id:

```text
com.hyperxlab.tales.demo
```

Screens:

- Welcome: `welcome.title`, `welcome.register`
- Register: `register.screen`, `register.email`, `register.password`, `register.submit`, `register.error`
- Verification: `verify.screen`, `verify.code`, `verify.submit`, `verify.error`
- Home: `home.screen`, `home.title`, `home.email`

The verification code is intentionally hardcoded to `A1B2C3` so the mobile e2e
flow is deterministic.

## Make Targets

Normal CI targets remain platform-neutral:

```bash
make test
make lint
make e2e
make e2e-failure
```

macOS/Xcode-only targets:

```bash
make build-ios-demo
make e2e-ios
make e2e-ios-failure
```

`make build-ios-demo`:

- requires macOS
- requires `xcodebuild` and `xcrun`
- builds `e2e/ios/demoapp/TalesDemoApp.xcodeproj`
- writes derived data under `build/ios/demoapp`
- produces `TalesDemoApp.app` for iOS Simulator

`make e2e-ios`:

- builds the Tales binary
- builds the demo app
- sets `IOS_APP_PATH`, `IOS_BUNDLE_ID`, and `IOS_DEVICE_NAME`
- runs `tales test ./e2e/ios/pass --seed 1234 --parallel 1`
- writes reports under `build/reports`
- writes mobile artifacts under `build/artifacts`

`make e2e-ios-failure` runs the failing iOS suite, expects exit code `1`, and
verifies that screenshot and hierarchy artifacts exist and are referenced in the
JSONL report.

Useful overrides:

```bash
IOS_DEVICE_NAME="iPhone 16" make e2e-ios
IOS_DEVICE_NAME="iPhone 16 Pro" make e2e-ios-failure
```

## Build Requirements

The application under test must be built for iOS Simulator. A physical-device
`.app` bundle will not install into the simulator.

Tales V1 only auto-builds the repository demo app via `make build-ios-demo`.
User applications should be built by the owning project and passed through:

```bash
IOS_APP_PATH=/path/to/MyApp.app \
IOS_BUNDLE_ID=com.example.MyApp \
IOS_DEVICE_NAME="iPhone 16" \
  ./build/tales test ./my/mobile/suite --seed 1234
```

## DSL Example

```hcl
config {
  mobile = {
    targets = {
      iphone = {
        platform    = "ios"
        device_name = env("IOS_DEVICE_NAME", "iPhone 16")
        app         = env("IOS_APP_PATH")
        bundle_id   = env("IOS_BUNDLE_ID", "com.hyperxlab.tales.demo")
        driver = {
          host     = env("IOS_DRIVER_HOST", "127.0.0.1")
          port     = 9080
          external = false
          project  = "drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj"
          scheme   = "TalesAppleDriverUITests"
        }
      }
    }
  }
}

scenario "iOS register demo app" {
  step "mobile" "launch" {
    platform = "ios"
    target   = "iphone"
    launch { clear_state = true }
    expect {
      visible { id = "welcome.register"; timeout = "20s" }
    }
  }

  step "mobile" "open_register" {
    platform = "ios"
    target   = "iphone"
    actions {
      tap { id = "welcome.register" }
    }
    expect {
      visible { id = "register.screen"; timeout = "10s" }
    }
  }
}
```

Supported V1 actions:

- `tap { id = "..." }`
- `input_text { id = "..." value = "..." secure = true }`
- `clear_text { id = "..." }`

Actions have an implicit wait of `10s` with `250ms` polling. Each action may
also set `timeout = "2s"`.

Supported V1 expectations:

- `visible { id = "..." timeout = "10s" }`
- `not_visible { id = "..." timeout = "10s" }`

Expectations default to `10s` with `250ms` polling.

Supported V1 captures:

- `value("id")`
- `text("id")`
- `request.actions[N].value` for evaluated action values

Secure input values are masked in user-facing reports.

## Accessibility Identifiers

Tales selectors are accessibility identifiers only. Do not rely on visible text
as a selector.

SwiftUI example:

```swift
TextField("Email", text: $email)
    .accessibilityIdentifier("register.email")

SecureField("Password", text: $password)
    .accessibilityIdentifier("register.password")

Button("Register") {
    submit()
}
.accessibilityIdentifier("register.submit")
```

Every element used by Tales should have a stable identifier. Duplicate IDs are
reported as errors instead of guessed.

## Concurrency

The provider serializes mobile step execution per target name. Two scenarios
using the same target, for example `iphone`, cannot clear state or terminate the
app while each other is tapping or asserting. Different targets may still run in
parallel when configured separately.

## Failure Artifacts

On mobile step failure Tales writes:

```text
build/artifacts/mobile/<scenario>-<file-hash>/<step>/<phase>/attempt-<n>/screenshot.png
build/artifacts/mobile/<scenario>-<file-hash>/<step>/<phase>/attempt-<n>/hierarchy.json
```

The file hash prevents collisions when two files contain scenarios with the same
name. Paths are included in console, JUnit, and JSONL reports when available.

## Troubleshooting

- Simulator not found: verify `IOS_DEVICE_NAME` with `xcrun simctl list devices`.
- App path missing: run `make build-ios-demo` or set `IOS_APP_PATH` to a simulator `.app` bundle.
- Device build installed into simulator: rebuild the app with `-sdk iphonesimulator`.
- Bundle ID mismatch: check `IOS_BUNDLE_ID` matches the app's `PRODUCT_BUNDLE_IDENTIFIER`.
- Driver health timeout: ensure Xcode can run `TalesAppleDriverUITests` on the selected simulator.
- Element not found: verify `.accessibilityIdentifier(...)` and inspect `hierarchy.json`.
- No screenshot: check simulator permissions and fallback `xcrun simctl io screenshot` availability.
- Port conflict: set `IOS_DRIVER_HOST` or update the target driver port in the `.tales` file.

## Known Limitations

- iOS only; Android is intentionally not implemented in this branch.
- No OCR, image matching, XPath, predicates, swipes, or long press yet.
- No cloud device support.
- Real-device signing is best effort and not a V1 target.
- The driver transport is HTTP/JSON for now.
