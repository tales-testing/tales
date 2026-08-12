#!/usr/bin/env bash
set -euo pipefail

# Asserts that the Android failure suite failed for the reason it is
# meant to: a UI assertion, not a broken device or driver. A suite that
# "fails" because adb was missing would otherwise look like a pass of
# this check.

jsonl=${1:-build/reports/e2e-android-failure.jsonl}
artifacts_root=${2:-build/artifacts/mobile}

fail() {
  echo "verify-android-failure: $*" >&2
  exit 1
}

[ -f "$jsonl" ] || fail "JSONL report not found: $jsonl"

grep -q '"scenario":"Android failure produces artifacts"' "$jsonl" || fail "expected scenario not found in JSONL"
grep -q '"step":"android_missing_element"' "$jsonl" || fail "expected failing step android_missing_element not found in JSONL"

if grep -Eiq 'adb executable not found|no ready Android device|did not finish booting|driver did not become healthy|install app|acquire session|external driver health' "$jsonl"; then
  fail "failure looks environmental (device/driver), not the expected UI assertion failure"
fi

grep -Eiq 'element not found|not found after|element\.that\.does\.not\.exist' "$jsonl" || \
  fail "expected missing-element assertion error not found in JSONL"

grep -q '"artifacts"' "$jsonl" || fail "JSONL does not include artifact paths"

[ -d "$artifacts_root" ] || fail "artifacts root not found: $artifacts_root"

screenshot=$(find "$artifacts_root" -name screenshot.png -type f | head -n 1 || true)
hierarchy=$(find "$artifacts_root" -name hierarchy.json -type f | head -n 1 || true)

[ -n "$screenshot" ] || fail "missing screenshot artifact under $artifacts_root"
[ -n "$hierarchy" ] || fail "missing hierarchy artifact under $artifacts_root"

echo "Verified expected Android failure."
echo "Screenshot: $screenshot"
echo "Hierarchy:  $hierarchy"
