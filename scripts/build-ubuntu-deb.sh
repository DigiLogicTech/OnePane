#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${ONEPANE_VERSION:?ONEPANE_VERSION is required}"
VERSION="$ONEPANE_VERSION"
REVISION="${ONEPANE_REVISION:-${GITHUB_SHA:-unknown}}"
BUILD_TIME="${ONEPANE_BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ARCH="${ONEPANE_ARCH:-amd64}"
OUT="${ONEPANE_OUT:-$ROOT/dist/ubuntu}"

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported ONEPANE_ARCH: $ARCH" >&2; exit 2 ;;
esac

DEB_VERSION="${VERSION#v}"
PKGROOT="$(mktemp -d)"
trap 'rm -rf "$PKGROOT"' EXIT

mkdir -p "$OUT" \
  "$PKGROOT/DEBIAN" \
  "$PKGROOT/usr/bin" \
  "$PKGROOT/lib/systemd/system" \
  "$PKGROOT/usr/share/doc/onepane"

printf 'Resolving Go module graph\n'
(cd "$ROOT" && go mod download && go mod verify)

printf 'Building OnePane Linux %s\n' "$ARCH"
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
  go build -trimpath -ldflags "-s -w -X github.com/DigiLogicTech/OnePane/internal/buildinfo.Version=$VERSION -X github.com/DigiLogicTech/OnePane/internal/buildinfo.Revision=$REVISION -X github.com/DigiLogicTech/OnePane/internal/buildinfo.BuildTime=$BUILD_TIME" \
  -o "$PKGROOT/usr/bin/onepane" ./cmd/harnessd)

install -m 0644 "$ROOT/packaging/debian/onepane.service" "$PKGROOT/lib/systemd/system/onepane.service"
if [[ -f "$ROOT/LICENSE" ]]; then
  install -m 0644 "$ROOT/LICENSE" "$PKGROOT/usr/share/doc/onepane/copyright"
fi
if [[ -f "$ROOT/NOTICE" ]]; then
  install -m 0644 "$ROOT/NOTICE" "$PKGROOT/usr/share/doc/onepane/NOTICE"
fi
install -m 0755 "$ROOT/packaging/debian/postinst" "$PKGROOT/DEBIAN/postinst"
install -m 0755 "$ROOT/packaging/debian/prerm" "$PKGROOT/DEBIAN/prerm"
install -m 0755 "$ROOT/packaging/debian/postrm" "$PKGROOT/DEBIAN/postrm"

cat > "$PKGROOT/DEBIAN/control" <<CONTROL
Package: onepane
Version: $DEB_VERSION
Section: utils
Priority: optional
Architecture: $ARCH
Maintainer: DigiLogic <digilogicenterprises@gmail.com>
Depends: adduser, ca-certificates, curl, podman, uidmap, slirp4netns, fuse-overlayfs
Homepage: https://github.com/DigiLogicTech/OnePane
Description: Local-first AI control plane and orchestration harness
 OnePane provides durable agents, task orchestration, model routing,
 sandboxed project execution, verification, automation, and managed local AI.
CONTROL

OUTPUT="$OUT/onepane_${VERSION#v}_${ARCH}.deb"
dpkg-deb --root-owner-group --build "$PKGROOT" "$OUTPUT"
(cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256")

echo "Built $OUTPUT"
cat "$OUTPUT.sha256"
