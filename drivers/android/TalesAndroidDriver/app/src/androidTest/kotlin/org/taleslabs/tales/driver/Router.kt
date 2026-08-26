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
        val value = payload.stringOrNull("value")
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

    /**
     * Brings an element into view, best-effort.
     *
     * Best-effort is the contract, shared verbatim with the iOS driver:
     * the only failure is a locator that names nothing. A screen with
     * nothing to scroll, or a container that runs out of travel before
     * the element appears, is a 200 — the follow-up tap or input_text is
     * what needs the element, and it reports a far more precise error
     * than this handler could. Returning 404 for those made a scenario's
     * outcome depend on the AVD's screen size, since a Compose container
     * only publishes itself as scrollable once its content overflows.
     */
    private fun scrollTo(payload: Map<String, Any?>): HttpResponse {
        val locator = locatorOf(payload)
        if (locator.isEmpty) return HttpResponse.error(400, "scroll_to requires an element id, label or text")

        val screen = screenBounds()

        // Only used while the locator resolves to nothing, and sticky on
        // purpose: once a container reports it cannot travel any further
        // one way, the other way is the only one left, and re-deriving
        // the guess every round would undo the previous scroll and
        // oscillate in place.
        var blind = ScrollDirection.FORWARD

        var attemptsLeft = SCROLL_TO_ATTEMPTS

        // Why the loop stopped, so a 404 says which of the three
        // endings it is. "not found" alone sends the reader to the app,
        // which is where issue #69 started: the message stated the
        // opposite of what the very next /hierarchy call observed.
        var ending = "out of attempts"

        while (attemptsLeft-- > 0) {
            val node = locators.resolve(locator)

            // Idempotent by contract: scenarios call it before every
            // interaction without first querying UI state.
            if (node != null && isReachable(node, screen)) return HttpResponse.ok()

            // An unresolved locator is not yet a failure: a lazy list
            // does not compose its off-screen rows, so the element
            // genuinely does not exist until something scrolls.
            val scrollable = node?.let { locators.scrollableAncestor(it) } ?: firstScrollable()

            if (scrollable == null) {
                ending = if (node == null) {
                    "nothing on screen can scroll, and the element is not in the tree"
                } else {
                    "the element has no scrollable ancestor"
                }

                break
            }

            val moved = if (node != null) {
                // The tree says where the element is relative to the
                // container that has to move, so the direction is a
                // fact. A container that will not travel that way cannot
                // help — the element is outside its range, and flipping
                // would only scroll away from it.
                scrollable.performAction(scrollActionOf(directionFor(node, boundsOf(scrollable))))
            } else {
                scrollable.performAction(scrollActionOf(blind)) || run {
                    // At its end this way. Flip and keep going: an
                    // element above the viewport is only reachable
                    // backwards, and an unresolved one has no position
                    // to read the direction off.
                    blind = opposite(blind)

                    scrollable.performAction(scrollActionOf(blind))
                }
            }

            device.waitForIdle(SCROLL_SETTLE_MS)

            // The scroll just happened on this thread, so every cached
            // node still describes the screen from before it. Without
            // this the next resolve re-reads the pre-scroll tree and the
            // loop scrolls a lazy list to its end while insisting the
            // rows it realized are not there.
            //
            // This runs even when the container refused to travel, which
            // is what a `break` here used to skip. That refusal is the
            // interesting case: performAction returns false once the
            // container cannot go *further*, which on a scrolling column
            // is the state it reaches as the last section comes into
            // view. Leaving early left the closing resolve reading the
            // tree from before the scroll that revealed the element, so
            // the driver answered 404 for something the very next
            // /hierarchy call held (issue #69).
            automation.invalidateNodeCache()

            if (!moved) {
                ending = "the container cannot travel any further"

                break
            }
        }

        // The verdict is read from a live tree. Every path that scrolled
        // has already invalidated, but the ones that never scrolled have
        // not, and deciding between 200 and 404 on a cached view is what
        // issue #69 was: a refusal for an element the next /hierarchy
        // call held.
        //
        // Doing this here rather than at the top of the handler is
        // deliberate. Refreshing before the *first* resolve changes
        // which branch the loop takes — an off-screen element that used
        // to be invisible to the resolver becomes visible to it, and the
        // known-direction branch runs where blind scrolling used to —
        // and that swap broke a suite that rc.18 scrolled correctly
        // (issue #71). The verdict needs freshness; the loop's opening
        // read does not.
        automation.invalidateNodeCache()

        // The same predicate the loop opens with. Reporting success on
        // the strength of the element merely existing is what let every
        // early exit above answer 200 for something still off screen.
        val settled = locators.resolve(locator)

        if (settled == null || !isReachable(settled, screen)) {
            return HttpResponse.error(404, "element $locator not on screen: $ending")
        }

        return HttpResponse.ok()
    }

    private fun screenBounds(): Bounds = Bounds(0, 0, device.displayWidth, device.displayHeight)

    /** Defers to the encoder so scroll_to and the hierarchy dump never disagree. */
    private fun isOnScreen(node: AccessibilityNodeInfo, screen: Bounds): Boolean =
        HierarchyEncoder.isOnScreen(AccessibilityNode(node), screen)

    /**
     * Whether the element is where a scenario can act on it — that is,
     * whether the hierarchy Tales reads would contain it.
     *
     * [isOnScreen] answers for the node alone, and the encoder drops a
     * whole subtree the moment an ancestor is invisible, so a node can
     * pass on its own while never appearing in a snapshot. scroll_to
     * read that as "already there" and returned 200 without scrolling,
     * for an element that stayed off screen and that the following
     * wait_visible then hunted for until it timed out (issue #71). A
     * false positive is worse than the 404 of issue #69: a refusal
     * fails on the action that caused it, while a wrongful success
     * fails later, somewhere else.
     *
     * Resolution walks the raw tree on purpose — that is what lets the
     * loop see an element it has yet to bring into view — so the
     * ancestor chain has to be checked here rather than left to the
     * resolver.
     */
    private fun isReachable(node: AccessibilityNodeInfo, screen: Bounds): Boolean {
        if (!isOnScreen(node, screen)) return false

        var parent = node.parent

        while (parent != null) {
            if (!parent.isVisibleToUser) return false

            parent = parent.parent
        }

        return true
    }

    private fun boundsOf(node: AccessibilityNodeInfo): Bounds = AccessibilityNode(node).boundsInScreen

    private fun directionFor(node: AccessibilityNodeInfo, viewport: Bounds): ScrollDirection =
        HierarchyEncoder.scrollDirectionFor(boundsOf(node), viewport)

    private fun scrollActionOf(direction: ScrollDirection): Int = when (direction) {
        ScrollDirection.FORWARD -> AccessibilityNodeInfo.ACTION_SCROLL_FORWARD
        ScrollDirection.BACKWARD -> AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD
    }

    private fun opposite(direction: ScrollDirection): ScrollDirection = when (direction) {
        ScrollDirection.FORWARD -> ScrollDirection.BACKWARD
        ScrollDirection.BACKWARD -> ScrollDirection.FORWARD
    }

    private fun firstScrollable(): AccessibilityNodeInfo? {
        for (root in automation.appWindowRoots()) {
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
