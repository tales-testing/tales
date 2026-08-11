package org.taleslabs.tales.driver

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpTest {

    @Test
    fun `parses a percent-encoded query`() {
        val query = parseQuery("bundleId=com.example.app&label=Sign%20in")

        assertEquals("com.example.app", query["bundleId"])
        assertEquals("Sign in", query["label"])
    }

    @Test
    fun `skips pairs without a value separator`() {
        // No driver route takes a valueless flag, so such a pair is a
        // client bug worth not silently absorbing as an empty value.
        val query = parseQuery("a=1&broken&b=2")

        assertEquals(mapOf("a" to "1", "b" to "2"), query)
    }

    @Test
    fun `decodes plus as space and tolerates malformed escapes`() {
        assertEquals("a b", percentDecode("a+b"))
        assertEquals("100%", percentDecode("100%"))
        assertEquals("a%zz", percentDecode("a%zz"))
    }

    @Test
    fun `error responses carry a json error field`() {
        // The Go client surfaces the body verbatim in the step failure,
        // so this is what a user reads when a step fails.
        val response = HttpResponse.error(404, "element not found")

        assertEquals(404, response.status)
        assertEquals("""{"error":"element not found"}""", String(response.body))
    }

    @Test
    fun `ok responses match the shared driver contract`() {
        assertEquals("""{"ok":true}""", String(HttpResponse.ok().body))
    }

    @Test
    fun `png responses declare an image content type`() {
        val response = HttpResponse.png(byteArrayOf(1, 2, 3))

        assertEquals("image/png", response.contentType)
        assertTrue(response.body.contentEquals(byteArrayOf(1, 2, 3)))
    }
}
