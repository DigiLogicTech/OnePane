#!/usr/bin/env bash
# Write a Windows release source archive. Deferred Ubuntu packaging stays in
# git and is build-tagged for Linux; it is not copied into this archive.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
exclude_file="$root/packaging/windows/release-archive-excludes.txt"
[[ -f "$exclude_file" ]] || { echo "missing $exclude_file" >&2; exit 1; }
args=(archive --format=tar HEAD -- .)
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [[ -z "$line" ]] && continue
  args+=(":(exclude)$line")
done < "$exclude_file"
exec git "${args[@]}"
