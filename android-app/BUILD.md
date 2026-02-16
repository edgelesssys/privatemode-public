# Building the Privatemode AI Android App

## Prerequisites

- Android Studio Arctic Fox or later (or Android Gradle Plugin 8.7+)
- Android SDK with API level 35
- JDK 17

Optional (for native proxy — not required for basic functionality):
- Android NDK
- Go 1.25+

## Quick Start

The app works out of the box in **direct HTTPS mode**, connecting to `api.privatemode.ai`
over TLS. No native proxy compilation is needed.

```bash
cd android-app

# Debug build
./gradlew assembleDebug

# Install on connected device/emulator
./gradlew installDebug
```

Or open the `android-app/` directory in Android Studio and build from the IDE.

## Project Structure

```
android-app/
├── app/
│   ├── build.gradle.kts          # App build configuration
│   └── src/main/
│       ├── AndroidManifest.xml
│       ├── cpp/                   # JNI bridge C code (future)
│       │   └── privatemode_jni.c  # JNI bridge to Go proxy
│       ├── java/ai/privatemode/android/
│       │   ├── MainActivity.kt    # Entry point
│       │   ├── PrivatemodeApp.kt  # Application class
│       │   ├── proxy/             # Connection management
│       │   │   ├── NativeProxy.kt # JNI declarations
│       │   │   └── ProxyManager.kt# Connection lifecycle
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
│       ├── jniLibs/               # Native libraries (optional)
│       └── res/                   # Android resources
├── scripts/
│   └── build-native.sh           # Cross-compile script (future)
├── build.gradle.kts              # Root build file
└── settings.gradle.kts
```

## Connection Modes

The app supports two connection modes, selected automatically at startup:

### 1. Direct HTTPS Mode (default)

Connects directly to `https://api.privatemode.ai` over TLS. This is the default
mode when the native proxy library is not present.

- Transport encryption via TLS
- Backend runs in a Trusted Execution Environment (AMD SEV-SNP + NVIDIA H100)
- No client-side attestation verification (the Contrast SDK requires Linux)

### 2. Native Proxy Mode (future)

When `libprivatemode.so` is available, the app loads it via JNI and routes all
traffic through a local proxy, identical to the desktop Electron app. This adds:

- Client-side remote attestation via the Contrast SDK
- HPKE field-level end-to-end encryption on top of TLS
- Manifest verification with hash display in the Security screen

The JNI bridge and build scripts are in place for when the Contrast SDK
adds Android/mobile support.

## Building the Native Proxy (Optional)

> **Note:** The Contrast SDK currently requires Linux-specific interfaces
> (AMD SEV-SNP) that are not available on Android. The native proxy cross-compilation
> will succeed once upstream support is added. In the meantime, the app works
> fully in direct HTTPS mode.

```bash
# Set Android NDK path
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/<version>

# Run the cross-compilation script
./scripts/build-native.sh
```

This would produce `libprivatemode.so` files in `app/src/main/jniLibs/` for each
supported architecture (arm64-v8a, armeabi-v7a, x86_64).

## Architecture

```
┌──────────────────────────────────┐
│     Android App (Kotlin/Compose) │
│  ┌────────────────────────────┐  │
│  │     UI Layer (Compose)     │  │
│  │  Chat │ Settings │Security │  │
│  └────────────┬───────────────┘  │
│  ┌────────────┴───────────────┐  │
│  │     Repository Layer       │  │
│  └────────────┬───────────────┘  │
│  ┌────────────┴───────────────┐  │
│  │   HTTP Client (OkHttp)     │  │
│  └────────────┬───────────────┘  │
│  ┌────────────┴───────────────┐  │
│  │   ProxyManager             │  │
│  │   ┌──────────────────────┐ │  │
│  │   │ Native Proxy (JNI)   │ │  │
│  │   │ libprivatemode.so    │ │  │
│  │   │ - Attestation        │ │  │
│  │   │ - HPKE encryption    │ │  │
│  │   └──────────────────────┘ │  │
│  │   OR                       │  │
│  │   ┌──────────────────────┐ │  │
│  │   │ Direct HTTPS         │ │  │
│  │   │ (TLS to backend)     │ │  │
│  │   └──────────────────────┘ │  │
│  └────────────┬───────────────┘  │
└───────────────┼──────────────────┘
                │ HTTPS
                ▼
┌──────────────────────────────────┐
│  Privatemode Backend (TEE)       │
│  api.privatemode.ai:443          │
│  AMD SEV-SNP + NVIDIA H100      │
└──────────────────────────────────┘
```

## Features

- **Onboarding** — Welcome screen with API key setup (UUID v4 validation)
- **Chat** — Multi-turn conversations with streaming SSE responses
- **Model selection** — gpt-oss-120b, Gemma 3 27B, Qwen3 Coder 30B
- **File upload** — Document upload via unstructured API
- **Extended thinking** — Reasoning mode toggle for supported models
- **Chat history** — Persistent local storage with date grouping
- **Chat management** — Create, rename, delete conversations
- **Word count tracking** — Context limit with visual indicators
- **Markdown rendering** — Rich message display (code, tables, lists)
- **Security dashboard** — Connection security info and attestation details
- **Settings** — API key management, danger zone for data deletion
- **Direct HTTPS** — TLS-encrypted connection to TEE-protected backend
- **Native proxy** (future) — Client-side attestation + HPKE encryption via JNI
