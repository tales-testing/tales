package org.taleslabs.tales.driver

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JsonTest {

    @Test
    fun `writes objects with stable key order`() {
        val json = Json.write(linkedMapOf("b" to 1, "a" to 2))

        assertEquals("""{"b":1,"a":2}""", json)
    }

    @Test
    fun `writes whole doubles without a fractional part`() {
        // Coordinates travel as numbers; "42" keeps the hierarchy
        // artifacts readable for a user debugging a failed step.
        assertEquals("""{"x":42}""", Json.write(mapOf("x" to 42.0)))
        assertEquals("""{"x":42.5}""", Json.write(mapOf("x" to 42.5)))
    }

    @Test
    fun `escapes control characters in strings`() {
        // Accessibility labels routinely carry stray control bytes;
        // emitting them raw would produce invalid JSON.
        val json = Json.write(mapOf("label" to "ab\nc\"d"))

        assertEquals("""{"label":"ab\nc\"d"}""", json)
    }

    @Test
    fun `round-trips nested structures`() {
        val source = """{"a":[1,2,{"b":"x"}],"c":true,"d":null}"""

        @Suppress("UNCHECKED_CAST")
        val parsed = Json.parse(source) as Map<String, Any?>

        assertEquals(true, parsed["c"])
        assertNull(parsed["d"])

        @Suppress("UNCHECKED_CAST")
        val list = parsed["a"] as List<Any?>
        assertEquals(3, list.size)

        @Suppress("UNCHECKED_CAST")
        val nested = list[2] as Map<String, Any?>
        assertEquals("x", nested["b"])
    }

    @Test
    fun `parses escapes including unicode`() {
        @Suppress("UNCHECKED_CAST")
        val parsed = Json.parse("""{"s":"aé\n\t\"b\""}""") as Map<String, Any?>

        assertEquals("aé\n\t\"b\"", parsed["s"])
    }

    @Test
    fun `parseObject treats an empty body as an empty object`() {
        // Routes with no parameters are POSTed without a body; each
        // handler should report its own missing fields rather than the
        // codec rejecting the request first.
        assertTrue(Json.parseObject("").isEmpty())
        assertTrue(Json.parseObject("   ").isEmpty())
    }

    @Test(expected = JsonException::class)
    fun `parseObject rejects a non-object body`() {
        Json.parseObject("[1,2]")
    }

    @Test(expected = JsonException::class)
    fun `rejects trailing content`() {
        Json.parse("""{"a":1} junk""")
    }

    @Test
    fun `reads typed fields`() {
        val obj = Json.parseObject("""{"s":"x","n":3,"f":1.5,"b":true}""")

        assertEquals("x", obj.stringOrNull("s"))
        assertEquals("", obj.stringOr("missing"))
        assertEquals(3, obj.intOrNull("n"))
        assertEquals(1.5, obj.doubleOrNull("f")!!, 0.0001)
        assertTrue(obj.boolOr("b"))
        assertTrue(obj.boolOr("missing", fallback = true))
    }
}
