#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${ONEPANE_VERSION:-0.1.0-alpha.3}"
OUT="${ONEPANE_OUT:-$ROOT/dist/macos}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

[[ "$(uname -s)" == "Darwin" ]] || { echo "build-macos.sh must run on macOS" >&2; exit 1; }
command -v xcrun >/dev/null
command -v lipo >/dev/null
command -v hdiutil >/dev/null

mkdir -p "$OUT" "$WORK/backend" "$WORK/app-shell"

(cd "$ROOT" && go mod download && go mod verify)

for arch in amd64 arm64; do
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" \
    -o "$WORK/backend/OnePane.Backend-$arch" ./cmd/harnessd)
done
lipo -create \
  "$WORK/backend/OnePane.Backend-amd64" \
  "$WORK/backend/OnePane.Backend-arm64" \
  -output "$WORK/OnePane.Backend"

SDK="$(xcrun --sdk macosx --show-sdk-path)"
for target in x86_64-apple-macos13.0 arm64-apple-macos13.0; do
  suffix="${target%%-*}"
  xcrun swiftc \
    -sdk "$SDK" \
    -target "$target" \
    -framework Cocoa \
    -framework WebKit \
    -O \
    "$ROOT/packaging/macos/OnePaneApp.swift" \
    -o "$WORK/app-shell/OnePane-$suffix"
done
lipo -create \
  "$WORK/app-shell/OnePane-x86_64" \
  "$WORK/app-shell/OnePane-arm64" \
  -output "$WORK/OnePane"

APP="$WORK/OnePane.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
install -m 0755 "$WORK/OnePane" "$APP/Contents/MacOS/OnePane"
install -m 0755 "$WORK/OnePane.Backend" "$APP/Contents/MacOS/OnePane.Backend"
install -m 0644 "$ROOT/packaging/macos/Info.plist" "$APP/Contents/Info.plist"

ICONSET="$WORK/OnePane.iconset"
mkdir -p "$ICONSET"
SOURCE_ICON="$ROOT/packaging/windows/assets/OnePane.png"
sips -z 16 16     "$SOURCE_ICON" --out "$ICONSET/icon_16x16.png" >/dev/null
sips -z 32 32     "$SOURCE_ICON" --out "$ICONSET/icon_16x16@2x.png" >/dev/null
sips -z 32 32     "$SOURCE_ICON" --out "$ICONSET/icon_32x32.png" >/dev/null
sips -z 64 64     "$SOURCE_ICON" --out "$ICONSET/icon_32x32@2x.png" >/dev/null
sips -z 128 128   "$SOURCE_ICON" --out "$ICONSET/icon_128x128.png" >/dev/null
sips -z 256 256   "$SOURCE_ICON" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
sips -z 256 256   "$SOURCE_ICON" --out "$ICONSET/icon_256x256.png" >/dev/null
sips -z 512 512   "$SOURCE_ICON" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
sips -z 512 512   "$SOURCE_ICON" --out "$ICONSET/icon_512x512.png" >/dev/null
sips -z 1024 1024 "$SOURCE_ICON" --out "$ICONSET/icon_512x512@2x.png" >/dev/null
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/OnePane.icns"

# Ad-hoc signing keeps the internal alpha self-consistent without requiring an
# Apple Developer identity. Distribution signing/notarization will replace this.
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

STAGE="$WORK/dmg-stage"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/OnePane.app"
ln -s /Applications "$STAGE/Applications"

DMG="$OUT/OnePane-v${VERSION#v}-macos-universal.dmg"
hdiutil create -volname "OnePane Alpha 3" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
shasum -a 256 "$DMG" > "$DMG.sha256"

lipo -info "$APP/Contents/MacOS/OnePane"
lipo -info "$APP/Contents/MacOS/OnePane.Backend"
echo "Built $DMG"
cat "$DMG.sha256"
