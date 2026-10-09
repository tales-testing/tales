package org.taleslabs.tales.driver

import android.os.SystemClock

/** [Clock] on the device's uptime, the clock the rest of the driver times itself with. */
object DeviceClock : Clock {
    override fun uptimeMillis(): Long = SystemClock.uptimeMillis()

    override fun sleep(millis: Long) = SystemClock.sleep(millis)
}
