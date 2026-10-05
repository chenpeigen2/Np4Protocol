#!/usr/bin/env bash
# Build the np4bridge native library and place artifacts where each Flutter
# platform build expects them.
#
#   Usage: ./tool/build_native.sh [target ...]
#   Targets: android macos windows linux   (default: every one that is possible)
#
# Artifacts:
#   android  -> android/app/src/main/jniLibs/<abi>/libnp4bridge.so   (3 ABIs)
#   macos    -> native/macos/libnp4bridge.dylib                      (universal arm64+x86_64, @rpath id)
#   windows  -> native/windows/np4bridge.dll                         (needs mingw-w64)
#   linux    -> native/linux/libnp4bridge.so                         (native or zig cc cross)
#
# iOS is intentionally not a target (deferred by product decision).
set -euo pipefail

FLUTTER_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_DIR="$(cd "$FLUTTER_DIR/../.." && pwd)"
GO_DIR="$REPO_DIR/go"
BRIDGE_PKG="./cmd/np4bridge"

NATIVE_DIR="$FLUTTER_DIR/native"
JNI_LIBS="$FLUTTER_DIR/android/app/src/main/jniLibs"

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
  targets=(android macos windows linux)
fi

# -s -w strips symbol tables and DWARF: the bridge ships to end users, not
# debuggers (60MB -> ~20MB per artifact).
STRIP="-ldflags=-s -w"

run_go() {
  (cd "$GO_DIR" && env "$@" go build "$STRIP" -buildmode=c-shared -o "$out" "$BRIDGE_PKG")
}

build_android() {
  local ndk="${ANDROID_NDK_HOME:-$HOME/Library/Android/sdk/ndk}"
  if [ ! -d "$ndk" ]; then
    echo "skip android: no NDK at $ndk (set ANDROID_NDK_HOME)"; return 0
  fi
  # Pick the newest installed NDK version.
  local toolchain
  toolchain="$(ls -d "$ndk"/*/toolchains/llvm/prebuilt/*/bin 2>/dev/null | sort -V | tail -1)"
  if [ -z "$toolchain" ]; then
    echo "skip android: no toolchain under $ndk"; return 0
  fi
  local api=21
  mkdir -p "$JNI_LIBS"/{arm64-v8a,armeabi-v7a,x86_64}
  echo "android: NDK toolchain $toolchain (API $api)"
  for spec in "arm64-v8a arm64  aarch64-linux-android${api}-clang        " \
              "armeabi-v7a arm   armv7a-linux-androideabi${api}-clang   " \
              "x86_64     amd64 x86_64-linux-android${api}-clang         "; do
    # shellcheck disable=SC2206
    abi=($spec); out="$JNI_LIBS/${abi[0]}/libnp4bridge.so"
    echo "  -> ${abi[0]}"
    # -checklinkname=0: go-libp2p's anet uses //go:linkname on net.zoneCache,
    # which Go 1.23+ rejects by default (wlynxg/anet#how-to-build).
    (cd "$GO_DIR" && env GOOS=android GOARCH="${abi[1]}" CGO_ENABLED=1 \
      CC="$toolchain/${abi[2]}" go build -ldflags="-s -w -checklinkname=0" -buildmode=c-shared -o "$out" "$BRIDGE_PKG")
  done
  echo "android OK: $JNI_LIBS/*/libnp4bridge.so"
}

build_macos() {
  if [ "$(uname -s)" != "Darwin" ]; then
    echo "skip macos: not on macOS"; return 0
  fi
  mkdir -p "$NATIVE_DIR/macos"
  local tmp="$NATIVE_DIR/macos/.tmp"
  mkdir -p "$tmp"

  echo "macos: arm64"
  (cd "$GO_DIR" && env GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
    go build "$STRIP" -buildmode=c-shared -o "$tmp/lib-arm64.dylib" "$BRIDGE_PKG")
  echo "macos: x86_64 (cross)"
  (cd "$GO_DIR" && env GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 CC="clang -arch x86_64" \
    go build "$STRIP" -buildmode=c-shared -o "$tmp/lib-amd64.dylib" "$BRIDGE_PKG")

  lipo -create -output "$NATIVE_DIR/macos/libnp4bridge.dylib" \
    "$tmp/lib-arm64.dylib" "$tmp/lib-amd64.dylib"
  # The pod vendors this dylib into the app's Frameworks dir; the binary's
  # rpath (@executable_path/../Frameworks) resolves @rpath against it.
  install_name_tool -id @rpath/libnp4bridge.dylib "$NATIVE_DIR/macos/libnp4bridge.dylib"
  rm -rf "$tmp"
  echo "macos OK: $NATIVE_DIR/macos/libnp4bridge.dylib (universal)"
}

build_windows() {
  mkdir -p "$NATIVE_DIR/windows"
  if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    echo "windows: x86_64 via mingw-w64"
    (cd "$GO_DIR" && env GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
      CC=x86_64-w64-mingw32-gcc go build "$STRIP" -buildmode=c-shared -o "$NATIVE_DIR/windows/np4bridge.dll" "$BRIDGE_PKG")
  elif command -v zig >/dev/null 2>&1; then
    echo "windows: x86_64 via zig cc"
    (cd "$GO_DIR" && env GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
      CC="zig cc -target x86_64-windows-gnu" go build "$STRIP" -buildmode=c-shared -o "$NATIVE_DIR/windows/np4bridge.dll" "$BRIDGE_PKG")
  else
    echo "skip windows: no mingw-w64 or zig (brew install mingw-w64 / zig)"; return 0
  fi
  echo "windows OK: $NATIVE_DIR/windows/np4bridge.dll"
}

build_linux() {
  mkdir -p "$NATIVE_DIR/linux"
  if [ "$(uname -s)" = "Linux" ]; then
    echo "linux: native $(uname -m)"
    (cd "$GO_DIR" && env CGO_ENABLED=1 \
      go build "$STRIP" -buildmode=c-shared -o "$NATIVE_DIR/linux/libnp4bridge.so" "$BRIDGE_PKG")
  elif command -v zig >/dev/null 2>&1; then
    echo "linux: cross via zig cc (x86_64)"
    (cd "$GO_DIR" && env GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
      CC="zig cc -target x86_64-linux-gnu" go build "$STRIP" -buildmode=c-shared -o "$NATIVE_DIR/linux/libnp4bridge.so" "$BRIDGE_PKG")
  else
    echo "skip linux: build on Linux, or install zig for cross-compiling"; return 0
  fi
  echo "linux OK: $NATIVE_DIR/linux/libnp4bridge.so"
}

for t in "${targets[@]}"; do
  case "$t" in
    android) build_android ;;
    macos)   build_macos ;;
    windows) build_windows ;;
    linux)   build_linux ;;
    *) echo "unknown target: $t"; exit 2 ;;
  esac
done
