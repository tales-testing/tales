package org.taleslabs.tales.driver

import java.io.BufferedOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/** One parsed request: method, path, decoded query and raw body. */
data class HttpRequest(
    val method: String,
    val path: String,
    val query: Map<String, String>,
    val body: String,
)

/**
 * One response. [contentType] lets /screenshot return raw PNG bytes
 * through the same path as the JSON routes.
 */
data class HttpResponse(
    val status: Int,
    val body: ByteArray,
    val contentType: String = "application/json",
) {
    companion object {
        fun json(status: Int, value: Any?): HttpResponse =
            HttpResponse(status, Json.write(value).toByteArray(Charsets.UTF_8))

        fun ok(): HttpResponse = json(200, mapOf("ok" to true))

        /**
         * Errors always carry a JSON `error` field. The Go client
         * surfaces the body verbatim in the step failure, so the message
         * is what a user reads when a step fails.
         */
        fun error(status: Int, message: String): HttpResponse =
            json(status, mapOf("error" to message))

        fun png(bytes: ByteArray): HttpResponse = HttpResponse(200, bytes, "image/png")
    }

    // ByteArray in a data class: equals/hashCode compare identity, which
    // is wrong for tests asserting on bodies. Override both.
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (other !is HttpResponse) return false

        return status == other.status &&
            contentType == other.contentType &&
            body.contentEquals(other.body)
    }

    override fun hashCode(): Int = (status * 31 + contentType.hashCode()) * 31 + body.contentHashCode()
}

/**
 * A minimal HTTP/1.1 server: GET and POST, Content-Length bodies, one
 * response per connection, `Connection: close` throughout.
 *
 * This is deliberately not a general-purpose server. The driver serves
 * a fixed set of well-known routes to a single local client over an adb
 * port forward, and the APK it lives in is embedded in the Tales binary,
 * so pulling in Ktor or NanoHTTPD would cost dex size and dependency
 * risk for capabilities nothing here uses.
 */
