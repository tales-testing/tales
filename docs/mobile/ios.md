# Mobile / iOS driver

Tales V1 supports iOS automation through Apple official tooling and a
repository-owned Swift/XCUITest HTTP driver. There is no Appium server, no
Maestro runtime, no IDB requirement, and no external WebDriverAgent dependency.

**Single-binary distribution.** The Swift driver source is embedded into the
`tales` binary at build time via `go:embed`. On the first iOS test run, Tales
extracts the driver to a per-user cache, builds it once with Xcode, and reuses
the cached build on subsequent runs. A released `tales` binary therefore runs
iOS tests on any macOS host with Xcode and a Simulator runtime installed —
the Tales repository does not need to be checked out next to it.

## Architecture

```text
.tales scenario
  -> step "mobile"
  -> internal/runtime/mobile.go
  -> internal/provider/mobile (Go)
  -> xcrun simctl + xcodebuild
  -> embedded XCUITest driver (extracted from the binary on first use)
  -> XCUIApplication(bundleIdentifier: <SUT>)
```

The Go provider owns simulator lifecycle, app installation/launch/termination,
step serialization per mobile target, implicit waits, and artifact collection.
The Swift driver is maintained in this repository under
`drivers/apple/TalesAppleDriver/` and exposes a small HTTP/JSON surface for
hierarchy, tap, input text, clear text, and screenshot operations. The
embedded copy is materialized to disk and built on demand — see
[Embedded driver cache](#embedded-driver-cache) below.

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
make doctor-ios
make build-ios-demo
make e2e-ios
make e2e-ios-failure
```

`make doctor-ios` prints system, Xcode, `simctl`, and iOS-related environment
state without requiring optional variables to be set. Run it first when a local
simulator behaves strangely after an Xcode upgrade.

`make build-ios-demo`:

- requires macOS
- requires `xcodebuild` and `xcrun`
- builds `e2e/ios/demoapp/TalesDemoApp.xcodeproj`
- writes derived data under `build/ios/demoapp`
- produces `TalesDemoApp.app` for iOS Simulator
- writes the resolved app bundle to `build/ios/demoapp/app_path.txt`

`make e2e-ios`:

- builds the Tales binary
- builds the demo app
- sets `IOS_APP_PATH`, `IOS_BUNDLE_ID`, and `IOS_DEVICE_NAME`
- defaults to `IOS_DEVICE_NAME="iPhone 17"`
- runs `tales test ./e2e/ios/pass --seed 1234 --parallel 1`
- writes reports under `build/reports`
- writes mobile artifacts under `build/artifacts`
- prints the selected device configuration, report paths, and artifact root

`make e2e-ios-failure` runs the failing iOS suite, expects exit code `1`, and
verifies that the failure is the expected `missing_element` visibility failure,
not a simulator/driver environment failure. It also checks that screenshot and
hierarchy artifacts exist and are referenced in the JSONL report.

Useful overrides:

```bash
IOS_DEVICE_NAME="iPhone 17" make e2e-ios
IOS_DEVICE_NAME="iPhone 17 Pro" make e2e-ios-failure
```

## Build Requirements

The application under test must be built for iOS Simulator. A physical-device
`.app` bundle will not install into the simulator.

Tales V1 only auto-builds the repository demo app via `make build-ios-demo`.
User applications should be built by the owning project and passed through:

```bash
IOS_APP_PATH=/path/to/MyApp.app \
IOS_BUNDLE_ID=com.example.MyApp \
IOS_DEVICE_NAME="iPhone 17" \
  ./build/tales test ./my/mobile/suite --seed 1234
```

## DSL Example

```hcl
config {
  mobile = {
    targets = {
      iphone = {
        platform    = "ios"
        device_name = env("IOS_DEVICE_NAME", "iPhone 17")
        app         = env("IOS_APP_PATH")
        bundle_id   = env("IOS_BUNDLE_ID", "com.hyperxlab.tales.demo")
        driver = {
          host     = env("IOS_DRIVER_HOST", "127.0.0.1")
          port     = 9080
          external = false
          // Embedded mode is the default: project/scheme omitted.
          // The driver is extracted from the tales binary and built once.
        }
      }
    }
  }
}

scenario "iOS register demo app" {
  # Guard the scenario so cross-platform CI runs do not try to
  # exercise the mobile provider on Linux / Windows. The provider
  # itself stays strict — running an iOS step on a non-darwin host
  # without a skip rule still fails loudly so silent skips never
  # mask real coverage gaps.
  skip_unless {
    os      = ["darwin"]
    env_set = ["IOS_APP_PATH"]
    reason  = "iOS tests require macOS and IOS_APP_PATH pointing at a simulator-built app"
  }

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
- `double_tap { id = "..." }`
- `long_press { id = "..." duration = "1s" }` — `duration` optional (default `1s`).
- `input_text { id = "..." value = "..." secure = true }`
- `clear_text { id = "..." }`
- `swipe { id = "..." direction = "up" distance = 0.6 duration = "300ms" }` — drags
  one finger across the element. `direction` is the finger travel
  (`up` / `down` / `left` / `right`); `distance` (optional, a fraction in
  `(0, 1]`, default `0.6`) is the travel as a share of the element's relevant
  dimension; `duration` optional (default `300ms`).
- `scroll { id = "..." direction = "down" }` — scrolls the element's content.
  `direction` is the content direction to reveal (the finger travels the
  opposite way). Accepts the same optional `distance` / `duration` as `swipe`.

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

## Driver modes

The `driver` block selects one of three execution modes:

| Configuration                                         | Mode                                                  |
| ----------------------------------------------------- | ----------------------------------------------------- |
| `external = false`, no `source_path`      | **Embedded** (default). Extract + build + cache.      |
| `external = false`, `source_path = "..."` | **Developer override**. Same pipeline, local source.  |
| `external = true`                         | **External**. Health-check only; never spawn or kill. |

> **Note**: the legacy `driver.project` + `driver.scheme` mode is no longer
> supported. A `.tales` file mentioning either field now fails parsing with
> a clear migration message. Omit both for embedded mode, or set
> `source_path` for a local Swift checkout.

### Embedded mode (default)

No extra fields are required. Tales:

1. Hashes the embedded driver source and assembles a cache key from
   `<source-hash>-xcode-<version>-sdk-<version>-dev-<DEVELOPER_DIR>-ios-<runtime>-mac-<major>`.
2. Extracts the source into `<cache>/source/` atomically (rename-after-write).
3. Runs `xcodebuild build-for-testing` once, capturing output to
   `<cache>/logs/build.log` and writing a `build.ok` marker on success.
4. Launches the driver via `xcodebuild test-without-building -xctestrun ...`
   on every subsequent session.
5. Self-heals once on `/health` failure: invalidates `build.ok` and rebuilds
   from scratch before failing the test.

### Developer override

When iterating on the Swift driver, point `source_path` at a local checkout:

```hcl
driver = {
  external    = false
  source_path = "/path/to/drivers/apple/TalesAppleDriver"
}
```

The cache key still includes the source hash, so edits invalidate the cache
automatically.

### External mode (debugging)

When you launch `xcodebuild test` yourself (for example to attach a debugger
or capture detailed logs), point Tales at the existing endpoint:

```hcl
driver = {
  external = true
  host     = "127.0.0.1"
  port     = 9080
}
```

Tales only health-checks the URL; it never spawns or kills an external driver.

## Embedded driver cache

### Location

- Default: `~/Library/Caches/tales/apple-driver/<cache-key>/` on macOS.
- Override: set `TALES_DRIVER_CACHE_DIR` to a directory of your choice (used as
  the final base, no extra suffix). Useful in CI to share or pin a cache.

### Layout

```text
~/Library/Caches/tales/apple-driver/<cache-key>/
  source/                     extracted Swift driver source
    TalesAppleDriver.xcodeproj/
    ...
  derived-data/               xcodebuild -derivedDataPath
  logs/
    build.log                 build-for-testing stdout+stderr
  extract.ok                  marker, written after a successful extract
  build.ok                    marker, contains the cached .xctestrun path
  metadata.json               source_hash, xcode_version, ios_runtime, ...
  .lock                       cross-process flock to serialize parallel tales
```

### Wiping the cache

```bash
make clean-ios-driver-cache
# or, for a custom base:
rm -rf "$TALES_DRIVER_CACHE_DIR"
```

Wipe the cache after a major Xcode upgrade, when you suspect a corrupted build,
or before single-binary smoke testing.

### Inspecting the cache

Run `tales doctor` for a one-screen view of everything that influences the
embedded driver pipeline:

```bash
tales doctor          # human-readable text
tales doctor --json   # machine-readable JSON, suitable for CI assertions
```

The output covers:

- Tales build info (version, go runtime, platform).
- Embedded driver: source hash (16-hex prefix used in the cache key), file
  count, total uncompressed bytes.
- Driver cache: base directory and one block per entry with extract / build
  markers, the cached `.xctestrun` path, the recorded Xcode / SDK / iOS
  runtime / macOS / created-at metadata, on-disk size, and a marker telling
  you whether the entry was built from the source the running binary embeds
  (`✓ matches embedded` vs `⚠ source-hash mismatch — rebuilt on next run`).
- Xcode introspection (`xcodebuild -version`, `xcrun --show-sdk-version`,
  `xcode-select -p`, `sw_vers`).
- Available simulator runtimes and devices.
- Hints (cache wipe, env override, stale CoreSimulator recovery).

Missing Xcode or unavailable simctl is reported as a degraded section, not as
an error: `tales doctor` is the tool you reach for when things are broken.

For CI, prefer `--json`:

```bash
tales doctor --json | jq .embedded_driver.source_hash_short
tales doctor --json | jq '.cache.entries[] | select(.built == false)'
```

`make doctor-ios` remains as a shell-only fallback for sysadmins that have
not built the Tales binary yet (it covers the same Xcode / simctl ground but
not the Tales cache state).

## Log paths

| Stream                   | Path                                                                |
| ------------------------ | ------------------------------------------------------------------- |
| Embedded driver build    | `<cache>/logs/build.log`                                            |
| Runtime driver process   | `build/artifacts/mobile/driver/<target>/driver.log`                 |
| Failure-step screenshots | `build/artifacts/mobile/<scenario>-<hash>/<step>/<phase>/attempt-N/screenshot.png` |
| Failure-step hierarchy   | `build/artifacts/mobile/<scenario>-<hash>/<step>/<phase>/attempt-N/hierarchy.json` |

## Simulator Selection

When duplicate simulator names exist across runtimes, Tales selects
deterministically:

- available iOS devices only
- newest iOS runtime first
- booted device first when runtime and name are equal
- UDID as a stable tie-breaker

The selected simulator name, UDID, and runtime are printed before the session is
used. This avoids accidentally choosing an older duplicate runtime when Xcode
ships several simulator runtimes.

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

When Tales starts the managed Apple driver, stdout and stderr are written to:

```text
build/artifacts/mobile/driver/<target>/driver.log
```

If the driver does not become healthy, the failure message includes this log
path and suggests `make doctor-ios`.

## Troubleshooting

- Run `make doctor-ios` to collect system, Xcode, runtime, device, and
  environment diagnostics.
- Simulator not found: verify `IOS_DEVICE_NAME` with `xcrun simctl list devices`.
- App path missing: run `make build-ios-demo` or set `IOS_APP_PATH` to a simulator `.app` bundle.
- Device build installed into simulator: rebuild the app with `-sdk iphonesimulator`.
- Bundle ID mismatch: check `IOS_BUNDLE_ID` matches the app's `PRODUCT_BUNDLE_IDENTIFIER`.
- Driver build failure: read `<cache>/logs/build.log` (path printed in the
  error). Common causes: SDK no longer installed, signing config drift, stale
  derived data. `make clean-ios-driver-cache` then retry.
- Driver health timeout: Tales auto-retries once with a rebuilt cache. If it
  still fails, inspect the runtime log at
  `build/artifacts/mobile/driver/<target>/driver.log`.
- Stale CoreSimulator after Xcode upgrade: run `sudo xcodebuild -runFirstLaunch`,
  then `xcrun simctl shutdown all`, then
  `killall -9 com.apple.CoreSimulator.CoreSimulatorService || true`, then
  `xcrun simctl list devices`. Optionally `make clean-ios-driver-cache` to
  force a fresh driver build against the upgraded toolchain.
- Cache invalidation: the cache key already includes Xcode version, SDK
  version, `DEVELOPER_DIR`, iOS runtime, and macOS major. Manually wipe if
  in doubt: `make clean-ios-driver-cache`.
- Element not found: verify `.accessibilityIdentifier(...)` and inspect `hierarchy.json`.
- No screenshot: check simulator permissions and fallback `xcrun simctl io screenshot` availability.
- Port conflict: set `IOS_DRIVER_HOST` or update the target driver port in
  the `.tales` file. Tales also calls `simctl terminate <runner>` on session
  close as belt-and-suspenders, but a manually started XCUITest runner on
  the same port will still collide.

## Known Limitations

- iOS only; Android is intentionally not implemented in this branch.
- No OCR, image matching, XPath, predicates, swipes, or long press yet.
- No cloud device support.
- Real-device signing is best effort and not a V1 target.
- The driver transport is HTTP/JSON for now.
