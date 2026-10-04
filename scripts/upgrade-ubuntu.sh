#!/usr/bin/env bash
set -euo pipefail
VERSION="${ONEPANE_VERSION:-latest}"
RELEASE_BASE="${ONEPANE_RELEASE_BASE_URL:-https://github.com/DigiLogicTech/OnePane/releases}"
PREFIX="${ONEPANE_PREFIX:-/usr/local}"
DATA_DIR="${ONEPANE_DATA_DIR:-/var/lib/onepane}"
BINARY_OVERRIDE="${ONEPANE_BINARY:-}"
[[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo "run as root" >&2; exit 1; }
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "unsupported architecture" >&2; exit 1;; esac

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
if [[ -n "$BINARY_OVERRIDE" ]]; then
  cp "$BINARY_OVERRIDE" "$tmp/onepane"
else
  artifact="onepane-linux-${ARCH}"
  if [[ "$VERSION" == "latest" ]]; then url="$RELEASE_BASE/latest/download/$artifact"; else url="$RELEASE_BASE/download/$VERSION/$artifact"; fi
  curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/onepane" "$url"
  curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/onepane.sha256" "$url.sha256"
  (cd "$tmp" && sha256sum -c onepane.sha256)
fi
chmod 0755 "$tmp/onepane"

systemctl stop onepane.service
backup="$DATA_DIR/backups/pre-upgrade-$(date -u +%Y%m%dT%H%M%SZ)"
install -d -o onepane -g onepane -m 0700 "$backup"
# The daemon is stopped, so SQLite/WAL state is quiescent before the backup.
for f in "$DATA_DIR/state/harness.db" "$DATA_DIR/state/harness.db-wal" "$DATA_DIR/state/harness.db-shm"; do [[ -f "$f" ]] && cp -a "$f" "$backup/"; done
cp -a "$PREFIX/bin/onepane" "$backup/onepane.previous" 2>/dev/null || true
install -m 0755 "$tmp/onepane" "$PREFIX/bin/onepane.new"
mv -f "$PREFIX/bin/onepane.new" "$PREFIX/bin/onepane"

if ! systemctl start onepane.service; then
  cp -a "$backup/onepane.previous" "$PREFIX/bin/onepane" 2>/dev/null || true
  systemctl start onepane.service || true
  echo "upgrade failed; previous binary restored" >&2
  exit 1
fi
for _ in $(seq 1 30); do curl --silent --fail http://127.0.0.1:8080/v1/health >/dev/null && { echo "OnePane upgraded successfully"; exit 0; }; sleep 1; done
systemctl stop onepane.service || true
cp -a "$backup/onepane.previous" "$PREFIX/bin/onepane" 2>/dev/null || true
systemctl start onepane.service || true
echo "upgrade health check failed; previous binary restored" >&2
exit 1
