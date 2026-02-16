plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// ---------------------------------------------------------------------------
// Task: build the native Go proxy library (libprivatemode) via the NDK.
//
// This calls scripts/build-native.sh which cross-compiles the Go code for
// every Android ABI and places the resulting .so files into jniLibs/.
// Gradle's input/output tracking ensures it only re-runs when source files
// change.
// ---------------------------------------------------------------------------
val buildNativeLibs by tasks.registering(Exec::class) {
    description = "Cross-compile the Go proxy library for Android"
    group = "build"

    val scriptFile = project.file("../scripts/build-native.sh")
    val goSource = project.file("../../privatemode-proxy/libprivatemode")
    val jniCSource = project.file("src/main/cpp/privatemode_jni.c")
    val jniLibsDir = project.file("src/main/jniLibs")

    // Inputs: the build script, Go sources, and the JNI C bridge.
    inputs.file(scriptFile)
    inputs.dir(goSource)
    inputs.file(jniCSource)

    // Output: the jniLibs directory that will contain the .so files.
    outputs.dir(jniLibsDir)

    commandLine("bash", scriptFile.absolutePath)

    // Fail the build with a clear message when prerequisites are missing.
    doFirst {
        val goFound = try {
            Runtime.getRuntime().exec(arrayOf("go", "version")).waitFor() == 0
        } catch (_: Exception) { false }

        if (!goFound) {
            throw GradleException(
                "Go is required to build the native proxy library but was not found on PATH.\n" +
                "Install Go 1.25+ from https://go.dev/dl/ and make sure it is on your PATH."
            )
        }

        // ANDROID_NDK_HOME is resolved by the script itself (it checks
        // standard SDK locations), so we only warn here if it's unset.
    }
}

android {
    namespace = "ai.privatemode.android"
    compileSdk = 35

    defaultConfig {
        applicationId = "ai.privatemode.android"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "1.0.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }
}

// Wire buildNativeLibs so the .so files are ready before Gradle packages them.
// Match all merge*JniLibFolders tasks (mergeDebugJniLibFolders, mergeReleaseJniLibFolders, …).
tasks.configureEach {
    if (name.contains("JniLibFolders")) {
        dependsOn(buildNativeLibs)
    }
}

configurations.all {
    exclude(group = "org.jetbrains", module = "annotations-java5")
}

dependencies {
    // Compose BOM
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)

    // Compose UI
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    // Activity & Lifecycle
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")

    // Navigation
    implementation("androidx.navigation:navigation-compose:2.8.5")

    // Networking
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.google.code.gson:gson:2.11.0")

    // Markdown rendering
    implementation("io.noties.markwon:core:4.6.2")
    implementation("io.noties.markwon:ext-strikethrough:4.6.2")
    implementation("io.noties.markwon:ext-tables:4.6.2")
    implementation("io.noties.markwon:html:4.6.2")
    implementation("io.noties.markwon:syntax-highlight:4.6.2")
    implementation("io.noties:prism4j:2.0.0")

    // Core KTX
    implementation("androidx.core:core-ktx:1.15.0")

    // DataStore for preferences
    implementation("androidx.datastore:datastore-preferences:1.1.1")

    // Coroutines
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")

    // Testing
    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
    androidTestImplementation(composeBom)
    androidTestImplementation("androidx.compose.ui:ui-test-junit4")
    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
