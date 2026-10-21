#!/usr/bin/env bash
# Verify the browser step diagnostics of issue #83 after a browser e2e run.
#
#   scripts/verify-browser-diagnostics.sh failure <artifacts-dir> <report.jsonl>
#       after e2e/browser-fail: the failed "diagnose" step of
#       e2e/browser-fail/diagnostics.tales left console.jsonl,
#       network.jsonl and state.json next to its dom.html, with the page's
#       logs and exception, its fetch without the query string, and the
#       focused field; the JSONL action events carry their timing and the
#       clicks their point.
#
#   scripts/verify-browser-diagnostics.sh pass <artifacts-dir>
#       after e2e/browser in actions capture mode: the passing "observe"
#       step of e2e/browser/diagnostics.tales wrote the same three files.
#
# <artifacts-dir> is the browser artifacts root, e.g. build/artifacts/browser.
# python3 is required for the JSON checks.
set -euo pipefail

fail() {
  echo "verify-browser-diagnostics: $*" >&2
  exit 1
}

command -v python3 >/dev/null 2>&1 || fail "python3 is required"

mode=${1:-}
artifacts=${2:-}

[ -n "$mode" ] && [ -n "$artifacts" ] || fail "usage: $0 failure|pass <artifacts-dir> [report.jsonl]"

case "$mode" in
  failure)
    pattern="Failure_diagnostics_record_console_network_and_focus-*/diagnose/step/attempt-1/step"
    report=${3:-}
    [ -n "$report" ] || fail "failure mode needs the JSONL report"
    [ -f "$report" ] || fail "JSONL report not found: $report"
    ;;
  pass)
    pattern="Diagnostics_are_recorded_on_a_passing_step-*/observe/step/attempt-1/step"
    report=""
    ;;
  *)
    fail "unknown mode: $mode"
    ;;
esac

shopt -s nullglob
dirs=("$artifacts"/$pattern)
shopt -u nullglob

[ "${#dirs[@]}" -eq 1 ] || fail "expected one step directory matching $artifacts/$pattern, found ${#dirs[@]}"

python3 - "$mode" "${dirs[0]}" "$report" <<'PY'
import json
import os
import sys

mode, step_dir, report = sys.argv[1], sys.argv[2], sys.argv[3]
token = "s3cr3t-query-token"
typed = "typed-needle"


def fail(message):
    print("verify-browser-diagnostics: " + message, file=sys.stderr)
    sys.exit(1)


def read(name):
    path = os.path.join(step_dir, name)
    if not os.path.isfile(path):
        fail(name + " missing in " + step_dir)
    with open(path, encoding="utf-8") as handle:
        raw = handle.read()
    if token in raw or "?token=" in raw:
        fail(name + " kept the query string of a recorded URL")
    return raw


def lines(name):
    return [json.loads(line) for line in read(name).splitlines() if line.strip()]


console = lines("console.jsonl")
if not any(e.get("type") == "console" and e.get("level") == "log" and "diagnostics page ready" in e.get("text", "") for e in console):
    fail("console.jsonl lacks the page's console.log: " + json.dumps(console))
if not any(e.get("type") == "exception" and e.get("text") == "Uncaught Error: diagnostics boom" for e in console):
    fail("console.jsonl lacks the uncaught exception: " + json.dumps(console))

network = lines("network.jsonl")
ping = [e for e in network if e.get("url", "").endswith("/web/diagnostics/ping")]
if not any(e.get("type") == "request" and e.get("method") == "GET" and e.get("resource_type") == "Fetch" for e in ping):
    fail("network.jsonl lacks the fetch request: " + json.dumps(network))
if not any(e.get("type") == "response" and e.get("status") == 200 and isinstance(e.get("elapsed_ms"), (int, float)) for e in ping):
    fail("network.jsonl lacks the fetch response with its timing: " + json.dumps(network))

state_raw = read("state.json")
state = json.loads(state_raw)
if typed in state_raw:
    fail("state.json recorded the value typed into the focused field")
if state.get("url", "").split("/")[-2:] != ["web", "diagnostics"]:
    fail("state.json url: " + str(state.get("url")))
viewport = state.get("viewport", {})
if not viewport.get("width") or not viewport.get("height"):
    fail("state.json viewport: " + json.dumps(viewport))
if "scroll" not in state:
    fail("state.json lacks the scroll offsets")

active = state.get("active_element") or {}
if mode == "failure" and active.get("data_testid") != "diagnostics.query":
    fail("state.json active element is not the clicked search field: " + json.dumps(active))

if mode == "failure":
    with open(report, encoding="utf-8") as handle:
        events = [json.loads(line) for line in handle if line.strip()]
    actions = [e for e in events if e.get("type") == "action"]
    if not actions:
        fail("no action event in " + report)
    for e in actions:
        ran = e.get("status") != "skipped"
        if ran != ("started_at" in e and "ended_at" in e):
            fail("action event timing: " + json.dumps(e))
        if e.get("ended_at", "") < e.get("started_at", ""):
            fail("action ended before it started: " + json.dumps(e))
    clicks = [e for e in actions if e.get("kind") == "click" and e.get("status") != "skipped"]
    if not clicks:
        fail("no click action event in " + report)
    for e in clicks:
        point = e.get("point") or {}
        if not isinstance(point.get("x"), (int, float)) or not isinstance(point.get("y"), (int, float)):
            fail("click action event without its point: " + json.dumps(e))

print("verify-browser-diagnostics: " + mode + " OK (" + step_dir + ")")
PY
