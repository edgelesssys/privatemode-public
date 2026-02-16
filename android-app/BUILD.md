# Building the Privatemode AI Android App

## Prerequisites

- Android Studio Arctic Fox or later (or Android Gradle Plugin 8.7+)
- Android SDK with API level 35
- Android NDK (for native proxy / TEE attestation)
- Go 1.25+ (for cross-compiling the proxy)
- JDK 17

## Quick Start (with TEE Attestation)

```bash
cd android-app

# Step 1: Set Android NDK path
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/<version>

# Step 2: Build the native proxy (TEE attestation + HPKE encryption)
./scripts/build-native.sh

# Step 3: Build the APK
./gradlew assembleDebug

# Step 4: Install on connected device/emulator
./gradlew installDebug
```

Or open the `android-app/` directory in Android Studio and build from the IDE.

### Without Native Proxy (direct HTTPS only)

If you skip the `build-native.sh` step, the app will fall back to direct HTTPS
mode: it still connects to `api.privatemode.ai` over TLS, but without client-side
attestation verification or HPKE encryption.

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
│       ├── jniLibs/               # Native libraries (built by build-native.sh)
│       └── res/                   # Android resources
├── scripts/
│   └── build-native.sh           # Cross-compile Go proxy for Android
├── build.gradle.kts              # Root build file
└── settings.gradle.kts
```

## Connection Modes

The app supports two connection modes, selected automatically at startup:

### 1. Native Proxy Mode (preferred)

When `libprivatemode.so` is present (built by `build-native.sh`), the app loads
it via JNI and routes all traffic through a local proxy. This is identical to how
the desktop Electron app operates, providing:

- **Client-side TEE attestation** — Verifies the AMD SEV-SNP attestation report
  from the Privatemode backend using the Contrast SDK. This cryptographically
  proves the backend is running unmodified code in a Trusted Execution Environment.
- **HPKE end-to-end encryption** — Field-level encryption on top of TLS using
  keys derived from the attested mesh CA certificate.
- **Manifest verification** — The Security screen displays the manifest hash,
  trusted measurement, product line, and TCB firmware versions.

### 2. Direct HTTPS Mode (fallback)

When the native proxy library is not present, the app connects directly to
`https://api.privatemode.ai` over TLS. The backend still runs in a TEE, but
the Android client cannot independently verify this.

## Building the Native Proxy

The build script cross-compiles the Go proxy from `privatemode-proxy/libprivatemode/`
for each Android architecture. It passes the `-tags contrast_unstable_api` build
tag required by the Contrast SDK (matching the desktop Nix build).

```bash
export ANDROID_NDK_HOME=$HOME/Android/Sdk/ndk/<version>
./scripts/build-native.sh
```

This produces `libprivatemode.so` files in `app/src/main/jniLibs/` for:
- `arm64-v8a` (most modern Android phones)
- `armeabi-v7a` (older 32-bit devices)
- `x86_64` (emulators)

### Why this works

The Contrast SDK's attestation *verification* is pure cryptography (ECDSA-P384,
x509 cert chains, SHA-512) with zero platform dependencies. Only attestation
report *generation* (accessing `/dev/sev-guest`) requires Linux — but the Android
app only needs to **verify** reports received from the backend, not generate them.

The dependency chain for `ValidateAttestation`:
```
Contrast SDK → snp/validator.go → go-sev-guest/verify (platform-independent)
             → tdx/validator.go → go-tdx-guest/verify (platform-independent)
```

The Linux-only `go-sev-guest/client` and `go-tdx-guest/client` packages are only
imported by the `issuer/` sub-packages (for report generation), which are not in
the `ValidateAttestation` import chain.

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
│  │   │ - TEE Attestation    │ │  │
│  │   │ - HPKE encryption    │ │  │
│  │   │ - Secret exchange    │ │  │
│  │   └──────────────────────┘ │  │
│  │   OR (fallback)            │  │
│  │   ┌──────────────────────┐ │  │
│  │   │ Direct HTTPS         │ │  │
│  │   │ (TLS only)           │ │  │
│  │   └──────────────────────┘ │  │
│  └────────────┬───────────────┘  │
└───────────────┼──────────────────┘
                │ HTTPS (E2E encrypted with proxy)
                ▼
┌──────────────────────────────────┐
│  Privatemode Backend (TEE)       │
│  api.privatemode.ai:443          │
│  AMD SEV-SNP + NVIDIA H100      │
└──────────────────────────────────┘
```

## Features

- **TEE attestation** — Client-side AMD SEV-SNP verification via Contrast SDK
- **E2E encryption** — HPKE field-level encryption via embedded Go proxy
- **Onboarding** — Welcome screen with API key setup (UUID v4 validation)
- **Chat** — Multi-turn conversations with streaming SSE responses
- **Model selection** — gpt-oss-120b, Gemma 3 27B, Qwen3 Coder 30B
- **File upload** — Document upload via unstructured API
- **Extended thinking** — Reasoning mode toggle for supported models
- **Chat history** — Persistent local storage with date grouping
- **Chat management** — Create, rename, delete conversations
- **Word count tracking** — Context limit with visual indicators
- **Markdown rendering** — Rich message display (code, tables, lists)
- **Security dashboard** — Attestation info, manifest hash, TCB versions
- **Settings** — API key management, danger zone for data deletion
