package org.taleslabs.tales.driver

/**
 * Driver logging.
 *
 * Everything goes to logcat under a single tag. An app process' stdout
 * and stderr are discarded on Android unless the device is reconfigured,
 * and `am instrument` only forwards the test runner's own status stream,
 * so logcat is the one channel that reliably survives. The Go backend
 * captures `adb logcat -s tales-driver` for the session and attaches it
 * as a diagnostic artifact when the driver dies mid-scenario.
 *
 * Messages carry a `[tales-driver]` prefix to match the iOS driver's
 * format, so the two platforms' logs read the same way. Every non-health
 * route emits a request/response pair: when the runner dies, the last
 * request without a matching response is the one that killed it.
 */
object Log {
    const val TAG = "tales-driver"

    fun i(message: String) {
        android.util.Log.i(TAG, "[tales-driver] $message")
    }

    fun e(message: String) {
        android.util.Log.e(TAG, "[tales-driver] $message")
    }
}
