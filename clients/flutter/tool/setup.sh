#!/usr/bin/env bash
# One-time (idempotent) setup for the Flutter client:
#   1. Generate the four platform folders (android/windows/macos/linux — no iOS).
#   2. Build the native np4bridge library for every possible target.
#   3. Apply platform glue (macOS pod + entitlements, Windows/Linux bundling).
#   4. pub get / analyze / test.
#
# Requires the Flutter SDK on PATH (https://docs.flutter.dev/get-started/install).
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v flutter >/dev/null 2>&1; then
  echo "error: flutter not on PATH — install the Flutter SDK first."
  echo "  macOS:   brew install --cask flutter"
  echo "  Windows: choco install flutter (or download the SDK zip)"
  exit 1
fi

PLATFORMS="android,windows,macos,linux"

# --- 1. Platform scaffolding -------------------------------------------------
if [ ! -d android ] || [ ! -d windows ] || [ ! -d macos ] || [ ! -d linux ]; then
  echo "==> flutter create ($PLATFORMS)"
  flutter create --project-name np4_client --org app.np4 --platforms "$PLATFORMS" .
fi

# --- 2. Native library -------------------------------------------------------
echo "==> building native np4bridge"
./tool/build_native.sh

# --- 3. Platform glue --------------------------------------------------------
echo "==> applying platform glue"

# macOS: vendor the dylib as a pod so it lands in .app/Contents/Frameworks
# and links against the Runner binary.
cat > macos/Np4Bridge.podspec <<'EOF'
Pod::Spec.new do |s|
  s.name             = 'Np4Bridge'
  s.version          = '0.1.0'
  s.summary          = 'Native np4 mixnet bridge for the Flutter client.'
  s.description      = 'Go-built np4 bridge (libnp4bridge.dylib); see go/cmd/np4bridge.'
  s.homepage         = 'https://example.invalid/np4'
  s.license          = { :type => 'MIT' }
  s.authors          = { 'Np4Protocol' => 'dev@np4.invalid' }
  s.source           = { :path => '.' }
  s.platform         = :osx, '10.15'
  s.vendored_libraries = 'native/libnp4bridge.dylib'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
EOF
mkdir -p macos/native
cp -f native/macos/libnp4bridge.dylib macos/native/libnp4bridge.dylib

if [ -f macos/Podfile ] && ! grep -q "pod 'Np4Bridge'" macos/Podfile; then
  # Insert inside the Runner target block, before the flutter pod helper.
  sed -i '' "s/target 'Runner' do/target 'Runner' do\\n  pod 'Np4Bridge', :path => '.'/" macos/Podfile
fi

# macOS sandbox: the app must be allowed to listen (DHT/relay) and dial.
for f in macos/Runner/DebugProfile.entitlements macos/Runner/Release.entitlements; do
  [ -f "$f" ] || continue
  grep -q "com.apple.security.network.client" "$f" || \
    sed -i '' 's#</dict>#\t<key>com.apple.security.network.client</key>\n\t<true/>\n</dict>#' "$f"
  grep -q "com.apple.security.network.server" "$f" || \
    sed -i '' 's#</dict>#\t<key>com.apple.security.network.server</key>\n\t<true/>\n</dict>#' "$f"
done

# Windows: copy the DLL next to the exe at build time.
if [ -f windows/CMakeLists.txt ] && ! grep -q np4bridge.dll windows/CMakeLists.txt; then
  cat >> windows/CMakeLists.txt <<'EOF'

# np4bridge native library (built by tool/build_native.sh)
add_custom_command(TARGET ${BINARY_NAME} POST_BUILD
  COMMAND ${CMAKE_COMMAND} -E copy_if_different
    "${CMAKE_SOURCE_DIR}/../native/windows/np4bridge.dll"
    $<TARGET_FILE_DIR:${BINARY_NAME}>)
EOF
fi

# Linux: install the .so into the bundle's lib dir (rpath $ORIGIN/lib resolves it).
if [ -f linux/CMakeLists.txt ] && ! grep -q np4bridge.so linux/CMakeLists.txt; then
  cat >> linux/CMakeLists.txt <<'EOF'

# np4bridge native library (built by tool/build_native.sh)
install(FILES "${CMAKE_SOURCE_DIR}/../native/linux/libnp4bridge.so"
  DESTINATION "${INSTALL_BUNDLE_LIB_DIR}"
  COMPONENT Runtime)
EOF
fi

# --- 4. Verify ---------------------------------------------------------------
echo "==> flutter pub get"
flutter pub get
echo "==> flutter analyze"
flutter analyze
echo "==> flutter test"
flutter test

echo
echo "setup complete. Run the app:"
echo "  flutter run -d macos|windows|linux|<android-device>"
