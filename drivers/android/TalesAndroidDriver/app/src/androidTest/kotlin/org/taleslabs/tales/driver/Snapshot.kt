package org.taleslabs.tales.driver

import android.app.UiAutomation
import androidx.test.uiautomator.UiDevice
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicBoolean

/** Raised when a snapshot is already running or overran its budget. */
class SnapshotBusyException(message: String) : RuntimeException(message)

/**
 * Captures the UI tree, bounded in time and limited to one at a time.
 *
 * Two failure modes motivate the machinery. A snapshot walks the whole
 * accessibility tree, which blocks on the app under test answering; an
 * app busy doing synchronous work can stall it indefinitely. And Tales
 * polls the hierarchy every 250ms, so a slow snapshot would otherwise
 * pile requests onto the same tree walk until the driver wedged.
 *
 * So: one snapshot in flight at a time, an 8s ceiling, and a retryable
 * 503 in either case. The Go provider's poll loop absorbs a 503 and
 * comes back, which turns a momentary stall into a slower step instead
 * of a failed one.
 */
class SnapshotService(
    private val device: UiDevice,
    private val automation: UiAutomation,
) {
    private val inFlight = AtomicBoolean(false)
    private val executor = Executors.newSingleThreadExecutor { r ->
        Thread(r, "tales-driver-snapshot").apply { isDaemon = true }
    }

    /**
     * Returns the current tree, rooted in a synthetic node holding one
     * child per window. The root exists because the Go tree model is
     * single-rooted while Android exposes several windows.
     */
    fun capture(): ViewNode {
        if (!inFlight.compareAndSet(false, true)) {
            throw SnapshotBusyException("snapshot busy; retry")
        }

        val task = executor.submit<ViewNode> {
            try {
                captureNow()
            } finally {
                inFlight.set(false)
            }
        }

        return try {
            task.get(TIMEOUT_MS, TimeUnit.MILLISECONDS)
        } catch (e: TimeoutException) {
            // Deliberately leave inFlight set: the walk is still running
            // and starting a second one would compound the stall. The
            // finally block above clears it when the first one lands.
            throw SnapshotBusyException("snapshot timed out after ${TIMEOUT_MS}ms; retry")
        }
    }

    private fun captureNow(): ViewNode {
        refreshAccessibilityCache()

        val screen = Bounds(0, 0, device.displayWidth, device.displayHeight)
        val roots = automation.windowRoots()

        val windows = roots.map { HierarchyEncoder.encode(AccessibilityNode(it), screen) }

        return ViewNode(
            id = "",
            type = "application",
            enabled = true,
            visible = true,
            bounds = screen,
            children = windows,
        )
    }

    /**
     * Drops the platform's cached node copies before walking the tree.
     *
     * AccessibilityInteractionClient hands out snapshots of nodes and
     * keeps them cached; without invalidating, a walk right after a tap
     * can report the pre-tap state and make Tales assert against a
     * screen that no longer exists. Resetting serviceInfo is the
     * documented lever for that. The bounded waitForIdle in front of it
     * lets an in-flight frame settle without ever blocking indefinitely
     * — the implicit UiAutomator waits are disabled precisely because
     * unbounded idle waits hang on apps that animate continuously.
     */
    private fun refreshAccessibilityCache() {
        device.waitForIdle(IDLE_WAIT_MS)
        automation.serviceInfo = automation.serviceInfo
    }

    private companion object {
        const val TIMEOUT_MS = 8_000L
        const val IDLE_WAIT_MS = 500L
    }
}
