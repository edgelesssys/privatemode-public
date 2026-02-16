# Building the Privatemode AI Android App

## Prerequisites

- Android Studio Arctic Fox or later (or Android Gradle Plugin 8.7+)
- Android SDK with API level 35
- Android NDK (for native proxy compilation)
- Go 1.25+ (for cross-compiling the proxy)
- JDK 17

## Project Structure

```
android-app/
├── app/
│   ├── build.gradle.kts          # App build configuration
│   └── src/main/
│       ├── AndroidManifest.xml
│       ├── cpp/                   # JNI bridge C code
│       │   └── privatemode_jni.c  # JNI bridge to Go proxy
│       ├── java/ai/privatemode/android/
│       │   ├── MainActivity.kt    # Entry point
│       │   ├── PrivatemodeApp.kt  # Application class
│       │   ├── proxy/             # Native proxy management
│       │   │   ├── NativeProxy.kt # JNI declarations
│       │   │   └── ProxyManager.kt# Proxy lifecycle
│       │   ├── data/              # Data layer
│       │   │   ├── model/         # Data models
│       │   │   ├── local/         # Local storage
│       │   │   ├── remote/        # API client (SSE streaming)
│       │   │   └── repository/    # Repository pattern
│       │   ├── ui/                # Jetpack Compose UI
│       │   │   ├── theme/         # Material 3 theme
│       │   │   ├── navigation/    # Navigation graph
│       │   │   ├── setup/         # Onboarding screens
│       │   │   ├── chat/          # Chat interface
│       │   │   ├── settings/      # Settings screen
│       │   │   ├── security/      # Security info screen
│       │   │   └── components/    # Shared components
│       │   └── util/              # Utilities
│       ├── jniLibs/               # Native libraries (built)
│       └── res/                   # Android resources
├── scripts/
│   └── build-native.sh           # Cross-compile script
├── build.gradle.kts              # Root build file
└── settings.gradle.kts
```

## Building the Native Proxy

The Android app embeds the same proxy as the desktop app, cross-compiled for
Android architectures. This proxy handles remote attestation, HPKE secret
exchange, and end-to-end encryption.

### Step 1: Build native libraries

```bash
# Set Android NDK path
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/<version>

# Run the cross-compilation script
./scripts/build-native.sh
```

This produces `libprivatemode.so` files in `app/src/main/jniLibs/` for each
supported architecture (arm64-v8a, armeabi-v7a, x86_64).

### Step 2: Build the APK

```bash
# Debug build
./gradlew assembleDebug

# Release build (requires signing configuration)
./gradlew assembleRelease
```

Or open the project in Android Studio and build from the IDE.

## Architecture

The app follows the same architecture as the desktop Electron app:

```
┌─────────────────────────────────┐
│     Android App (Kotlin/Compose)│
│  ┌───────────────────────────┐  │
│  │     UI Layer (Compose)    │  │
│  │  Chat │ Settings│Security │  │
│  └───────────┬───────────────┘  │
│  ┌───────────┴───────────────┐  │
│  │    Repository Layer       │  │
│  └───────────┬───────────────┘  │
│  ┌───────────┴───────────────┐  │
│  │   HTTP Client (OkHttp)    │  │
│  │   connects to localhost   │  │
│  └───────────┬───────────────┘  │
│  ┌───────────┴───────────────┐  │
│  │   Native Proxy (JNI)     │  │
│  │   libprivatemode.so       │  │
│  │   ┌───────────────────┐   │  │
│  │   │ Go proxy server   │   │  │
│  │   │ - Attestation     │   │  │
│  │   │ - HPKE encryption │   │  │
│  │   │ - Secret exchange │   │  │
│  │   └───────────────────┘   │  │
│  └───────────┬───────────────┘  │
└──────────────┼──────────────────┘
               │ HTTPS (E2E encrypted)
               ▼
┌──────────────────────────────────┐
│  Privatemode Backend (TEE)       │
│  api.privatemode.ai:443          │
│  AMD SEV-SNP + NVIDIA H100      │
└──────────────────────────────────┘
```

The proxy runs as a library loaded into the app process (not a separate
service), binding to `127.0.0.1` on a random port. The app's HTTP client
connects to this local proxy for all API calls.

## Features

- **Onboarding** - Welcome screen with API key setup (UUID v4 validation)
- **Chat** - Multi-turn conversations with streaming SSE responses
- **Model selection** - gpt-oss-120b, Gemma 3 27B, Qwen3 Coder 30B
- **File upload** - Document upload via unstructured API
- **Extended thinking** - Reasoning mode toggle for supported models
- **Chat history** - Persistent local storage with date grouping
- **Chat management** - Create, rename, delete conversations
- **Word count tracking** - Context limit with visual indicators
- **Markdown rendering** - Rich message display (code, tables, lists)
- **Security dashboard** - Remote attestation info, manifest hash, TCB versions
- **Settings** - API key management, danger zone for data deletion
- **End-to-end encryption** - Via embedded proxy with HPKE
- **Remote attestation** - AMD SEV-SNP verification before connecting
