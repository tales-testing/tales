#!/usr/bin/env bash
# Wait until an Android device is not merely attached but usable.
#
# `adb devices` reporting "device" only means adbd answered, and
# sys.boot_completed only means init finished. A freshly booted emulator
# goes on scanning packages and running dexopt well past both, and
# driving UI inside that window makes every hierarchy fetch outlast the
# scenarios' own action timeouts. The suite then fails with messages
# naming the element it was looking for, which points nowhere near the
# device that was not ready — one CI run lost six scenarios that way,
# having started the suite two seconds after boot completed.
#
# Run as: scripts/android-wait-ready.sh
#
# Environment:
#   ADB_PATH               adb binary (default: adb)
#   ANDROID_SERIAL         target device (default: the only one attached)
#   ANDROID_READY_TIMEOUT  seconds to wait (default: 180)
set -euo pipefail

adb=${ADB_PATH:-adb}
deadline=${ANDROID_READY_TIMEOUT:-180}

adb_args=()
if [ -n "${ANDROID_SERIAL:-}" ]; then
  adb_args=(-s "$ANDROID_SERIAL")
fi

fail() {
  echo "android-wait-ready: $*" >&2
  echo "Attached devices:" >&2
  "$adb" devices -l >&2 || true
  exit 1
}

# getprop through adb fails in several ways while a device settles (no
# such device, closed transport, empty output), none of which should end
# the wait early — only the deadline does.
prop() {
  "$adb" "${adb_args[@]}" shell getprop "$1" 2>/dev/null | tr -d '\r\n' || true
}

ready() {
  [ "$(prop sys.boot_completed)" = "1" ] || return 1
  [ "$(prop init.svc.bootanim)" = "stopped" ] || return 1

  # The package manager is the last of the three to answer, and it is
  # the one that matters: Tales installs an APK before it drives
  # anything.
  "$adb" "${adb_args[@]}" shell pm path android >/dev/null 2>&1 || return 1

  return 0
}

started=$SECONDS

while ! ready; do
  if [ $((SECONDS - started)) -ge "$deadline" ]; then
    fail "device not ready after ${deadline}s (boot_completed=$(prop sys.boot_completed) bootanim=$(prop init.svc.bootanim))"
  fi

  sleep 2
done

elapsed=$((SECONDS - started))
echo "android-wait-ready: device ready after ${elapsed}s"
