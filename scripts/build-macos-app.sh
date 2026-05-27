#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-v$(date +'%Y.%m.%d')}"
APP_NAME="NasNotify-Go"
BUNDLE_ID="com.autunn.nasnotify-go"
BUILD_DIR="$ROOT_DIR/build/macos"
DIST_DIR="$ROOT_DIR/dist"
APP_DIR="$DIST_DIR/$APP_NAME.app"

case "$(uname -m)" in
  arm64) GOARCH="arm64" ;;
  x86_64) GOARCH="amd64" ;;
  *) echo "Unsupported macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

rm -rf "$BUILD_DIR" "$APP_DIR"
mkdir -p "$BUILD_DIR" "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$DIST_DIR"

echo "==> Build Go backend ($GOARCH)"
(
  cd "$ROOT_DIR"
  GOOS=darwin GOARCH="$GOARCH" CGO_ENABLED=0 \
    go build -ldflags "-s -w -X main.Version=$VERSION" \
    -o "$BUILD_DIR/nasnotify-go-app" ./cmd/nasnotify
)

echo "==> Build Swift menu bar app"
(
  cd "$ROOT_DIR/macos/NasNotifyGo"
  swift build -c release
)

SWIFT_BIN="$ROOT_DIR/macos/NasNotifyGo/.build/release/NasNotifyGo"
ICON_ICNS="$ROOT_DIR/macos/NasNotifyGo/Sources/NasNotifyGo/Resources/AppIcon.icns"

if [[ ! -f "$SWIFT_BIN" ]]; then
  echo "Swift binary not found: $SWIFT_BIN" >&2
  exit 1
fi

if [[ ! -f "$ICON_ICNS" ]]; then
  echo "App icon not found: $ICON_ICNS" >&2
  exit 1
fi

cp "$SWIFT_BIN" "$APP_DIR/Contents/MacOS/NasNotifyGo"
cp "$BUILD_DIR/nasnotify-go-app" "$APP_DIR/Contents/Resources/nasnotify-go-app"
cp "$ICON_ICNS" "$APP_DIR/Contents/Resources/AppIcon.icns"

cat > "$APP_DIR/Contents/Resources/service-runner.sh" <<'RUNNER'
#!/usr/bin/env bash
set -euo pipefail
APP_SUPPORT="$HOME/Library/Application Support/NasNotify-Go"
mkdir -p "$APP_SUPPORT/config" "$APP_SUPPORT/data"
cd "$APP_SUPPORT"
RESOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$RESOURCE_DIR/nasnotify-go-app"
RUNNER
chmod +x "$APP_DIR/Contents/Resources/service-runner.sh"

cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>zh_CN</string>
  <key>CFBundleDisplayName</key>
  <string>$APP_NAME</string>
  <key>CFBundleExecutable</key>
  <string>NasNotifyGo</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleIdentifier</key>
  <string>$BUNDLE_ID</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>$APP_NAME</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$VERSION</string>
  <key>CFBundleVersion</key>
  <string>$VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST

chmod +x "$APP_DIR/Contents/MacOS/NasNotifyGo"

echo "==> Created: $APP_DIR"
echo "You can run it with: open '$APP_DIR'"
