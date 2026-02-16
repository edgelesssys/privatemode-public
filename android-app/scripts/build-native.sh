#!/bin/bash
# Build script for cross-compiling libprivatemode for Android.
#
# Prerequisites:
#   - Go 1.25+ installed
#   - Android NDK installed (via Android Studio SDK Manager or standalone)
#   - ANDROID_NDK_HOME environment variable set
#
# Usage:
#   ./scripts/build-native.sh
#
# This script compiles the Go proxy code from the parent repo's
# privatemode-proxy/libprivatemode/ directory into .so files for
# each Android architecture, then copies them to the JNI libs directory.
#
# The resulting libraries are loaded by the Android app at runtime
# and provide the same proxy functionality as the desktop app.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ANDROID_APP_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$ANDROID_APP_DIR")"
LIB_SOURCE="$REPO_ROOT/privatemode-proxy/libprivatemode"
JNI_LIBS_DIR="$ANDROID_APP_DIR/app/src/main/jniLibs"
JNI_C_DIR="$ANDROID_APP_DIR/app/src/main/cpp"

# Ensure Android NDK is available
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
    # Try common locations
    if [ -d "$HOME/Android/Sdk/ndk" ]; then
        ANDROID_NDK_HOME=$(ls -d "$HOME/Android/Sdk/ndk/"* 2>/dev/null | sort -V | tail -1)
    elif [ -d "$HOME/Library/Android/sdk/ndk" ]; then
        ANDROID_NDK_HOME=$(ls -d "$HOME/Library/Android/sdk/ndk/"* 2>/dev/null | sort -V | tail -1)
    fi

    if [ -z "${ANDROID_NDK_HOME:-}" ]; then
        echo "ERROR: ANDROID_NDK_HOME not set and NDK not found in default locations."
        echo "Install the NDK via Android Studio SDK Manager or set ANDROID_NDK_HOME."
        exit 1
    fi
fi

echo "Using Android NDK: $ANDROID_NDK_HOME"
echo "Go library source: $LIB_SOURCE"
echo "JNI output directory: $JNI_LIBS_DIR"

# Architecture configurations: GOARCH, Android ABI, NDK toolchain target
declare -A ARCH_MAP=(
    ["arm64"]="arm64-v8a:aarch64-linux-android"
    ["amd64"]="x86_64:x86_64-linux-android"
    ["arm"]="armeabi-v7a:armv7a-linux-androideabi"
)

# Minimum API level
MIN_API=26

for goarch in "${!ARCH_MAP[@]}"; do
    IFS=':' read -r abi ndk_target <<< "${ARCH_MAP[$goarch]}"

    echo ""
    echo "========================================="
    echo "Building for $abi (GOARCH=$goarch)"
    echo "========================================="

    OUTPUT_DIR="$JNI_LIBS_DIR/$abi"
    mkdir -p "$OUTPUT_DIR"

    # Set up the NDK toolchain
    TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64"
    if [ ! -d "$TOOLCHAIN" ]; then
        TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/darwin-x86_64"
    fi
    if [ ! -d "$TOOLCHAIN" ]; then
        TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/darwin-arm64"
    fi

    CC="${TOOLCHAIN}/bin/${ndk_target}${MIN_API}-clang"
    CXX="${TOOLCHAIN}/bin/${ndk_target}${MIN_API}-clang++"

    if [ ! -f "$CC" ]; then
        echo "WARNING: Compiler not found at $CC, skipping $abi"
        continue
    fi

    # Cross-compile the Go library as a shared C library.
    # The contrast_unstable_api tag is required by the Contrast SDK (all its
    # files are gated behind this build constraint). This matches the desktop
    # build (see nix/packages/by-name/privatemode-proxy/package.nix).
    (
        cd "$LIB_SOURCE"
        CGO_ENABLED=1 \
        GOOS=android \
        GOARCH="$goarch" \
        CC="$CC" \
        CXX="$CXX" \
        go build -buildmode=c-shared \
            -tags contrast_unstable_api \
            -o "$OUTPUT_DIR/libprivatemode_go.so" \
            .
    )

    echo "Built libprivatemode_go.so for $abi"

    # Now compile the JNI bridge C code and link against the Go shared library
    "$CC" -shared -fPIC \
        -I"$OUTPUT_DIR" \
        -o "$OUTPUT_DIR/libprivatemode.so" \
        "$JNI_C_DIR/privatemode_jni.c" \
        -L"$OUTPUT_DIR" \
        -lprivatemode_go \
        -llog

    echo "Built libprivatemode.so (JNI bridge) for $abi"
done

echo ""
echo "========================================="
echo "Build complete! Native libraries:"
find "$JNI_LIBS_DIR" -name "*.so" -exec ls -lh {} \;
echo "========================================="
