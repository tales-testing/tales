package org.taleslabs.tales.demo

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp

@Composable
fun WelcomeScreen(onNavigate: (Screen) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("welcome.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Tales Demo", modifier = Modifier.testTag("welcome.title"))

        Button(onClick = { onNavigate(Screen.Login) }, modifier = Modifier.testTag("welcome.signin")) {
            Text("Sign in")
        }

        Button(onClick = { onNavigate(Screen.Register) }, modifier = Modifier.testTag("welcome.register")) {
            Text("Create account")
        }

        Button(onClick = { onNavigate(Screen.Gestures) }, modifier = Modifier.testTag("welcome.gestures")) {
            Text("Gestures")
        }

        Button(onClick = { onNavigate(Screen.Permissions) }, modifier = Modifier.testTag("welcome.permissions")) {
            Text("Permissions")
        }

        Button(onClick = { onNavigate(Screen.BackNav) }, modifier = Modifier.testTag("welcome.backnav")) {
            Text("Back navigation")
        }
    }
}

@Composable
fun LoginScreen(auth: AuthStore, onBack: () -> Unit, onSignedIn: () -> Unit) {
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("login.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("login.back")) {
            Text("Back")
        }

        OutlinedTextField(
            value = email,
            onValueChange = { email = it },
            label = { Text("Email") },
            modifier = Modifier
                .fillMaxWidth()
                .testTag("login.email"),
        )

        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Password") },
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("login.password"),
        )

        if (auth.loginError.isNotEmpty()) {
            Text(auth.loginError, modifier = Modifier.testTag("login.error"))
        }

        if (auth.isLoggingIn) {
            Text("Signing in…", modifier = Modifier.testTag("login.loading"))
        }

        Button(
            onClick = { auth.signIn(email, password, onSignedIn) },
            modifier = Modifier.testTag("login.submit"),
        ) {
            Text("Sign in")
        }
    }
}

@Composable
fun RegisterScreen(auth: AuthStore, onBack: () -> Unit, onRegistered: () -> Unit) {
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var repeatPassword by remember { mutableStateOf("") }
    var acceptTerms by remember { mutableStateOf(false) }
    var acceptPrivacy by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("register.screen"),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        TextButton(onClick = onBack, modifier = Modifier.testTag("register.back")) {
            Text("Back")
        }

        OutlinedTextField(
            value = email,
            onValueChange = { email = it },
            label = { Text("Email") },
            modifier = Modifier
                .fillMaxWidth()
                .testTag("register.email"),
        )

        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Password") },
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("register.password"),
        )

        OutlinedTextField(
            value = repeatPassword,
            onValueChange = { repeatPassword = it },
            label = { Text("Repeat password") },
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier
                .fillMaxWidth()
                .testTag("register.repeat_password"),
        )

        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(
                checked = acceptTerms,
                onCheckedChange = { acceptTerms = it },
                modifier = Modifier.testTag("register.accept_terms"),
            )
            Text("Accept terms")
        }

        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(
                checked = acceptPrivacy,
                onCheckedChange = { acceptPrivacy = it },
                modifier = Modifier.testTag("register.accept_privacy"),
            )
            Text("Accept privacy policy")
        }

        if (auth.registerError.isNotEmpty()) {
            Text(auth.registerError, modifier = Modifier.testTag("register.error"))
        }

        if (auth.isRegistering) {
            Text("Creating account…", modifier = Modifier.testTag("register.loading"))
        }

        Button(
            onClick = {
                auth.register(email, password, repeatPassword, acceptTerms, acceptPrivacy, onRegistered)
            },
            modifier = Modifier.testTag("register.submit"),
        ) {
            Text("Create account")
        }
    }
}

@Composable
fun VerificationScreen(email: String, onDone: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp)
            .testTag("verify.screen"),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Check your inbox", modifier = Modifier.testTag("verify.title"))
        Text(email, modifier = Modifier.testTag("verify.email"))

        // The code is fixed rather than generated: a scenario asserting
        // on it needs a value it can write down, and the app is a test
        // fixture, not a product.
        Text("Code: 123456", modifier = Modifier.testTag("verify.verification_code"))

        Button(onClick = onDone, modifier = Modifier.testTag("verify.done")) {
            Text("Done")
        }
    }
}
