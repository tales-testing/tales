#!/usr/bin/env bash
# Verify a Tales visual HTML report:
#   - the file exists and is non-empty
#   - it embeds a parseable JSON data island
#   - every screenshot path declared inside the JSON payload resolves
#     to a file on disk, relative to the report's directory
#   - no external asset (http/https/data:) is referenced
#   - no obvious raw secret leaked into the rendered document
#
# Run as: scripts/verify-ios-visual.sh build/reports/e2e-ios.html
#
# python3 is required for the JSON walk.
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

command -v python3 >/dev/null 2>&1 || fail "python3 is required to validate the JSON payload"

# The visual report posts screenshot/hierarchy paths dynamically from the
# embedded JSON data island rather than as static <img src=...> attributes,
# so the file existence check walks the JSON payload itself. The python
# helper:
#   - extracts the data island between the two <script> tags
#   - parses it
#   - reverses the "<\/script" defuse so paths are usable on disk
#   - prints "external|<url>" for any http(s)/data asset (a failure case)
#   - prints "screenshot|<path>" and "hierarchy|<path>" for every artifact
report_dir=$(dirname "$report")

payload_walk=$(
  REPORT_PATH="$report" python3 <<'PY'
import json, os, re, sys

with open(os.environ["REPORT_PATH"], "r", encoding="utf-8") as fh:
    html = fh.read()

m = re.search(
    r'<script type="application/json" id="tales-report-data">(.*?)</script>',
    html,
    re.S,
)
if not m:
    sys.stderr.write("data island not found\n")
    sys.exit(2)

raw = m.group(1).replace("<\\/script", "</script")

try:
    data = json.loads(raw)
except json.JSONDecodeError as e:
    sys.stderr.write(f"data island is not valid JSON: {e}\n")
    sys.exit(2)

def emit(kind, path):
    if not path:
        return
    if re.match(r"^(?:https?:|data:)", path, re.I):
        print(f"external|{path}")
        return
    print(f"{kind}|{path}")

for scenario in data.get("scenarios", []) or []:
    for step in scenario.get("steps", []) or []:
        for action in step.get("actions", []) or []:
            emit("screenshot", action.get("screenshot"))
            emit("hierarchy", action.get("hierarchy"))
PY
)

if [ -z "$payload_walk" ]; then
  # No actions captured screenshots. That's legitimate for capture modes
  # other than `actions` / `steps`, and the doc validates that case
  # elsewhere — nothing to assert here.
  :
else
  while IFS='|' read -r kind path; do
    [ -z "$kind" ] && continue

    if [ "$kind" = "external" ]; then
      fail "external asset detected (no CDN allowed): $path"
    fi

    if [ -f "$report_dir/$path" ] || [ -f "$path" ]; then
      continue
    fi

    fail "referenced $kind not on disk: $path"
  done <<EOF
$payload_walk
EOF
fi

# Raw secret canary: the demo scenarios should never leak the password
# literal. Adapt the canary list if a future suite adds another secret.
for canary in 'hunter2' 'Secret123'; do
  if grep -F -q "$canary" "$report"; then
    fail "raw secret $canary found in HTML report"
  fi
done

echo "verify-ios-visual: ok ($report)"
