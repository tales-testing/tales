package org.taleslabs.tales.driver

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** A clock that only moves when the code under test sleeps, or a read takes time. */
private class FakeClock : Clock {
    var now = 0L

    override fun uptimeMillis(): Long = now

    override fun sleep(millis: Long) {
        now += millis
    }
}

class WaitsTest {

    @Test
    fun `does not take two equal reads for an element at rest`() {
        // Issue #77. Bounds are whole pixels, and the tail of a Compose
        // scroll moves the content by less than one per read well before
        // the scroll ends. The scrollable consumes a touch that lands
        // while it is still scrolling, so an element that reads the same
        // twice, 100ms apart, may still be on its way: the tap that
        // followed scroll_to reached the screen, right on the element,
        // and clicked nothing.
        val clock = FakeClock()
        var reads = 0

        val got = Waits.untilStill(2065, Waits.TOUCH_QUIET_MS, pollMs = 100, capMs = 2_000, clock = clock) {
            reads++

            2065
        }

        assertEquals(2065, got)
        assertTrue("touch sent after ${clock.now}ms of stillness", clock.now >= 300)
        assertTrue("touch sent after $reads equal re-reads", reads >= 3)
    }

    @Test
    fun `restarts the quiet window whenever the value moves`() {
        // An element gliding into place: three readings that differ,
        // then the same one over and over. Only the stretch after the
        // last move counts towards "still".
        val clock = FakeClock()
        val readings = ArrayDeque(listOf(1990, 2040, 2065, 2065, 2065, 2065))

        val got = Waits.untilStill(1900, quietMs = 300, pollMs = 100, capMs = 2_000, clock = clock) {
            readings.removeFirst()
        }

        assertEquals(2065, got)
        // Last move read at 300ms, then held for 300ms more.
        assertEquals(600, clock.now)
        assertEquals(0, readings.size)
    }

    @Test
    fun `gives up at the cap with the latest reading`() {
        // Something that never holds still (a marquee, a spinner in a
        // list) must not hold the action forever.
        val clock = FakeClock()
        var position = 0

        val got = Waits.untilStill(position, quietMs = 300, pollMs = 100, capMs = 2_000, clock = clock) {
            ++position
        }

        assertEquals(2_000, clock.now)
        assertEquals(position, got)
    }

    @Test
    fun `stops on the last reading when the value goes away`() {
        // A node that has left the tree cannot be read again; what was
        // read before it left is all there is.
        val clock = FakeClock()
        val readings = ArrayDeque(listOf<Int?>(2065, null, 1234))

        val got = Waits.untilStill(2000, quietMs = 300, pollMs = 100, capMs = 2_000, clock = clock) {
            readings.removeFirst()
        }

        assertEquals(2065, got)
        assertEquals(listOf<Int?>(1234), readings.toList())
    }

    @Test
    fun `measures the quiet window on the clock, not in reads`() {
        // On a loaded emulator one read can take longer than the whole
        // window. Counting reads would multiply the wait by the read
        // time; the window is about how long the value held, and two
        // equal readings 350ms apart already cover it.
        val clock = FakeClock()
        var reads = 0

        val got = Waits.untilStill(2065, quietMs = 300, pollMs = 100, capMs = 2_000, clock = clock) {
            reads++
            clock.now += 250

            2065
        }

        assertEquals(2065, got)
        assertEquals(1, reads)
        assertEquals(350, clock.now)
    }
}
