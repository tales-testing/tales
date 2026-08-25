package org.taleslabs.tales.demo

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTagsAsResourceId

/**
 * The Android demo app the e2e suites drive.
 *
 * It mirrors the iOS demo app screen for screen, with the same element
 * identifiers, so a `.tales` scenario moves between platforms by
 * changing `platform` and nothing else. The screens that exist only to
 * reproduce iOS-specific regressions (the photo picker, the stalling
 * form) have no counterpart here; a back-navigation screen takes their
 * place, since that gesture is central on Android and absent on iOS.
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        setContent {
            MaterialTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    DemoApp()
                }
            }
        }
    }
}

@OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)
@Composable
private fun DemoApp() {
    // Saveable, not merely remembered: the activity can be relaunched
    // under the running scenario (a process replaced after an install, a
    // configuration change the manifest does not absorb), and a store
    // that came back empty would rewind the app to Welcome with every
    // field cleared. Tales would then report a missing element on a
    // screen it had just asserted.
    val auth = rememberSaveable(saver = AuthStore.Saver) { AuthStore() }
    var screen by rememberSaveable { mutableStateOf(Screen.Welcome) }
    var detailIndex by rememberSaveable { mutableStateOf(0) }

    // Without this, Compose test tags are invisible to UiAutomator: they
    // live in the semantics tree, which the accessibility layer does not
    // expose unless an app opts in. This single modifier is what makes
    // every `testTag` below reachable as a resource id, and therefore
    // what makes `id = "welcome.signin"` work at all.
    Box(
        modifier = Modifier
            .fillMaxSize()
            .semantics { testTagsAsResourceId = true }
            .testTag("app.root"),
    ) {
        when (screen) {
            Screen.Welcome -> WelcomeScreen(onNavigate = { screen = it })

            Screen.Login -> LoginScreen(
                auth = auth,
                onBack = { screen = Screen.Welcome },
                onSignedIn = { screen = Screen.Feed },
            )

            Screen.Register -> RegisterScreen(
                auth = auth,
                onBack = { screen = Screen.Welcome },
                onRegistered = { screen = Screen.Verification },
            )

            Screen.Verification -> VerificationScreen(
                email = auth.currentEmail,
                onDone = { screen = Screen.Welcome },
            )

            Screen.Gestures -> GesturesScreen(onBack = { screen = Screen.Welcome })

            Screen.Permissions -> PermissionsScreen(onBack = { screen = Screen.Welcome })

            Screen.BackNav -> BackNavScreen(onBack = { screen = Screen.Welcome })

            Screen.Feed -> FeedScreen(
                onOpen = { index ->
                    detailIndex = index
                    screen = Screen.FeedDetail
                },
                onSearch = { screen = Screen.Search },
                onProfile = { screen = Screen.Profile },
            )

            Screen.FeedDetail -> FeedDetailScreen(
                index = detailIndex,
                onBack = { screen = Screen.Feed },
            )

            Screen.Search -> SearchScreen(onBack = { screen = Screen.Feed })

            Screen.Profile -> ProfileScreen(
                email = auth.currentEmail,
                onBack = { screen = Screen.Feed },
                onLogout = {
                    auth.signOut()
                    screen = Screen.Welcome
                },
            )
        }
    }

    // The system back gesture unwinds to Welcome from anywhere else, so
    // `press_button { button = "back" }` has consistent, assertable
    // behavior on every screen.
    BackHandler(enabled = screen != Screen.Welcome) {
        screen = when (screen) {
            Screen.FeedDetail, Screen.Search, Screen.Profile -> Screen.Feed
            else -> Screen.Welcome
        }
    }
}
