package org.taleslabs.tales.demo

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** The screens the demo app can show. */
enum class Screen {
    Welcome,
    Login,
    Register,
    Verification,
    Gestures,
    Permissions,
    BackNav,
    Feed,
    FeedDetail,
    Search,
    Profile,
}

/**
 * Authentication state and its validation rules.
 *
 * The rules mirror the iOS demo app exactly — same error strings, same
 * magic address, same artificial delay — because the two apps are driven
 * by the same scenarios. A rule that differed would show up as a test
 * that passes on one platform and not the other, for reasons that have
 * nothing to do with the driver under test.
 */
class AuthStore {
    var isAuthenticated by mutableStateOf(false)
        private set
    var currentEmail by mutableStateOf("")
        private set

    var loginError by mutableStateOf("")
        private set
    var isLoggingIn by mutableStateOf(false)
        private set

    var registerError by mutableStateOf("")
        private set
    var isRegistering by mutableStateOf(false)
        private set

    fun signIn(email: String, password: String, onDone: () -> Unit) {
        loginError = ""

        if (email.isEmpty() || !email.contains("@")) {
            loginError = "Enter a valid email"
            return
        }

        if (password.isEmpty()) {
            loginError = "Enter a password"
            return
        }

        if (email.lowercase() == BAD_EMAIL) {
            loginError = "Account locked. Contact support."
            return
        }

        isLoggingIn = true

        delayed {
            isLoggingIn = false
            currentEmail = email
            isAuthenticated = true
            onDone()
        }
    }

    fun register(
        email: String,
        password: String,
        repeatPassword: String,
        acceptTerms: Boolean,
        acceptPrivacy: Boolean,
        onDone: () -> Unit,
    ) {
        registerError = ""

        if (email.isEmpty() || !email.contains("@")) {
            registerError = "Enter a valid email"
            return
        }

        if (password.length < MIN_PASSWORD_LENGTH) {
            registerError = "Password must be at least $MIN_PASSWORD_LENGTH characters"
            return
        }

        if (password != repeatPassword) {
            registerError = "Passwords do not match"
            return
        }

        if (!acceptTerms || !acceptPrivacy) {
            registerError = "Accept the terms and the privacy policy"
            return
        }

        isRegistering = true

        delayed {
            isRegistering = false
            currentEmail = email
            onDone()
        }
    }

    fun signOut() {
        isAuthenticated = false
        currentEmail = ""
    }

    companion object {
        /** Signing in with this address always fails, for the sad path. */
        const val BAD_EMAIL = "bad@example.com"
        const val MIN_PASSWORD_LENGTH = 8
    }
}

/**
 * Runs [block] after a short delay on the main thread.
 *
 * The delay is deliberate: it gives the login and register flows a
 * visible in-flight state, so a scenario asserting on `login.loading`
 * has something to observe rather than a transition that completes
 * before the first hierarchy poll.
 */
private fun delayed(block: () -> Unit) {
    android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({ block() }, SUBMIT_DELAY_MS)
}

private const val SUBMIT_DELAY_MS = 400L
