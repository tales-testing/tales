#!/usr/bin/env bash
# Verify a Tales visual HTML report:
#   - the file exists and is non-empty
#   - it embeds a parseable JSON data island
#   - every <img src="..."> resolves to a file on disk, relative to the
#     report's directory
#   - the suite name / scenarios are present (sanity check)
#   - no obvious raw secret leaked into the document
#
# Run as: scripts/verify-ios-visual.sh build/reports/e2e-ios.html
set -euo pipefail

report=${1:-build/reports/e2e-ios.html}

fail() {
  echo "verify-ios-visual: $*" >&2
  exit 1
}

[ -f "$report" ] || fail "HTML report not found: $report"
[ -s "$report" ] || fail "HTML report is empty: $report"

grep -q 'id="tales-report-data"' "$report" || fail "data island missing"
grep -q '<!doctype html>' "$report" || fail "HTML doctype missing"
grep -q 'Tales Visual Report' "$report" || fail "report title missing"

# Extract the JSON payload between the data-island script tags and pipe
# it to a JSON parser to make sure it is well-formed.
payload=$(sed -n 's@.*<script type="application/json" id="tales-report-data">\(.*\)</script>.*@\1@p' "$report" | head -n 1)
[ -n "$payload" ] || fail "data island payload empty"

if command -v python3 >/dev/null 2>&1; then
  echo "$payload" | python3 -c 'import json,sys; json.loads(sys.stdin.read())' \
    || fail "data island is not valid JSON"
fi

# Walk every <img src="..."> referenced in the HTML and assert the file
# resolves on disk relative to the report's directory.
report_dir=$(dirname "$report")

while IFS= read -r src; do
  [ -z "$src" ] && continue

  case "$src" in
    http://*|https://*|data:*) fail "external asset detected (no CDN allowed): $src" ;;
  esac

  if [ -f "$report_dir/$src" ] || [ -f "$src" ]; then
    continue
  fi

  fail "referenced screenshot not on disk: $src"
done < <(grep -Eo 'src="[^"]+"' "$report" | sed -E 's/src="([^"]+)"/\1/' | sort -u)

# Raw secret canary: the demo scenarios should never leak the password
# literal. Adapt the canary list if a future suite adds another secret.
for canary in 'hunter2' 'Secret123'; do
  if grep -F -q "$canary" "$report"; then
    fail "raw secret $canary found in HTML report"
  fi
done

echo "verify-ios-visual: ok ($report)"
