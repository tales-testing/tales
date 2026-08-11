// Root build file. Plugins are declared without applying them so the
// :app module can opt in; keeping them here pins one version for the
// whole build.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.android) apply false
}
