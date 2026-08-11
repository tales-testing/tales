import java.security.MessageDigest

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "org.taleslabs.tales.driver"
    compileSdk = libs.versions.compileSdk.get().toInt()

    defaultConfig {
        applicationId = "org.taleslabs.tales.driver"
        minSdk = libs.versions.minSdk.get().toInt()
        targetSdk = libs.versions.targetSdk.get().toInt()
        versionCode = 1
        versionName = "1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        // Both APKs must be signed with the same key for the platform to
        // let the instrumentation attach. The keystore is committed so
        // every build — local or CI — produces interchangeable artifacts.
        getByName("debug") {
            storeFile = rootProject.file("debug.keystore")
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
    }

    buildTypes {
        getByName("debug") {
            signingConfig = signingConfigs.getByName("debug")
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        }
    }

    testOptions {
        unitTests {
            // The JVM tests cover the pure logic (JSON, HTTP framing,
            // tree encoding). Those files call android.util.Log, whose
            // android.jar stub throws by default; returning defaults
            // makes logging a no-op instead of a test failure.
            isReturnDefaultValues = true
        }
    }
}

dependencies {
    androidTestImplementation(libs.androidx.uiautomator)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.junit)

    // Pure-JVM tests for the parts that do not need a device: the HTTP
    // parser, the JSON codec, the tree encoding and the locator.
    testImplementation(libs.junit)
}

// ---------------------------------------------------------------------
// Prebuilt artifact plumbing
//
// The Tales binary embeds the two APKs so running Android tests needs
// only adb and a device — no JDK, no Gradle, no Android SDK. That means
// the APKs are checked in, and checked-in binaries drift. The sentinel
// below is a SHA-256 of every driver source file; CI recomputes it and
// fails the build when it no longer matches what is committed, naming
// the command to run. Verifying is pure file I/O, so the Go side never
// has to load the Android plugin.
// ---------------------------------------------------------------------

val prebuiltDir: File = rootProject.file("../prebuilt")

val copyDriverApk by tasks.registering(Copy::class) {
    description = "Copies the driver app APK into drivers/android/prebuilt/."
    from(layout.buildDirectory.file("outputs/apk/debug/app-debug.apk"))
    into(prebuiltDir)
    rename { "tales-driver.apk" }
}

val copyDriverTestApk by tasks.registering(Copy::class) {
    description = "Copies the instrumentation APK into drivers/android/prebuilt/."
    from(layout.buildDirectory.file("outputs/apk/androidTest/debug/app-debug-androidTest.apk"))
    into(prebuiltDir)
    rename { "tales-driver-test.apk" }
}

/**
 * Hashes every driver source file, sorted by path so the digest does not
 * depend on filesystem order, with line endings normalised so a Windows
 * checkout produces the same value.
 */
fun driverSourceHash(): String {
    val root = rootProject.projectDir
    val digest = MessageDigest.getInstance("SHA-256")

    root.walkTopDown()
        .filter { it.isFile }
        .filter { file ->
            val path = file.relativeTo(root).invariantSeparatorsPath
            !path.startsWith("build/") &&
                !path.startsWith("app/build/") &&
                !path.startsWith(".gradle/") &&
                (
                    path.endsWith(".kt") ||
                        path.endsWith(".java") ||
                        path.endsWith(".xml") ||
                        path.endsWith(".kts") ||
                        path.endsWith(".toml") ||
                        path.endsWith(".properties")
                    )
        }
        .sortedBy { it.relativeTo(root).invariantSeparatorsPath }
        .forEach { file ->
            digest.update(file.relativeTo(root).invariantSeparatorsPath.toByteArray())
            digest.update(0)
            digest.update(file.readText().replace("\r\n", "\n").toByteArray())
            digest.update(0)
        }

    return digest.digest().joinToString("") { "%02x".format(it) }
}

val updateSourceSentinel by tasks.registering {
    description = "Writes the driver source SHA-256 next to the prebuilt APKs."

    doLast {
        prebuiltDir.mkdirs()
        File(prebuiltDir, "source.sha256").writeText(driverSourceHash() + "\n")
    }
}

// AGP registers its variant tasks lazily, so assembleDebug does not
// exist while this file is being evaluated. afterEvaluate is where they
// are reachable by name.
afterEvaluate {
    tasks.named("assembleDebug") { finalizedBy(copyDriverApk, updateSourceSentinel) }
    tasks.named("assembleDebugAndroidTest") { finalizedBy(copyDriverTestApk, updateSourceSentinel) }
}

tasks.register("checkPrebuiltFresh") {
    description = "Fails when the committed APKs are older than the driver source."
    group = "verification"

    doLast {
        val sentinel = File(prebuiltDir, "source.sha256")
        if (!sentinel.isFile) {
            throw GradleException(
                "drivers/android/prebuilt/source.sha256 is missing. " +
                    "Run: make build-android-driver",
            )
        }

        val committed = sentinel.readText().trim()
        val actual = driverSourceHash()

        if (committed != actual) {
            throw GradleException(
                "The committed Android driver APKs are stale.\n" +
                    "  committed source hash: $committed\n" +
                    "  actual source hash:    $actual\n" +
                    "Rebuild and commit them with: make build-android-driver",
            )
        }
    }
}
