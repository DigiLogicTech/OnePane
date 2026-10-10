#!/usr/bin/env bash
# Build a disposable MSI candidate containing actual Windows OnePane binaries.
# This is intentionally NOT the current production installer.
set -euo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tooling="$repo/packaging/wixl"
payload="$repo/build/wixl-onepane-candidate/payload"
out="$repo/build/wixl-onepane-candidate/output"
mkdir -p "$payload" "$out"
command -v go >/dev/null || { echo "Go 1.23+ is required" >&2; exit 1; }
command -v docker >/dev/null || { echo "Docker is required" >&2; exit 1; }
docker info >/dev/null || { echo "Docker daemon access required" >&2; exit 1; }
cd "$repo"
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -ldflags="-s -w" -o "$payload/OnePane.Backend.exe" ./cmd/harnessd
go build -trimpath -ldflags="-s -w" -o "$payload/OnePane.Service.exe" ./packaging/windows/service
go build -trimpath -ldflags="-s -w -H windowsgui" -o "$payload/OnePane.Desktop.exe" ./packaging/windows/desktop
cp packaging/windows/assets/OnePane.ico "$payload/OnePane.ico"
for executable in OnePane.Backend.exe OnePane.Service.exe OnePane.Desktop.exe; do
  test -s "$payload/$executable"
  file "$payload/$executable" | grep -Eiq 'PE32\+.*x86-64|PE32\+.*AMD64'
done
sha256sum "$payload"/OnePane.* > "$out/payload.sha256"
image="digilogic-wixl-toolchain-ubuntu2604:0.106"
docker build --pull -f "$tooling/Dockerfile" -t "$image" "$tooling"
docker run --rm --read-only --network none --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,nosuid,nodev,size=384m \
  -v "$tooling:/probe:ro" -v "$payload:/payload:ro" \
  -v "$out:/out:rw" \
  "$image" bash /probe/validate-onepane-candidate.sh
echo "OnePane Linux wixl candidate validated. DO NOT INSTALL OR RELEASE."
