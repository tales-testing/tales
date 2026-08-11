package org.taleslabs.tales.driver

import android.os.Bundle
import android.view.KeyEvent
import android.view.accessibility.AccessibilityNodeInfo
import androidx.test.uiautomator.UiDevice

/**
 * Text entry and erasure.
 *
 * The primary path sets the field's value through the accessibility
 * action rather than synthesising keystrokes. Keystroke synthesis is
 * what the ecosystem commonly does, and it costs dearly: each character
 * needs a keycode, so non-ASCII input is simply unrepresentable, and the
 * per-character pacing makes a long field take seconds. ACTION_SET_TEXT
 * is atomic, handles any Unicode, and does not depend on an IME being
 * up. Keystrokes remain the fallback for the cases it cannot serve.
 */
class TextInput(
    private val device: UiDevice,
    private val locators: LocatorResolver,
) {

    /**
     * Writes [value] into the element named by [locator].
     *
     * With no locator the text goes to whatever currently holds focus,
     * which is the "type into the field I just tapped" case the shared
     * contract supports.
     */
    fun input(locator: Locator, value: String): HttpResponse {
        val node = if (locator.isEmpty) focusedEditable() else locators.resolve(locator)

        if (node == null) {
            return if (locator.isEmpty) {
                HttpResponse.error(404, "no focused text field to type into")
            } else {
                HttpResponse.error(404, "element $locator not found")
            }
        }

        if (setText(node, value)) {
            return verify(locator, node, value)
        }

        // Not editable, or the widget refused the action: focus it and
        // type. Custom views backed by a non-EditText often land here.
        node.performAction(AccessibilityNodeInfo.ACTION_CLICK)

        if (!typeKeystrokes(value)) {
            return HttpResponse.error(
                500,
                "input text: $locator did not accept ACTION_SET_TEXT and \"$value\" " +
                    "contains characters with no keycode; give the field a standard EditText or use ASCII",
            )
        }

        return verify(locator, node, value)
    }

    /**
     * Erases [characters] characters from the element.
     *
     * Clearing through ACTION_SET_TEXT is one operation regardless of
     * length; the count only matters on the keystroke fallback, where it
     * bounds how many deletes to send.
     */
    fun erase(locator: Locator, characters: Int): HttpResponse {
        val node = if (locator.isEmpty) focusedEditable() else locators.resolve(locator)

        if (node != null && setText(node, "")) {
            return HttpResponse.ok()
        }

        node?.performAction(AccessibilityNodeInfo.ACTION_CLICK)

        repeat(characters.coerceIn(0, MAX_ERASE)) {
            device.pressKeyCode(KeyEvent.KEYCODE_DEL)
        }

        return HttpResponse.ok()
    }

    /**
     * Confirms the field holds what was asked for.
     *
     * A silently truncated field (an input filter, a maxLength) would
     * otherwise surface much later as a confusing assertion failure on
     * an unrelated step, so a partial write is worth catching here.
     *
     * But a field can also be simply unreadable. A masked field exposes
     * bullets rather than characters, and a Compose password field
     * exposes nothing at all — its semantics node carries no text and
     * does not set isPassword, which is a View-level flag. Reading back
     * an empty string there means "cannot tell", not "nothing landed":
     * the widget already accepted the value, or the write would have
     * reported failure. Treating that as truncation would fail every
     * password entry on Compose.
     *
     * So: an empty read-back is unverifiable and trusted; a non-empty
     * one that is shorter than what was written is a real truncation.
     */
    private fun verify(locator: Locator, node: AccessibilityNodeInfo, want: String): HttpResponse {
        if (want.isEmpty()) return HttpResponse.ok()

        node.refresh()

        val got = node.text?.toString().orEmpty()

        if (got.isEmpty()) return HttpResponse.ok()

        // Masked fields report one bullet per character, so compare
        // lengths there and content everywhere else.
        val ok = if (node.isPassword || got != want) got.length == want.length else true

        if (ok) return HttpResponse.ok()

        return HttpResponse.error(
            500,
            "input text: $locator holds ${got.length} of ${want.length} characters after the write " +
                "(an input filter or maxLength may be truncating it)",
        )
    }

    private fun setText(node: AccessibilityNodeInfo, value: String): Boolean {
        if (!node.isEditable) return false

        val args = Bundle().apply {
            putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, value)
        }

        return node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)
    }

    private fun focusedEditable(): AccessibilityNodeInfo? =
        device.instrumentationOrNull()?.uiAutomation?.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)

    /**
     * Sends [value] one keycode at a time.
     *
     * Returns false when any character has no keycode, so the caller can
     * report exactly why the write is impossible rather than silently
     * writing a mangled string.
     */
    private fun typeKeystrokes(value: String): Boolean {
        val events = value.map { ch -> keyEventsFor(ch) ?: return false }

        for (event in events) {
            device.pressKeyCode(event.first, event.second)
        }

        return true
    }

    /** Maps a character onto (keycode, metaState), or null when unmappable. */
    private fun keyEventsFor(ch: Char): Pair<Int, Int>? = when (ch) {
        in '0'..'9' -> (KeyEvent.KEYCODE_0 + (ch - '0')) to 0
        in 'a'..'z' -> (KeyEvent.KEYCODE_A + (ch - 'a')) to 0
        in 'A'..'Z' -> (KeyEvent.KEYCODE_A + (ch - 'A')) to KeyEvent.META_SHIFT_LEFT_ON
        ' ' -> KeyEvent.KEYCODE_SPACE to 0
        '.' -> KeyEvent.KEYCODE_PERIOD to 0
        ',' -> KeyEvent.KEYCODE_COMMA to 0
        '-' -> KeyEvent.KEYCODE_MINUS to 0
        '_' -> KeyEvent.KEYCODE_MINUS to KeyEvent.META_SHIFT_LEFT_ON
        '@' -> KeyEvent.KEYCODE_AT to 0
        '/' -> KeyEvent.KEYCODE_SLASH to 0
        ':' -> KeyEvent.KEYCODE_SEMICOLON to KeyEvent.META_SHIFT_LEFT_ON
        ';' -> KeyEvent.KEYCODE_SEMICOLON to 0
        '\'' -> KeyEvent.KEYCODE_APOSTROPHE to 0
        else -> null
    }

    private companion object {
        const val MAX_ERASE = 256
    }
}

/**
 * UiDevice does not expose the Instrumentation it was built from, and
 * the focused-node lookup needs the UiAutomation. Reading it back from
 * the registry keeps TextInput constructible from UiDevice alone.
 */
private fun UiDevice.instrumentationOrNull(): android.app.Instrumentation? = try {
    androidx.test.platform.app.InstrumentationRegistry.getInstrumentation()
} catch (e: IllegalStateException) {
    Log.e("instrumentation unavailable: ${e.message}")
    null
}
