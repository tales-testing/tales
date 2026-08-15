package org.taleslabs.tales.driver

/** Screen-space bounds, in device pixels. */
data class Bounds(val x: Int, val y: Int, val width: Int, val height: Int) {
    fun toJson(): Map<String, Any?> = linkedMapOf(
        "x" to x,
        "y" to y,
        "width" to width,
        "height" to height,
    )

    val isEmpty: Boolean get() = width <= 0 || height <= 0
}

/**
 * The way a scrollable container has to move to reveal an element.
 *
 * Platform-neutral on purpose: this file stays free of android imports
 * so the encoding and geometry rules — the parts most likely to regress
 * — are covered by plain JVM tests. [Router] maps these onto the
 * matching AccessibilityNodeInfo scroll actions.
 */
enum class ScrollDirection { FORWARD, BACKWARD }

/**
 * One element of the normalised UI tree.
 *
 * The field set and the JSON keys are dictated by the Go side's
 * `tree.ViewNode`, which both platforms decode into. Keeping the two
 * drivers on one shape is what lets the same `.tales` assertions run
 * against iOS and Android unchanged, so this struct should only grow
 * when that shared type does.
 */
data class ViewNode(
    val id: String = "",
    val label: String = "",
    val text: String = "",
    val value: String = "",
    val type: String = "",
    val enabled: Boolean = false,
    val visible: Boolean = false,
    val bounds: Bounds = Bounds(0, 0, 0, 0),
    val children: List<ViewNode> = emptyList(),
) {
    fun toJson(): Map<String, Any?> = linkedMapOf(
        "id" to id,
        "label" to label,
        "text" to text,
        "value" to value,
        "type" to type,
        "enabled" to enabled,
        "visible" to visible,
        "bounds" to bounds.toJson(),
        "children" to children.map { it.toJson() },
    )
}

/**
 * The subset of an accessibility node the encoder reads.
 *
 * AccessibilityNodeInfo is final and awkward to construct off-device, so
 * the encoding rules — which are the interesting part, and the ones most
 * likely to regress — sit behind this interface and are covered by plain
 * JVM tests. The device implementation is a thin adapter.
 */
interface NodeAttributes {
    /** Fully qualified `pkg:id/name`, or "" when the view has no id. */
    val viewIdResourceName: String?
    val contentDescription: CharSequence?
    val text: CharSequence?
    val className: CharSequence?
    val isEnabled: Boolean
    val isVisibleToUser: Boolean
    val isCheckable: Boolean
    val isChecked: Boolean
    val isPassword: Boolean
    val isEditable: Boolean
    val boundsInScreen: Bounds
    val childNodes: List<NodeAttributes>
}

/**
 * Turns a platform accessibility tree into [ViewNode]s.
 *
 * The mapping choices worth knowing:
 *
 *  - `id` is the *short* resource id (`login_button`), not the
 *    fully-qualified `com.example.app:id/login_button`. Scenarios name
 *    elements the way the app's source does, and the package prefix
 *    would make every locator depend on the application id. The Go
 *    resolver also accepts the qualified form, so both work.
 *  - `type` maps Android widget classes onto the same snake_case
 *    vocabulary the iOS driver emits, so a `.tales` expectation reads
 *    identically on both platforms. Compose renders almost everything
 *    as a bare `android.view.View`, which is why locators should prefer
 *    id or text over type on Compose screens.
 *  - `value` mirrors what a user would call the element's current value:
 *    the text for an editable field, "1"/"0" for a checkable one. This
 *    matches XCUIElement.value, which `expect { value }` compares
 *    against.
 */
object HierarchyEncoder {

    /**
     * Encodes [node] and its subtree, clipping bounds to [screen].
     *
     * Invisible subtrees are skipped, with one exception: inside a
     * WebView the platform reports every descendant as not visible to
     * the user, so honouring the flag there would erase all web content
     * from the tree. Once inside one, keep descending.
     */
    fun encode(node: NodeAttributes, screen: Bounds, insideWebView: Boolean = false): ViewNode {
        val className = node.className?.toString().orEmpty()
        val webView = insideWebView || className == WEB_VIEW_CLASS

        val children = node.childNodes
            .filter { it.isVisibleToUser || webView }
            .map { encode(it, screen, webView) }

        return ViewNode(
            id = shortResourceId(node.viewIdResourceName),
            label = node.contentDescription?.toString().orEmpty(),
            text = node.text?.toString().orEmpty(),
            value = valueOf(node),
            type = typeOf(className, node),
            enabled = node.isEnabled,
            visible = isOnScreen(node, screen),
            bounds = clip(node.boundsInScreen, screen),
            children = children,
        )
    }

