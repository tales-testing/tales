package org.taleslabs.tales.driver

import android.app.Instrumentation
import android.graphics.Rect
import android.os.SystemClock
import android.view.KeyEvent
import android.view.accessibility.AccessibilityNodeInfo
import androidx.test.uiautomator.UiDevice
import java.io.ByteArrayOutputStream

/**
 * Maps the Tales driver's HTTP contract onto UiAutomator.
 *
 * Route names, payload keys and status codes are shared verbatim with
 * the XCUITest driver: that is what lets one Go client and one set of
 * `.tales` assertions serve both platforms. Any change here has to land
 * on the Swift side too.
 *
 * `bundleId` on the wire carries the Android package name. The field
 * kept its iOS name so the shared client needs no per-platform payload.
 */
class Router(
    private val instrumentation: Instrumentation,
    private val device: UiDevice,
) {
    private val automation = instrumentation.uiAutomation
    private val snapshots = SnapshotService(device, automation)
    private val locators = LocatorResolver(automation)
    private val text = TextInput(device, locators)

    fun handle(request: HttpRequest): HttpResponse = when {
        request.method == "GET" && request.path == "/health" -> HttpResponse.json(200, mapOf("status" to "ok"))
        request.method == "GET" && request.path == "/hierarchy" -> hierarchy(request)
        request.method == "GET" && request.path == "/screenshot" -> screenshot()
        request.method == "POST" && request.path == "/tap" -> tap(body(request))
        request.method == "POST" && request.path == "/doubleTap" -> doubleTap(body(request))
        request.method == "POST" && request.path == "/longPress" -> longPress(body(request))
        request.method == "POST" && request.path == "/swipe" -> swipe(body(request))
        request.method == "POST" && request.path == "/pressKey" -> pressKey(body(request))
        request.method == "POST" && request.path == "/pressButton" -> pressButton(body(request))
        request.method == "POST" && request.path == "/orientation" -> orientation(body(request))
        request.method == "POST" && request.path == "/inputText" -> inputText(body(request))
        request.method == "POST" && request.path == "/eraseText" -> eraseText(body(request))
        request.method == "POST" && request.path == "/dismissKeyboard" -> dismissKeyboard()
        request.method == "POST" && request.path == "/scrollTo" -> scrollTo(body(request))
        request.method == "POST" && request.path == "/launch" -> launch(body(request))
        request.method == "POST" && request.path == "/terminate" -> terminate(body(request))
        else -> HttpResponse.error(404, "route not found")
    }

    private fun body(request: HttpRequest): Map<String, Any?> = Json.parseObject(request.body)

    private fun locatorOf(payload: Map<String, Any?>): Locator = Locator(
        id = payload.stringOr("id"),
        label = payload.stringOr("label"),
        text = payload.stringOr("text"),
    )

    // -- hierarchy & screenshot ----------------------------------------

    private fun hierarchy(request: HttpRequest): HttpResponse {
        if (request.query["bundleId"].isNullOrEmpty()) {
            return HttpResponse.error(400, "bundleId is required")
        }

        return try {
            HttpResponse.json(200, snapshots.capture().toJson())
        } catch (e: SnapshotBusyException) {
            // 503 rather than 500: the host's poll loop retries on it,
            // so a momentary stall costs time instead of the step.
            HttpResponse.error(503, e.message ?: "snapshot busy; retry")
        }
    }

    private fun screenshot(): HttpResponse {
        // takeScreenshot returns null while the window is being
        // reconfigured (rotation, an activity swap). A short retry
        // converts a transient null into a slightly slower capture.
        repeat(SCREENSHOT_ATTEMPTS) { attempt ->
            val bitmap = automation.takeScreenshot()

            if (bitmap != null) {
                val out = ByteArrayOutputStream()
                bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, out)
                bitmap.recycle()

                return HttpResponse.png(out.toByteArray())
            }

            SystemClock.sleep(SCREENSHOT_RETRY_MS * (attempt + 1))
        }

        return HttpResponse.error(503, "screenshot unavailable; window may not be ready")
    }

    // -- pointer gestures ----------------------------------------------

    /**
     * Resolves the request's target to a point.
     *
     * A locator is re-resolved against the live tree so the tap lands on
     * where the element is *now*; the host-computed coordinates are the
     * fallback for locator-less requests and for elements that have
     * since left the tree.
     */
    private fun pointFor(payload: Map<String, Any?>): Pair<Int, Int>? {
        val locator = locatorOf(payload)

        if (!locator.isEmpty) {
            locators.resolve(locator)?.let { node ->
                val rect = Rect()
                node.getBoundsInScreen(rect)

                if (!rect.isEmpty) return rect.centerX() to rect.centerY()
            }
        }

        val x = payload.intOrNull("x") ?: return null
        val y = payload.intOrNull("y") ?: return null

        return x to y
    }

    private fun tap(payload: Map<String, Any?>): HttpResponse {
        val (x, y) = pointFor(payload) ?: return HttpResponse.error(400, "expected {bundleId, x, y}")

        device.click(x, y)

        return HttpResponse.ok()
    }

    private fun doubleTap(payload: Map<String, Any?>): HttpResponse {
        val (x, y) = pointFor(payload) ?: return HttpResponse.error(400, "expected {bundleId, x, y}")

        device.click(x, y)
        SystemClock.sleep(DOUBLE_TAP_GAP_MS)
        device.click(x, y)

        return HttpResponse.ok()
    }

    private fun longPress(payload: Map<String, Any?>): HttpResponse {
        val (x, y) = pointFor(payload) ?: return HttpResponse.error(400, "expected {bundleId, x, y}")

        val seconds = payload.doubleOrNull("duration") ?: DEFAULT_LONG_PRESS_SECONDS

        // A swipe that never moves is a press-and-hold. UiAutomator has
        // no press-with-duration primitive, and its step count is the
        // only exposed way to control how long the gesture takes.
        device.swipe(x, y, x, y, stepsFor(seconds))

        return HttpResponse.ok()
    }

    private fun swipe(payload: Map<String, Any?>): HttpResponse {
        val startX = payload.intOrNull("startX")
        val startY = payload.intOrNull("startY")
        val endX = payload.intOrNull("endX")
        val endY = payload.intOrNull("endY")

        if (startX == null || startY == null || endX == null || endY == null) {
            return HttpResponse.error(400, "expected {startX, startY, endX, endY}")
        }

        val seconds = payload.doubleOrNull("duration") ?: DEFAULT_SWIPE_SECONDS

        device.swipe(startX, startY, endX, endY, stepsFor(seconds))

        return HttpResponse.ok()
    }

    /**
     * UiAutomator paces a gesture in steps of roughly 5ms, so the step
     * count is how a duration is expressed. Clamped to at least one step
     * so a zero duration still produces a gesture.
     */
    private fun stepsFor(seconds: Double): Int = maxOf(1, (seconds * 1000 / MS_PER_STEP).toInt())

    // -- device-level actions ------------------------------------------

    private fun pressKey(payload: Map<String, Any?>): HttpResponse {
        val key = payload.stringOr("key")
        if (key.isEmpty()) return HttpResponse.error(400, "expected {key}")

        val code = KEY_CODES[key] ?: return HttpResponse.error(
            400,
            "unsupported key \"$key\" (supported: ${KEY_CODES.keys.sorted().joinToString(", ")})",
        )

        device.pressKeyCode(code)

        return HttpResponse.ok()
    }

    private fun pressButton(payload: Map<String, Any?>): HttpResponse {
        val button = payload.stringOr("button")
        if (button.isEmpty()) return HttpResponse.error(400, "expected {button}")

        val code = BUTTON_CODES[button] ?: return HttpResponse.error(
            400,
            "button \"$button\" is not supported on android " +
                "(supported: ${BUTTON_CODES.keys.sorted().joinToString(", ")})",
        )

        device.pressKeyCode(code)

        return HttpResponse.ok()
    }

    private fun orientation(payload: Map<String, Any?>): HttpResponse {
        return when (val orientation = payload.stringOr("orientation")) {
            "" -> HttpResponse.error(400, "expected {orientation}")
            "portrait" -> {
                device.setOrientationNatural()
                HttpResponse.ok()
            }

            "landscape_left" -> {
                device.setOrientationLeft()
                HttpResponse.ok()
            }

            "landscape_right" -> {
                device.setOrientationRight()
                HttpResponse.ok()
            }

            // Android exposes the rotation but almost no app declares
            // support for it, so the request would appear to succeed
            // while nothing rotated. Failing loudly is more useful.
            "upside_down" -> HttpResponse.error(
                501,
                "orientation \"upside_down\" is not supported on android " +
                    "(supported: portrait, landscape_left, landscape_right)",
            )

            else -> HttpResponse.error(
                400,
                "unsupported orientation \"$orientation\" " +
                    "(supported: portrait, landscape_left, landscape_right)",
            )
        }
    }

    // -- text ----------------------------------------------------------

    private fun inputText(payload: Map<String, Any?>): HttpResponse {
        val value = payload.stringOrNull("text")
            ?: return HttpResponse.error(400, "expected {bundleId, text}")

        return text.input(locatorOf(payload), value)
    }

    private fun eraseText(payload: Map<String, Any?>): HttpResponse {
        val characters = payload.intOrNull("characters")
            ?: return HttpResponse.error(400, "expected {bundleId, characters}")

        return text.erase(locatorOf(payload), characters)
    }

    private fun dismissKeyboard(): HttpResponse {
        // Idempotent by contract: scenarios call it defensively before a
        // snapshot-heavy step without first querying UI state.
        if (isKeyboardShown()) {
            device.pressBack()
            device.waitForIdle(KEYBOARD_SETTLE_MS)
        }

        return HttpResponse.ok()
    }

    /**
     * Reports whether an IME is currently on screen.
     *
     * There is no public API for this, so read it off the window list:
     * the IME lives in a window of type TYPE_INPUT_METHOD.
     */
    private fun isKeyboardShown(): Boolean =
        automation.windows.any { it.type == android.view.accessibility.AccessibilityWindowInfo.TYPE_INPUT_METHOD }

    private fun scrollTo(payload: Map<String, Any?>): HttpResponse {
        val locator = locatorOf(payload)
        if (locator.isEmpty) return HttpResponse.error(400, "scroll_to requires an element id, label or text")

        // Already on screen: nothing to do. The contract is idempotent,
        // so scenarios can call it before every interaction.
        locators.resolve(locator)?.let { node ->
            if (isOnScreen(node)) return HttpResponse.ok()
        }

        repeat(SCROLL_TO_ATTEMPTS) {
            val node = locators.resolve(locator)

            if (node != null && isOnScreen(node)) return HttpResponse.ok()

            val scrollable = node?.let { locators.scrollableAncestor(it) } ?: firstScrollable()
                ?: return HttpResponse.error(404, "element $locator not found and no scrollable container to search")

            if (!scrollable.performAction(AccessibilityNodeInfo.ACTION_SCROLL_FORWARD)) {
                return@repeat
            }

            device.waitForIdle(SCROLL_SETTLE_MS)
        }

        val node = locators.resolve(locator)
            ?: return HttpResponse.error(404, "element $locator not found")

        return if (isOnScreen(node)) {
            HttpResponse.ok()
        } else {
            HttpResponse.error(404, "element $locator could not be scrolled into view")
        }
    }

    private fun isOnScreen(node: AccessibilityNodeInfo): Boolean {
        val rect = Rect()
        node.getBoundsInScreen(rect)

        if (rect.isEmpty) return false

        return rect.top >= 0 && rect.bottom <= device.displayHeight
    }

    private fun firstScrollable(): AccessibilityNodeInfo? {
        for (root in automation.windowRoots()) {
            findScrollable(root)?.let { return it }
        }

        return null
    }

    private fun findScrollable(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        if (node.isScrollable) return node

        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            findScrollable(child)?.let { return it }
        }

        return null
    }

    // -- app lifecycle -------------------------------------------------

    private fun launch(payload: Map<String, Any?>): HttpResponse {
        val packageName = payload.stringOr("bundleId")
        if (packageName.isEmpty()) return HttpResponse.error(400, "expected {bundleId}")

        val intent = instrumentation.targetContext.packageManager
            .getLaunchIntentForPackage(packageName)
            ?: return HttpResponse.error(404, "no launch intent for package \"$packageName\" (is it installed?)")

        // CLEAR_TASK gives every launch the same starting point, which
        // is what a scenario expects from `launch { }`; without it a
        // relaunch would resume wherever the previous scenario left the
        // back stack.
        intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK or android.content.Intent.FLAG_ACTIVITY_CLEAR_TASK)
        instrumentation.targetContext.startActivity(intent)

        device.wait(androidx.test.uiautomator.Until.hasObject(androidx.test.uiautomator.By.pkg(packageName)), LAUNCH_TIMEOUT_MS)

        return HttpResponse.ok()
    }

    private fun terminate(payload: Map<String, Any?>): HttpResponse {
        val packageName = payload.stringOr("bundleId")
        if (packageName.isEmpty()) return HttpResponse.error(400, "expected {bundleId}")

        // force-stop rather than an intent: the app must be gone, not
        // backgrounded, so the next scenario's launch starts cold.
        device.executeShellCommand("am force-stop $packageName")

        return HttpResponse.ok()
    }

    private companion object {
        const val DOUBLE_TAP_GAP_MS = 80L
        const val DEFAULT_LONG_PRESS_SECONDS = 1.0
        const val DEFAULT_SWIPE_SECONDS = 0.3
        const val MS_PER_STEP = 5.0
        const val SCREENSHOT_ATTEMPTS = 3
        const val SCREENSHOT_RETRY_MS = 200L
        const val KEYBOARD_SETTLE_MS = 500L
        const val SCROLL_TO_ATTEMPTS = 10
        const val SCROLL_SETTLE_MS = 300L
        const val LAUNCH_TIMEOUT_MS = 10_000L

        /**
         * The key names are the DSL's, shared with iOS. Android has a
         * keycode for each, so `press_key` behaves identically on both
         * platforms.
         */
        val KEY_CODES = mapOf(
            "return" to KeyEvent.KEYCODE_ENTER,
            "enter" to KeyEvent.KEYCODE_ENTER,
            "tab" to KeyEvent.KEYCODE_TAB,
            "space" to KeyEvent.KEYCODE_SPACE,
            "escape" to KeyEvent.KEYCODE_ESCAPE,
            "delete" to KeyEvent.KEYCODE_DEL,
        )

        /**
         * `lock` is iOS' name for the power button and is accepted as an
         * alias so a cross-platform scenario can lock the device with
         * one word. `back` and `recent_apps` have no iOS counterpart.
         */
        val BUTTON_CODES = mapOf(
            "home" to KeyEvent.KEYCODE_HOME,
            "back" to KeyEvent.KEYCODE_BACK,
            "recent_apps" to KeyEvent.KEYCODE_APP_SWITCH,
            "power" to KeyEvent.KEYCODE_POWER,
            "lock" to KeyEvent.KEYCODE_POWER,
            "volume_up" to KeyEvent.KEYCODE_VOLUME_UP,
            "volume_down" to KeyEvent.KEYCODE_VOLUME_DOWN,
        )
    }
}
