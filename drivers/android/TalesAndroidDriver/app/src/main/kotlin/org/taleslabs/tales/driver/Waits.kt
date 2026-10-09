package org.taleslabs.tales.driver

/**
 * The time source the driver's waits run on.
 *
 * Behind an interface so [Waits] runs under plain JVM tests, which is
 * the only place its timing can be exercised on purpose: on a fast
 * device every wait ends on its first reading, and the readings that
 * break one are the ones a loaded CI emulator produces.
 */
interface Clock {
    fun uptimeMillis(): Long

    fun sleep(millis: Long)
}

/**
 * How the driver waits on a screen that may still be moving.
 *
 * Polling, because there is nothing to block on: the accessibility
 * layer does not report a scroll as running, and a tree read in the
 * middle of a change comes back incomplete rather than flagged as such.
 * Free of android imports for the same reason as the encoder, so the
 * rules are covered by JVM tests.
 */
object Waits {

    /**
     * How long an element has to hold still before the driver touches it.
     *
     * Two equal reads 100ms apart used to be enough, and they prove
     * nothing: bounds are whole pixels, and the tail of a scroll moves
     * the content by less than one per read well before it ends. A
     * Compose scrollable consumes a touch that lands while it is still
     * scrolling (that is how a finger stops a fling), so the tap that
     * followed scroll_to reached the screen, exactly on an element at
     * rest to the pixel, and clicked nothing (issue #77). The iOS driver
     * waits out the same deceleration for the same reason.
     */
    const val TOUCH_QUIET_MS = 300L

    /**
     * How long an action that cannot fall back on coordinates keeps
     * looking for its element before answering 404.
     *
     * Short next to the host's own budget: the host has just seen the
     * element visible, so a miss here is either a moment's gap in the
     * tree or a genuine disagreement, and only the first is worth
     * waiting for.
     */
    const val RESOLVE_TIMEOUT_MS = 2_000L

    /** How often it looks again in the meantime. */
    const val RESOLVE_POLL_MS = 100L

    /**
     * Calls [read] until it returns something, for at most [timeoutMs],
     * and returns null once the time is up.
     *
     * The first call is made as is, so a hit costs what it always did.
     * Only a miss waits [pollMs] and runs [beforeRetry] before reading
     * again: that is where the caller drops the node cache, since a
     * cached tree would only repeat the miss.
     */
    fun <T : Any> untilFound(
        timeoutMs: Long,
        pollMs: Long,
        clock: Clock,
        beforeRetry: () -> Unit,
        read: () -> T?,
    ): T? {
        val deadline = clock.uptimeMillis() + timeoutMs

        while (true) {
            read()?.let { return it }

            if (clock.uptimeMillis() >= deadline) return null

            clock.sleep(pollMs)
            beforeRetry()
        }
    }

    /**
     * Reads [read] every [pollMs] until it has returned the same value
     * for [quietMs], and returns that value; once [capMs] has passed,
     * returns the latest reading instead, because something that never
     * holds still (a marquee, a spinner in a list) would otherwise keep
     * the wait going forever.
     *
     * [first] is the reading the caller already holds. The window
     * restarts whenever the value changes, and it is measured on the
     * clock rather than counted in reads, so a read that is slow to come
     * back does not stretch it. A null from [read] means the thing being
     * read has gone, and the last reading is all there is.
     */
    fun <T : Any> untilStill(
        first: T,
        quietMs: Long,
        pollMs: Long,
        capMs: Long,
        clock: Clock,
        read: () -> T?,
    ): T {
        val deadline = clock.uptimeMillis() + capMs
        var value = first
        var since = clock.uptimeMillis()

        while (clock.uptimeMillis() < deadline) {
            clock.sleep(pollMs)

            val reading = read() ?: break
            val now = clock.uptimeMillis()

            if (reading != value) {
                value = reading
                since = now
            } else if (now - since >= quietMs) {
                break
            }
        }

        return value
    }
}
