# Prebuilt Android driver artifacts

This directory holds the built UiAutomator driver, embedded into the
`tales` binary by [`../embed.go`](../embed.go):

| File | Role |
|---|---|
| `tales-driver.apk` | the driver app: an empty shell owning the driver's package and permissions |
| `tales-driver-test.apk` | the instrumentation serving the driver's HTTP API on the device |
| `source.sha256` | SHA-256 of the Kotlin sources these APKs were built from |

They are committed so that **running** Android tests needs only `adb` and
a device — no JDK, no Gradle, no Android SDK. Building them does need
that toolchain, but only for someone changing the driver itself.

## Do not edit by hand

All three files are Gradle outputs. Regenerate them with:

```bash
make build-android-driver
```

and commit the result alongside the source change that prompted it.

## Staleness

Checked-in binaries drift from the source they were built from. The
`source.sha256` sentinel is how that is caught: CI recomputes the hash of
every Kotlin, Gradle and manifest file under
[`../TalesAndroidDriver`](../TalesAndroidDriver) and fails when it no
longer matches, naming the command to run. Verifying is pure file I/O,
so nothing outside this directory needs the Android plugin loaded.

Run the same check locally with:

```bash
make check-android-driver-fresh
```
