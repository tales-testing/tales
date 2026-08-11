package org.taleslabs.tales.driver

/**
 * A small JSON reader/writer covering exactly the driver's wire format.
 *
 * Android ships `org.json`, but it lives in android.jar and throws
 * "not mocked" under plain JVM unit tests, which is where the encoding
 * rules are cheapest to pin down. kotlinx.serialization would drag a
 * plugin and a runtime into an APK that is embedded in the Tales binary
 * and should stay small. Hand-rolling it keeps both properties.
 *
 * Values are represented with plain Kotlin types: `Map<String, Any?>`,
 * `List<Any?>`, `String`, `Double`, `Boolean` and `null`.
 */
object Json {

    /** Serialises [value] to compact JSON. */
    fun write(value: Any?): String = StringBuilder().also { writeTo(it, value) }.toString()

    private fun writeTo(out: StringBuilder, value: Any?) {
        when (value) {
            null -> out.append("null")
            is String -> writeString(out, value)
            is Boolean -> out.append(if (value) "true" else "false")
            is Int -> out.append(value.toString())
            is Long -> out.append(value.toString())
            is Double -> writeDouble(out, value)
            is Float -> writeDouble(out, value.toDouble())
            is Map<*, *> -> writeObject(out, value)
            is Iterable<*> -> writeArray(out, value)
            else -> writeString(out, value.toString())
        }
    }

    private fun writeObject(out: StringBuilder, value: Map<*, *>) {
        out.append('{')
        var first = true

        for ((k, v) in value) {
            if (!first) out.append(',')
            first = false
            writeString(out, k.toString())
            out.append(':')
            writeTo(out, v)
        }

        out.append('}')
    }

    private fun writeArray(out: StringBuilder, value: Iterable<*>) {
        out.append('[')
        var first = true

        for (item in value) {
            if (!first) out.append(',')
            first = false
            writeTo(out, item)
        }

        out.append(']')
    }

    /**
     * Emits whole doubles without a fractional part. Coordinates and
     * bounds travel as numbers and the Go side decodes them into
     * float64 either way, but "42" reads better than "42.0" in the
     * hierarchy artifacts a user opens when a test fails.
     */
    private fun writeDouble(out: StringBuilder, value: Double) {
        if (!value.isFinite()) {
            out.append('0')
            return
        }

        if (value == value.toLong().toDouble()) {
            out.append(value.toLong().toString())
        } else {
            out.append(value.toString())
        }
    }

    private fun writeString(out: StringBuilder, value: String) {
        out.append('"')

        for (ch in value) {
            when {
                ch == '"' -> out.append("\\\"")
                ch == '\\' -> out.append("\\\\")
                ch == '\n' -> out.append("\\n")
                ch == '\r' -> out.append("\\r")
                ch == '\t' -> out.append("\\t")
                // Control characters are illegal raw in JSON strings.
                // Accessibility labels routinely carry stray ones.
                ch < ' ' -> out.append("\\u%04x".format(ch.code))
                else -> out.append(ch)
            }
        }

        out.append('"')
    }

    /** Parses [text], throwing [JsonException] on malformed input. */
    fun parse(text: String): Any? {
        val reader = Reader(text)
        val value = reader.readValue()

        reader.skipWhitespace()

        if (!reader.atEnd()) {
            throw JsonException("trailing content at offset ${reader.offset}")
        }

        return value
    }

    /**
     * Parses [text] and requires a JSON object, which every driver
     * request body is. An empty body yields an empty object so handlers
     * can uniformly report their own missing-field errors.
     */
    @Suppress("UNCHECKED_CAST")
    fun parseObject(text: String): Map<String, Any?> {
        if (text.isBlank()) return emptyMap()

        return parse(text) as? Map<String, Any?>
            ?: throw JsonException("expected a JSON object")
    }

    private class Reader(private val src: String) {
        var offset = 0

        fun atEnd(): Boolean = offset >= src.length

        fun skipWhitespace() {
            while (offset < src.length && src[offset].isWhitespace()) offset++
        }

