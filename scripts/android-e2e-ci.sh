#!/usr/bin/env bash
# Run the Android e2e suites the way CI wants them: the passing suite
# with one retry, then the failure suite.
#
# This lives in a file rather than inline in the workflow because
# android-emulator-runner runs each line of its `script:` input as a
# separate command. A multi-line `if` there is split apart and the first
# fragment dies with `Syntax error: end of file unexpected`, before the
# emulator has been asked to do anything.
#
# The retry is deliberate, and mirrors the iOS job. What it absorbs is
# the emulator, not the suite: an APK install that takes tens of seconds,
# a system-initiated relaunch of the app under test, or a /hierarchy call
# outlasting its poll deadline all fail a scenario for reasons no
# scenario can prevent. A single attempt is then useless as a merge gate.
# A real regression still fails twice.
#
# The first attempt's reports and artifacts are moved aside rather than
# overwritten: without them a retried-then-green run hides the very
# evidence needed to explain the stall, and a retried-then-red one loses
# half of it.
#
# Run as: scripts/android-e2e-ci.sh
set -euo pipefail

if make e2e-android; then
  make e2e-android-failure
  exit 0
fi

echo "::warning::Android e2e failed on the first attempt, retrying once (first-attempt evidence is under attempt-1/ in the artifacts)"

mkdir -p build/attempt-1

if [ -d build/reports ]; then
  mv build/reports build/attempt-1/reports
fi

if [ -d build/artifacts ]; then
  mv build/artifacts build/attempt-1/artifacts
fi

make e2e-android
make e2e-android-failure
