#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${ONEPANE_VERSION:-0.1.0-alpha.3}"
ARCH="${ONEPANE_ARCH:-amd64}"
OUT="${ONEPANE_OUT:-$ROOT/dist/ubuntu}"

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported ONEPANE_ARCH: $ARCH" >&2; exit 2 ;;
esac

DEB_VERSION="${VERSION#v}"
DEB_VERSION="${DEB_VERSION/-alpha./~alpha.}"
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
  go build -trimpath -ldflags "-s -w" \
  -o "$PKGROOT/usr/bin/onepane" ./cmd/harnessd)

install -m 0644 "$ROOT/packaging/debian/onepane.service" "$PKGROOT/lib/systemd/system/onepane.service"
install -m 0644 "$ROOT/LICENSE" "$PKGROOT/usr/share/doc/onepane/copyright"
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
sha256sum "$OUTPUT" > "$OUTPUT.sha256"

echo "Built $OUTPUT"
cat "$OUTPUT.sha256"
