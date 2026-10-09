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