        fun readValue(): Any? {
            skipWhitespace()

            if (atEnd()) throw JsonException("unexpected end of input")

            return when (val ch = src[offset]) {
                '{' -> readObject()
                '[' -> readArray()
                '"' -> readString()
                't' -> readLiteral("true", true)
                'f' -> readLiteral("false", false)
                'n' -> readLiteral("null", null)
                else -> {
                    if (ch == '-' || ch.isDigit()) readNumber() else throw JsonException("unexpected character '$ch' at offset $offset")
                }
            }
        }

        private fun readLiteral(token: String, value: Any?): Any? {
            if (!src.startsWith(token, offset)) {
                throw JsonException("invalid literal at offset $offset")
            }

            offset += token.length

            return value
        }

        private fun readObject(): Map<String, Any?> {
            expect('{')

            val out = LinkedHashMap<String, Any?>()
            skipWhitespace()

            if (peek() == '}') {
                offset++
                return out
            }

            while (true) {
                skipWhitespace()
                val key = readString()
                skipWhitespace()
                expect(':')
                out[key] = readValue()
                skipWhitespace()

                when (val ch = next()) {
                    ',' -> continue
                    '}' -> return out
                    else -> throw JsonException("expected ',' or '}' but found '$ch'")
                }
            }
        }

        private fun readArray(): List<Any?> {
            expect('[')

            val out = ArrayList<Any?>()
            skipWhitespace()

            if (peek() == ']') {
                offset++
                return out
            }

            while (true) {
                out.add(readValue())
                skipWhitespace()

                when (val ch = next()) {
                    ',' -> continue
                    ']' -> return out
                    else -> throw JsonException("expected ',' or ']' but found '$ch'")
                }
            }
        }

        private fun readString(): String {
            expect('"')

            val out = StringBuilder()

            while (true) {
                val ch = next()

                when (ch) {
                    '"' -> return out.toString()
                    '\\' -> out.append(readEscape())
                    else -> out.append(ch)
                }
            }
        }

        private fun readEscape(): Char = when (val ch = next()) {
            '"', '\\', '/' -> ch
            'b' -> '\b'
            'f' -> '\u000C'
            'n' -> '\n'
            'r' -> '\r'
            't' -> '\t'
            'u' -> {
                if (offset + 4 > src.length) throw JsonException("truncated \\u escape")
                val hex = src.substring(offset, offset + 4)
                offset += 4
                hex.toIntOrNull(16)?.toChar() ?: throw JsonException("invalid \\u escape '$hex'")
            }

            else -> throw JsonException("invalid escape '\\$ch'")
        }

        private fun readNumber(): Double {
            val start = offset

            if (peek() == '-') offset++

            while (!atEnd() && (src[offset].isDigit() || src[offset] in ".eE+-")) offset++

            val token = src.substring(start, offset)

            return token.toDoubleOrNull() ?: throw JsonException("invalid number '$token'")
        }

        private fun peek(): Char? = if (atEnd()) null else src[offset]

        private fun next(): Char {
            if (atEnd()) throw JsonException("unexpected end of input")

            return src[offset++]
        }

        private fun expect(want: Char) {
            val got = next()
            if (got != want) throw JsonException("expected '$want' but found '$got' at offset ${offset - 1}")
        }
    }
}

/** Raised on malformed JSON; handlers turn it into a 400. */
class JsonException(message: String) : RuntimeException(message)

/** Reads a required string field, or null when absent or not a string. */
fun Map<String, Any?>.stringOrNull(key: String): String? = this[key] as? String

/** Reads a string field, defaulting to "" so handlers can treat absent and empty alike. */
fun Map<String, Any?>.stringOr(key: String, fallback: String = ""): String =
    stringOrNull(key) ?: fallback

/** Reads a numeric field. JSON has one number type, so this covers ints too. */
fun Map<String, Any?>.doubleOrNull(key: String): Double? = when (val v = this[key]) {
    is Double -> v
    is Int -> v.toDouble()
    is Long -> v.toDouble()
    else -> null
}

/** Reads a numeric field as an Int, rounding half-up. */
fun Map<String, Any?>.intOrNull(key: String): Int? = doubleOrNull(key)?.let { Math.round(it).toInt() }

/** Reads a boolean field, defaulting to [fallback] when absent. */
fun Map<String, Any?>.boolOr(key: String, fallback: Boolean = false): Boolean =
    this[key] as? Boolean ?: fallback
