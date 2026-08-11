package org.taleslabs.tales.driver

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.Configurator
import androidx.test.uiautomator.UiDevice
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The driver's entry point.
 *
 * Tales starts this with
 *
 *   adb shell am instrument -w \
 *     -e class org.taleslabs.tales.driver.TalesDriverTest#runServer \
 *     -e port <port> \
 *     org.taleslabs.tales.driver.test/androidx.test.runner.AndroidJUnitRunner
 *
 * and reaches it through `adb forward`. The test never returns: it is a
 * server, and `am instrument -w` keeps the process alive until Tales
 * kills it at the end of the session.
 */
@RunWith(AndroidJUnit4::class)
class TalesDriverTest {

    @Test
    fun runServer() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val arguments = InstrumentationRegistry.getArguments()

        val host = arguments.getString("host") ?: DEFAULT_HOST
        val port = arguments.getString("port")?.toIntOrNull() ?: DEFAULT_PORT

        disableImplicitWaits()

        val device = UiDevice.getInstance(instrumentation)
        val router = Router(instrumentation, device)

        Log.i("starting driver on $host:$port")

        HttpServer(host, port) { request -> router.handle(request) }.serve()
    }

    /**
     * Turns off UiAutomator's implicit waits.
     *
     * By default every query first waits for the accessibility layer to
     * report idle. On an app that animates continuously — a progress
     * spinner, a Compose or React Native screen with a running
     * transition — that wait never completes and the driver hangs with
     * no diagnostic.
     *
     * Tales does not need it: the host polls the hierarchy on its own
     * interval with its own timeout, so settling is decided there, where
     * it is visible in the step's `timeout` and reported on failure. The
     * only remaining wait is the bounded one in the snapshot service.
     */
    private fun disableImplicitWaits() {
        Configurator.getInstance()
            .setActionAcknowledgmentTimeout(0L)
            .setWaitForIdleTimeout(0L)
            .setWaitForSelectorTimeout(0L)
    }

    private companion object {
        const val DEFAULT_HOST = "127.0.0.1"
        const val DEFAULT_PORT = 9080
    }
}
