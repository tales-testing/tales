package org.taleslabs.tales.driver

import android.app.UiAutomation
import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo

/**
 * Adapts a live [AccessibilityNodeInfo] to the encoder's view of a node.
 *
 * Children are materialised lazily per access so a deep tree is walked
 * once, and nulls from the platform (recycled or detached nodes) are
 * dropped rather than crashing the snapshot.
 */
class AccessibilityNode(private val node: AccessibilityNodeInfo) : NodeAttributes {
    override val viewIdResourceName: String? get() = node.viewIdResourceName
    override val contentDescription: CharSequence? get() = node.contentDescription
    override val text: CharSequence? get() = node.text
    override val className: CharSequence? get() = node.className
    override val isEnabled: Boolean get() = node.isEnabled
    override val isVisibleToUser: Boolean get() = node.isVisibleToUser
    override val isCheckable: Boolean get() = node.isCheckable
    override val isChecked: Boolean get() = node.isChecked
    override val isPassword: Boolean get() = node.isPassword
    override val isEditable: Boolean get() = node.isEditable

    override val boundsInScreen: Bounds
        get() {
            val rect = Rect()
            node.getBoundsInScreen(rect)

            return Bounds(rect.left, rect.top, rect.width(), rect.height())
        }

    override val childNodes: List<NodeAttributes>
        get() = (0 until node.childCount)
            .mapNotNull { node.getChild(it) }
            .map { AccessibilityNode(it) }
}

/**
 * Collects the root node of every window the platform exposes.
 *
 * Going through [UiAutomation.getWindows] rather than
 * `rootInActiveWindow` is what makes dialogs, popups, toasts and the IME
 * visible to a snapshot: those live in their own windows, and the active
 * one only ever holds the app's own hierarchy. It is public API, unlike
 * the reflection-on-getWindowRoots trick the ecosystem often reaches
 * for, so it does not break when the platform's internals move.
 *
 * Falls back to the active window when the window list is empty, which
 * happens briefly between an app terminating and the next one drawing.
 */
fun UiAutomation.windowRoots(): List<AccessibilityNodeInfo> {
    val roots = windows.mapNotNull { it.root }

    if (roots.isNotEmpty()) return roots

    return listOfNotNull(rootInActiveWindow)
}

/**
 * Drops the platform's cached copies of every node.
 *
 * AccessibilityInteractionClient hands out snapshots of nodes and keeps
 * them cached, so a tree walk that follows an action the driver itself
 * performed reports the state from before it. Reassigning serviceInfo is
 * the documented lever for clearing that cache.
 *
 * Anything that changes the screen and then re-reads it without a
 * snapshot in between has to call this, or it reads its own stale view:
 * scroll_to scrolled a lazy list correctly for ten attempts and never
 * saw the rows it had just realized, then reported the element missing.
 */
fun UiAutomation.invalidateNodeCache() {
    serviceInfo = serviceInfo
}

/**
 * Like [windowRoots], minus the windows the app under test does not own.
 *
 * Scrolling has to stay inside the app: the IME's suggestion strip and
 * the notification shade are both scrollable, come first in the window
 * list often enough to matter, and scrolling either of them looks to a
 * scenario like a scroll that silently did nothing. Snapshots and
 * locator resolution deliberately keep the full list — a dialog or a
 * toast is content a scenario asserts on.
 */
fun UiAutomation.appWindowRoots(): List<AccessibilityNodeInfo> {
    val roots = windows
        .filter { it.type != AccessibilityWindowInfo.TYPE_INPUT_METHOD && it.type != AccessibilityWindowInfo.TYPE_SYSTEM }
        .mapNotNull { it.root }

    if (roots.isNotEmpty()) return roots

    return listOfNotNull(rootInActiveWindow)
}
