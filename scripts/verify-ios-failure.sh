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

# Issue #73: a wait that runs out never blames the hierarchy fetch its own
# deadline cancelled, and says what the driver answered meanwhile. The 1s
# wait may legitimately see no fetch come back on a loaded runner, and only
# then is the cancelled request part of the story.
failure=$(grep '"step":"missing_element"' "$jsonl" | grep '"status":"fail"' || true)
[ -n "$failure" ] || fail "no failing step event for missing_element"

grep -q 'hierarchy fetched' <<<"$failure" || \
  fail "the wait failure does not say when the driver last answered (issue #73)"

if grep -q 'poll timed out' <<<"$failure"; then
  fail "the wait failure blames its own deadline (issue #73)"
fi

if grep -q 'context deadline exceeded' <<<"$failure" && ! grep -q 'no hierarchy fetched during the wait' <<<"$failure"; then
  fail "the wait failure blames the request its own deadline cancelled (issue #73)"
fi

grep -q '"artifacts"' "$jsonl" || fail "JSONL does not include artifact paths"

[ -d "$artifacts_root" ] || fail "artifacts root not found: $artifacts_root"

screenshot=$(find "$artifacts_root" -name screenshot.png -type f | head -n 1 || true)
hierarchy=$(find "$artifacts_root" -name hierarchy.json -type f | head -n 1 || true)

[ -n "$screenshot" ] || fail "missing screenshot artifact under $artifacts_root"
[ -n "$hierarchy" ] || fail "missing hierarchy artifact under $artifacts_root"

echo "Verified expected iOS failure."
echo "Screenshot: $screenshot"
echo "Hierarchy:  $hierarchy"
