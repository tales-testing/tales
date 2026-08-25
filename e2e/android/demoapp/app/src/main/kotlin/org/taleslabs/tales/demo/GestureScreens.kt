package org.taleslabs.tales.demo

import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp

/**
 * Mirrors of the gesture counters the iOS demo app exposes.
 *
 * Each gesture flips a status line rather than animating: an assertion
 * on "longpress=1" is a fact the driver can read out of the hierarchy,
 * whereas an animation is a race.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun GesturesScreen(onBack: () -> Unit) {
    var longPresses by rememberSaveable { mutableIntStateOf(0) }
    var doubleTaps by rememberSaveable { mutableIntStateOf(0) }
    var submits by rememberSaveable { mutableIntStateOf(0) }
    var swipeDirection by rememberSaveable { mutableStateOf("none") }
    var keyField by rememberSaveable { mutableStateOf("") }

    val orientation = if (LocalConfiguration.current.orientation == Configuration.ORIENTATION_LANDSCAPE) {
        "landscape"
    } else {
        "portrait"
    }

    val keyboard = LocalSoftwareKeyboardController.current

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .testTag("gestures.screen"),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("gestures.back")) {
            Text("Back")
        }

        Text("longpress=$longPresses", modifier = Modifier.testTag("gestures.status.longpress"))
        Text("doubletap=$doubleTaps", modifier = Modifier.testTag("gestures.status.doubletap"))
        Text("submit=$submits", modifier = Modifier.testTag("gestures.status.submit"))
        Text("swipe=$swipeDirection", modifier = Modifier.testTag("gestures.status.swipe"))
        Text("orientation=$orientation", modifier = Modifier.testTag("gestures.status.orientation"))

        Text(
            "Long press me",
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .background(Color(0xFFE0E0E0))
                .combinedClickable(
                    onClick = {},
                    onLongClick = { longPresses++ },
                    onDoubleClick = {},
                )
                .testTag("gestures.longpress.target"),
        )

        Text(
            "Double tap me",
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .background(Color(0xFFD0D0D0))
                .combinedClickable(
                    onClick = {},
                    onDoubleClick = { doubleTaps++ },
                )
                .testTag("gestures.doubletap.target"),
        )

        Text(
            "Swipe me",
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .background(Color(0xFFC0C0C0))
                .pointerInput(Unit) {
                    detectHorizontalDragGestures { _, dragAmount ->
                        swipeDirection = if (dragAmount < 0) "left" else "right"
                    }
                }
                .testTag("gestures.swipe.target"),
        )

        OutlinedTextField(
            value = keyField,
            onValueChange = { keyField = it },
            label = { Text("Press return here") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(
                onDone = {
                    submits++
                    keyboard?.hide()
                },
            ),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("gestures.keyfield"),
        )

        // A long list so `scroll` has somewhere to go and a far row is
        // genuinely absent from the tree until scrolling realizes it.
        LazyColumn(modifier = Modifier.testTag("gestures.scroll")) {
            items((0 until 24).toList()) { index ->
                Text("Row $index", modifier = Modifier.testTag("gestures.row.$index"))
            }
        }
    }
}

/**
 * Reports which permissions the app currently holds.
 *
 * It never requests them: the scenario grants and revokes through the
 * `permissions { }` block, and this screen exists to prove the block
 * took effect.
 */
@Composable
fun PermissionsScreen(onBack: () -> Unit) {
    val context = LocalContext.current

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("permissions.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("permissions.back")) {
            Text("Back")
        }

        Text(
            "camera=${statusOf(context, android.Manifest.permission.CAMERA)}",
            modifier = Modifier.testTag("permissions.status.camera"),
        )

        Text(
            "location=${statusOf(context, android.Manifest.permission.ACCESS_FINE_LOCATION)}",
            modifier = Modifier.testTag("permissions.status.location"),
        )
    }
}

private fun statusOf(context: Context, permission: String): String =
    if (context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED) {
        "granted"
    } else {
        "denied"
    }

/**
 * A screen for the back gesture, which is central on Android and has no
 * iOS counterpart. Reaching it and pressing back returns to Welcome, so
 * `press_button { button = "back" }` has something to assert against.
 */
@Composable
fun BackNavScreen(onBack: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("backnav.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Press the back button to leave", modifier = Modifier.testTag("backnav.title"))

        Button(onClick = onBack, modifier = Modifier.testTag("backnav.leave")) {
            Text("Leave")
        }
    }
}
