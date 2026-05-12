#!/usr/bin/env bash
set -euo pipefail

jsonl=${1:-build/reports/e2e-ios-failure.jsonl}
artifacts_root=${2:-build/artifacts/mobile}

fail() {
  echo "verify-ios-failure: $*" >&2
  exit 1
}

[ -f "$jsonl" ] || fail "JSONL report not found: $jsonl"

grep -q '"scenario":"iOS failure produces artifacts"' "$jsonl" || fail "expected scenario not found in JSONL"
grep -q '"step":"missing_element"' "$jsonl" || fail "expected failing step missing_element not found in JSONL"

if grep -Eiq 'CoreSimulator|simctl|ensure booted|driver did not become healthy|app install|install app|app launch|launch app|acquire session|external driver health' "$jsonl"; then
  fail "failure looks environmental/driver-related, not the expected UI assertion failure"
fi

grep -Eiq 'element not found|visible timeout|visibility timeout|element\.that\.does\.not\.exist|timed out' "$jsonl" || \
  fail "expected missing-element/visibility assertion error not found in JSONL"

grep -q '"artifacts"' "$jsonl" || fail "JSONL does not include artifact paths"

[ -d "$artifacts_root" ] || fail "artifacts root not found: $artifacts_root"

screenshot=$(find "$artifacts_root" -name screenshot.png -type f | head -n 1 || true)
hierarchy=$(find "$artifacts_root" -name hierarchy.json -type f | head -n 1 || true)

[ -n "$screenshot" ] || fail "missing screenshot artifact under $artifacts_root"
[ -n "$hierarchy" ] || fail "missing hierarchy artifact under $artifacts_root"

echo "Verified expected iOS failure."
echo "Screenshot: $screenshot"
echo "Hierarchy:  $hierarchy"
