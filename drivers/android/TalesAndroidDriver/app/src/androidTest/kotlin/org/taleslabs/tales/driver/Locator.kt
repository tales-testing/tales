package org.taleslabs.tales.driver

import android.app.UiAutomation
import android.view.accessibility.AccessibilityNodeInfo

/**
 * An element locator as it arrives on the wire.
 *
 * Exactly one of the three is set on a well-formed request — the parser
 * enforces that — but the driver stays tolerant and applies them in the
 * order below, matching the Go resolver so both sides agree on which
 * element a request names.
 */
data class Locator(val id: String = "", val label: String = "", val text: String = "") {
    val isEmpty: Boolean get() = id.isEmpty() && label.isEmpty() && text.isEmpty()

    /** Renders the locator the way error messages should cite it. */
    override fun toString(): String = when {
        label.isNotEmpty() -> "label=\"$label\""
        text.isNotEmpty() -> "text=\"$text\""
        else -> "id=\"$id\""
    }
}

/**
 * Resolves locators against the live accessibility tree.
 *
 * Actions resolve here rather than trusting the coordinates the host
 * computed from its last snapshot: between that snapshot and the action
 * the screen may have moved, and tapping stale coordinates is the
 * classic source of flaky UI tests. Coordinates remain the fallback for
 * requests that carry no locator.
 */
class LocatorResolver(private val automation: UiAutomation) {

    /** Returns the first matching node in pre-order, or null. */
    fun resolve(locator: Locator): AccessibilityNodeInfo? {
        if (locator.isEmpty) return null

        for (root in automation.windowRoots()) {
            findIn(root, locator)?.let { return it }
        }

        return null
    }

    private fun findIn(node: AccessibilityNodeInfo, locator: Locator): AccessibilityNodeInfo? {
        if (matches(node, locator)) return node

        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            findIn(child, locator)?.let { return it }
        }

        return null
    }

    private fun matches(node: AccessibilityNodeInfo, locator: Locator): Boolean = when {
        locator.label.isNotEmpty() -> node.contentDescription?.toString() == locator.label
        locator.text.isNotEmpty() -> node.text?.toString() == locator.text
        // Accept both the short id a scenario writes and the fully
        // qualified form the platform reports, so a locator does not
        // have to hard-code the application id.
        else -> {
            val raw = node.viewIdResourceName
            raw == locator.id || HierarchyEncoder.shortResourceId(raw) == locator.id
        }
    }

    /** Walks up to the nearest scrollable ancestor, or null. */
    fun scrollableAncestor(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
        var current: AccessibilityNodeInfo? = node.parent

        while (current != null) {
            if (current.isScrollable) return current
            current = current.parent
        }

        return null
    }
}