    /**
     * `com.example.app:id/login_button` -> `login_button`.
     *
     * Compose's `Modifier.testTag` surfaces here too, but only when the
     * app opts in with `testTagsAsResourceId = true`; without it Compose
     * tags are invisible to the accessibility layer entirely.
     */
    fun shortResourceId(raw: String?): String {
        if (raw.isNullOrEmpty()) return ""

        val slash = raw.lastIndexOf('/')

        return if (slash >= 0 && slash < raw.length - 1) raw.substring(slash + 1) else raw
    }

    private fun valueOf(node: NodeAttributes): String = when {
        node.isCheckable -> if (node.isChecked) "1" else "0"
        node.isEditable -> node.text?.toString().orEmpty()
        else -> ""
    }

    /**
     * Visible means "the platform says so and it occupies screen space".
     * A zero-area node passes isVisibleToUser often enough (collapsed
     * containers, off-screen recycler items being measured) that taking
     * the flag alone would have Tales tap at a degenerate point.
     *
     * This is the driver's *only* definition of on-screen: the encoder
     * stamps it onto every node as `visible`, and `scroll_to` decides
     * whether it still has work to do with it. They used to disagree —
     * scroll_to demanded full vertical containment — so an element the
     * hierarchy dump reported as visible was "off screen" to scroll_to,
     * which then hunted for a container to scroll and 404ed when the
     * screen had none.
     */
    fun isOnScreen(node: NodeAttributes, screen: Bounds): Boolean =
        node.isVisibleToUser && !clip(node.boundsInScreen, screen).isEmpty

    /**
     * Which way a scrollable has to move to bring [bounds] into view.
     *
     * [viewport] is the scrolling container's own bounds, not the
     * screen. A row sitting just above a list that starts halfway down
     * the display is still at a positive screen y, so comparing against
     * the screen would call it "below" and scroll away from it.
     *
     * Only an element that starts above the viewport needs backward
     * scrolling; everything else is either below it or already visible,
     * and the caller returns before scrolling in the latter case.
     * Scrolling forward unconditionally — which the driver used to do —
     * makes an element above the fold unreachable, because every attempt
     * moves it further away.
     */
    fun scrollDirectionFor(bounds: Bounds, viewport: Bounds): ScrollDirection =
        if (bounds.y < viewport.y) ScrollDirection.BACKWARD else ScrollDirection.FORWARD

    /** Intersects with the screen, so off-screen nodes report empty bounds. */
    fun clip(bounds: Bounds, screen: Bounds): Bounds {
        val left = maxOf(bounds.x, screen.x)
        val top = maxOf(bounds.y, screen.y)
        val right = minOf(bounds.x + bounds.width, screen.x + screen.width)
        val bottom = minOf(bounds.y + bounds.height, screen.y + screen.height)

        if (right <= left || bottom <= top) return Bounds(0, 0, 0, 0)

        return Bounds(left, top, right - left, bottom - top)
    }

    /**
     * Maps an Android widget class onto the iOS driver's element-type
     * vocabulary. Unknown classes fall back to "other", matching
     * XCUIElementType.other, rather than leaking the Java class name
     * into a cross-platform field.
     */
    fun typeOf(className: String, node: NodeAttributes): String {
        val simple = className.substringAfterLast('.')

        return when {
            simple == "EditText" || node.isEditable ->
                if (node.isPassword) "secure_text_field" else "text_field"

            simple == "Button" || simple == "ImageButton" || simple == "AppCompatButton" -> "button"
            simple == "TextView" || simple == "AppCompatTextView" -> "static_text"
            simple == "ImageView" || simple == "AppCompatImageView" -> "image"
            simple == "Switch" || simple == "SwitchCompat" || simple == "CheckBox" ||
                simple == "RadioButton" || simple == "ToggleButton" -> "switch"

            simple == "RecyclerView" || simple == "GridView" -> "collection_view"
            simple == "ListView" -> "table"
            simple == "ScrollView" || simple == "NestedScrollView" || simple == "HorizontalScrollView" -> "scroll_view"
            simple == "ProgressBar" -> "activity_indicator"
            simple == "Toolbar" || simple == "ActionBar" -> "toolbar"
            simple == "TabWidget" || simple == "TabLayout" -> "tab_bar"
            simple == "WebView" -> "web_view"
            simple == "AlertDialog" || simple == "Dialog" -> "alert"
            else -> "other"
        }
    }

    private const val WEB_VIEW_CLASS = "android.webkit.WebView"
}
