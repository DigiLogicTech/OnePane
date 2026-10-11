#!/usr/bin/env bash
# Build a fixture MSI, verify its metadata and extract its single payload.
# No existing OnePane/MangaMesh installation or application data is touched.
set -euo pipefail
project="${1:-}"
output="${2:-}"
case "$project" in
  MangaMesh|OnePane) ;;
  *) echo "Usage: bash probe.sh MangaMesh|OnePane OUTPUT_DIRECTORY" >&2; exit 2 ;;
esac
if [[ -z "$output" ]]; then
  echo "Output directory is required" >&2
  exit 2
fi
command -v wixl >/dev/null
command -v msiinfo >/dev/null
command -v msiextract >/dev/null
source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
cd "$source_dir"
printf 'wixl version: '
wixl --version
msi="$output/${project}-WIXL-TOOLCHAIN-PROBE-NOT-FOR-INSTALL.msi"
wixl --arch x64 -o "$msi" "$source_dir/toolchain-probe.wxs"
test -s "$msi"
msiinfo tables "$msi" > "$scratch/tables.txt"
grep -Eq '(^|[[:space:]])(File|Component|Property)([[:space:]]|$)' "$scratch/tables.txt"
msiinfo export "$msi" Property > "$scratch/properties.txt"
grep -F 'DigiLogic Wixl Toolchain Probe - DO NOT INSTALL' "$scratch/properties.txt" >/dev/null
mkdir -p "$scratch/extracted"
msiextract -C "$scratch/extracted" "$msi" >/dev/null
match="$(find "$scratch/extracted" -type f -name probe.txt -print -quit)"
test -n "$match"
grep -F 'DigiLogic wixl/msitools disposable toolchain validation only.' "$match" >/dev/null
echo "WIXL_PROBE_OK project=$project package=$msi"
