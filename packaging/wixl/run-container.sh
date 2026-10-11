#!/usr/bin/env bash
# Run the exact same disposable wixl/msitools toolchain from either repository.
set -euo pipefail
project="${1:-}"
case "$project" in MangaMesh|OnePane) ;; *)
  echo "Usage: bash packaging/wixl/run-container.sh MangaMesh|OnePane" >&2; exit 2 ;;
esac
tool_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$tool_root/../.." && pwd)"
output="$repo_root/build/wixl-toolchain-probe"
command -v docker >/dev/null || { echo "Docker is required" >&2; exit 1; }
docker info >/dev/null || { echo "Docker daemon access is required" >&2; exit 1; }
mkdir -p "$output"
image="digilogic-wixl-toolchain-ubuntu2604:0.106"
docker build --pull -f "$tool_root/Dockerfile" -t "$image" "$tool_root"
docker run --rm --read-only --network none --cap-drop ALL \
  --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,size=128m \
  --user "$(id -u):$(id -g)" \
  -v "$tool_root:/probe:ro" -v "$output:/out:rw" \
  "$image" bash /probe/probe.sh "$project" /out
echo "This MSI is a TOOLCHAIN PROBE only. It must not be installed or released."