class HttpServer(
    private val host: String,
    private val port: Int,
    private val handler: (HttpRequest) -> HttpResponse,
) {
    private var socket: ServerSocket? = null

    /**
     * Serves connections off the accept loop. Cached rather than
     * fixed-size: the driver's real concurrency is one, and the extra
     * threads exist only to absorb connections that never send.
     */
    private val workers: ExecutorService = Executors.newCachedThreadPool { r ->
        Thread(r, "tales-driver-http").apply { isDaemon = true }
    }

    /**
     * Binds the port and serves until [stop] or the thread is
     * interrupted. Blocks the caller, which is what the instrumentation
     * entry point wants: the test method must not return.
     */
    fun serve() {
        val server = ServerSocket(port, BACKLOG, InetAddress.getByName(host))
        server.reuseAddress = true
        socket = server

        Log.i("listening on $host:$port")

        while (!Thread.currentThread().isInterrupted && !server.isClosed) {
            val connection = try {
                server.accept()
            } catch (e: IOException) {
                if (server.isClosed) return

                Log.e("accept failed: ${e.message}")
                continue
            }

            // Serve off the accept loop.
            //
            // Handling connections inline looks simpler — the driver
            // only ever has one caller — but it makes any connection
            // that opens without sending a complete request wedge the
            // whole server: the accept loop is blocked reading from it
            // and never reaches the next one. An HTTP client's
            // connection pool does exactly that routinely, and the
            // symptom is brutal to diagnose, because the driver stays
            // alive and simply stops answering, so the next request
            // fails with a bare EOF and nothing at all in the log.
            //
            // The read timeout below is the second half of the same
            // guard: it bounds how long such a connection can occupy a
            // thread. Concurrency is safe here — snapshots are
            // single-flighted in SnapshotService, and every other route
            // is a short UiAutomator call.
            workers.execute { handleConnection(connection) }
        }
    }

    fun stop() {
        socket?.close()
        socket = null
        workers.shutdownNow()
    }

    private fun handleConnection(connection: Socket) {
        connection.use { conn ->
            conn.tcpNoDelay = true
            // Bounds a connection that opens and then says nothing.
            conn.soTimeout = READ_TIMEOUT_MS

            try {
                val request = readRequest(conn.getInputStream())
                if (request == null) {
                    write(conn, HttpResponse.error(400, "malformed request"))
                    return
                }

                write(conn, dispatch(request))
            } catch (e: IOException) {
                // The client hung up (a cancelled step, a timed-out
                // poll). Nothing to report: the Go side already knows.
                Log.e("connection error: ${e.message}")
            }
        }
    }

    /**
     * Runs the handler and turns anything it throws into a 500 rather
     * than letting it kill the accept loop. A driver that stops
     * answering turns every later step into an opaque connection error,
     * so surviving a bad request matters more than failing fast.
     */
    private fun dispatch(request: HttpRequest): HttpResponse {
        val started = System.currentTimeMillis()
        val logged = !(request.method == "GET" && request.path == "/health")

        if (logged) Log.i("request: ${request.method} ${request.path}")

        val response = try {
            handler(request)
        } catch (e: JsonException) {
            HttpResponse.error(400, "invalid JSON body: ${e.message}")
        } catch (e: Throwable) {
            Log.e("handler for ${request.method} ${request.path} threw: $e")
            HttpResponse.error(500, "${request.method} ${request.path}: ${e.message ?: e.toString()}")
        }

        if (logged) {
            val elapsed = System.currentTimeMillis() - started
            Log.i("response: ${request.method} ${request.path} status=${response.status} elapsed=${elapsed}ms")
        }

        return response
    }

    private fun readRequest(input: InputStream): HttpRequest? {
        val head = readHead(input) ?: return null
        val lines = head.split("\r\n")
        val requestLine = lines.firstOrNull()?.split(' ') ?: return null

        if (requestLine.size < 2) return null

        val method = requestLine[0]
        val target = requestLine[1]

        val contentLength = lines.asSequence()
            .drop(1)
            .mapNotNull { line ->
                val idx = line.indexOf(':')
                if (idx <= 0) return@mapNotNull null

                val name = line.substring(0, idx).trim()
                if (!name.equals("Content-Length", ignoreCase = true)) return@mapNotNull null

                line.substring(idx + 1).trim().toIntOrNull()
            }
            .firstOrNull() ?: 0

        val body = if (contentLength > 0) readBody(input, contentLength) else ""

        val questionMark = target.indexOf('?')
        val path = if (questionMark < 0) target else target.substring(0, questionMark)
        val query = if (questionMark < 0) emptyMap() else parseQuery(target.substring(questionMark + 1))

        return HttpRequest(method = method, path = path, query = query, body = body)
    }

    /** Reads bytes until the blank line terminating the header block. */
    private fun readHead(input: InputStream): String? {
        val buffer = StringBuilder()

        while (true) {
            val b = input.read()
            if (b < 0) return if (buffer.isEmpty()) null else buffer.toString()

            buffer.append(b.toChar())

            if (buffer.length >= 4 && buffer.endsWith("\r\n\r\n")) {
                return buffer.substring(0, buffer.length - 4)
            }

            if (buffer.length > MAX_HEAD_BYTES) return null
        }
    }

    private fun readBody(input: InputStream, length: Int): String {
        val bytes = ByteArray(length)
        var read = 0

        while (read < length) {
            val n = input.read(bytes, read, length - read)
            if (n < 0) break
            read += n
        }

        return String(bytes, 0, read, Charsets.UTF_8)
    }

    private fun write(connection: Socket, response: HttpResponse) {
        val out = BufferedOutputStream(connection.getOutputStream())

        val header = buildString {
            append("HTTP/1.1 ").append(response.status).append(' ').append(reason(response.status)).append("\r\n")
            append("Content-Type: ").append(response.contentType).append("\r\n")
            append("Content-Length: ").append(response.body.size).append("\r\n")
            append("Connection: close\r\n\r\n")
        }

        out.write(header.toByteArray(Charsets.UTF_8))
        out.write(response.body)
        out.flush()
    }

    private fun reason(status: Int): String = when (status) {
        200 -> "OK"
        400 -> "Bad Request"
        404 -> "Not Found"
        500 -> "Internal Server Error"
        501 -> "Not Implemented"
        503 -> "Service Unavailable"
        else -> "Unknown"
    }

    private companion object {
        const val BACKLOG = 16
        const val READ_TIMEOUT_MS = 30_000
        const val MAX_HEAD_BYTES = 16 * 1024
    }
}

/**
 * Percent-decodes an `a=1&b=2` query string. Pairs without '=' are
 * skipped rather than treated as empty-valued keys: no driver route
 * takes a valueless flag, so such a pair is a client bug we would
 * rather not silently absorb.
 */
internal fun parseQuery(raw: String): Map<String, String> {
    if (raw.isEmpty()) return emptyMap()

    val out = LinkedHashMap<String, String>()

    for (pair in raw.split('&')) {
        val idx = pair.indexOf('=')
        if (idx <= 0) continue

        val key = percentDecode(pair.substring(0, idx))
        val value = percentDecode(pair.substring(idx + 1))
        out[key] = value
    }

    return out
}

internal fun percentDecode(raw: String): String {
    if ('%' !in raw && '+' !in raw) return raw

    val out = StringBuilder(raw.length)
    var i = 0

    while (i < raw.length) {
        when {
            raw[i] == '+' -> {
                out.append(' ')
                i++
            }

            raw[i] == '%' && i + 2 < raw.length -> {
                val hex = raw.substring(i + 1, i + 3).toIntOrNull(16)

                if (hex == null) {
                    out.append(raw[i])
                    i++
                } else {
                    out.append(hex.toChar())
                    i += 3
                }
            }

            else -> {
                out.append(raw[i])
                i++
            }
        }
    }

    return out.toString()
}
