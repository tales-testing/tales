package org.taleslabs.tales.driver

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** A hand-built accessibility node, so encoding is testable off-device. */
private data class FakeNode(
    override val viewIdResourceName: String? = null,
    override val contentDescription: CharSequence? = null,
    override val text: CharSequence? = null,
    override val className: CharSequence? = "android.view.View",
    override val isEnabled: Boolean = true,
    override val isVisibleToUser: Boolean = true,
    override val isCheckable: Boolean = false,
    override val isChecked: Boolean = false,
    override val isPassword: Boolean = false,
    override val isEditable: Boolean = false,
    override val boundsInScreen: Bounds = Bounds(0, 0, 100, 50),
    override val childNodes: List<NodeAttributes> = emptyList(),
) : NodeAttributes

private val SCREEN = Bounds(0, 0, 1080, 1920)

class HierarchyEncoderTest {

    @Test
    fun `exposes the short resource id`() {
        // Scenarios name elements the way the app's source does; the
        // package prefix would tie every locator to the application id.
        assertEquals("login_button", HierarchyEncoder.shortResourceId("com.example.app:id/login_button"))
        assertEquals("", HierarchyEncoder.shortResourceId(null))
        assertEquals("", HierarchyEncoder.shortResourceId(""))
        assertEquals("bare", HierarchyEncoder.shortResourceId("bare"))
    }

    @Test
    fun `maps widget classes onto the shared type vocabulary`() {
        val cases = mapOf(
            "android.widget.Button" to "button",
            "android.widget.TextView" to "static_text",
            "android.widget.ImageView" to "image",
            "android.widget.Switch" to "switch",
            "androidx.recyclerview.widget.RecyclerView" to "collection_view",
            "android.widget.ListView" to "table",
            "android.widget.ScrollView" to "scroll_view",
            "android.webkit.WebView" to "web_view",
            "android.view.View" to "other",
        )

        for ((className, want) in cases) {
            val got = HierarchyEncoder.typeOf(className, FakeNode(className = className))
            assertEquals("class $className", want, got)
        }
    }

    @Test
    fun `reports a password field as a secure text field`() {
        // The provider keys its paste-mode input and its empty-field
        // clear_text no-op off this exact type string, shared with iOS.
        val node = FakeNode(className = "android.widget.EditText", isEditable = true, isPassword = true)

        assertEquals("secure_text_field", HierarchyEncoder.typeOf("android.widget.EditText", node))
    }

    @Test
    fun `derives value from editable text and checkable state`() {
        val field = FakeNode(className = "android.widget.EditText", isEditable = true, text = "user@example.com")
        assertEquals("user@example.com", HierarchyEncoder.encode(field, SCREEN).value)

        val checked = FakeNode(className = "android.widget.Switch", isCheckable = true, isChecked = true)
        assertEquals("1", HierarchyEncoder.encode(checked, SCREEN).value)

        val unchecked = FakeNode(className = "android.widget.Switch", isCheckable = true, isChecked = false)
        assertEquals("0", HierarchyEncoder.encode(unchecked, SCREEN).value)

        val plain = FakeNode(className = "android.widget.TextView", text = "Hello")
        assertEquals("", HierarchyEncoder.encode(plain, SCREEN).value)
    }

    @Test
    fun `clips bounds to the screen`() {
        val node = FakeNode(boundsInScreen = Bounds(1000, 1900, 400, 400))

        assertEquals(Bounds(1000, 1900, 80, 20), HierarchyEncoder.encode(node, SCREEN).bounds)
    }

    @Test
    fun `treats a fully off-screen node as not visible`() {
        // isVisibleToUser alone is not enough: collapsed containers and
        // recycler items being measured report true with zero area, and
        // Tales would tap a degenerate point.
        val node = FakeNode(boundsInScreen = Bounds(2000, 3000, 100, 100))

        val encoded = HierarchyEncoder.encode(node, SCREEN)

        assertFalse(encoded.visible)
        assertTrue(encoded.bounds.isEmpty)
    }

    @Test
    fun `skips invisible children`() {
        val root = FakeNode(
            childNodes = listOf(
                FakeNode(viewIdResourceName = "app:id/shown"),
                FakeNode(viewIdResourceName = "app:id/hidden", isVisibleToUser = false),
            ),
        )

        val encoded = HierarchyEncoder.encode(root, SCREEN)

        assertEquals(1, encoded.children.size)
        assertEquals("shown", encoded.children[0].id)
    }

    @Test
    fun `keeps descending inside a WebView despite the visibility flag`() {
        // The platform reports every WebView descendant as not visible
        // to the user; honouring that would erase all web content.
        val root = FakeNode(
            className = "android.webkit.WebView",
            childNodes = listOf(
                FakeNode(viewIdResourceName = "app:id/web_child", isVisibleToUser = false),
            ),
        )

        val encoded = HierarchyEncoder.encode(root, SCREEN)

        assertEquals(1, encoded.children.size)
        assertEquals("web_child", encoded.children[0].id)
    }

    @Test
    fun `serialises the shape the Go tree decoder expects`() {
        val node = FakeNode(
            viewIdResourceName = "com.example.app:id/welcome_signin",
            contentDescription = "Sign in",
            text = "Sign in",
            className = "android.widget.Button",
            boundsInScreen = Bounds(10, 20, 100, 40),
        )

        val json = Json.write(HierarchyEncoder.encode(node, SCREEN).toJson())

        assertEquals(
            """{"id":"welcome_signin","label":"Sign in","text":"Sign in","value":"","type":"button",""" +
                """"enabled":true,"visible":true,"bounds":{"x":10,"y":20,"width":100,"height":40},"children":[]}""",
            json,
        )
    }
}
