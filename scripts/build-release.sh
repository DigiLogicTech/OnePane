#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${ONEPANE_VERSION:-dev}"
OUT="${ONEPANE_OUT:-$ROOT/dist}"
mkdir -p "$OUT"

case "$(uname -s)" in Linux) ;; *) echo "build-release must run on Linux for the v0.1 release profile" >&2; exit 1;; esac

echo "Resolving pinned Go module graph"
(cd "$ROOT" && go mod download && go mod verify)

for arch in amd64 arm64; do
  name="onepane-linux-${arch}"
  echo "Building $name"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/harnessd
  sha256sum "$OUT/$name" > "$OUT/$name.sha256"
done

cp "$ROOT/scripts/install-ubuntu.sh" "$OUT/install.sh"
cp "$ROOT/scripts/upgrade-ubuntu.sh" "$OUT/upgrade.sh"
echo "Release artifacts written to $OUT"
