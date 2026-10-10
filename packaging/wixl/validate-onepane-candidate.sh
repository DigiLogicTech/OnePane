#!/usr/bin/env bash
# Execute in restricted wixl container. Fail closed on any missing installer
# table or discrepancy between the input PE payload and MSI extraction.
set -euo pipefail
cd /probe
command -v wixl
command -v msiinfo
command -v msiextract
msi="/out/OnePane-wixl-REAL-PAYLOAD-CANDIDATE-DO-NOT-INSTALL.msi"
rm -f -- "$msi"
wixl --arch x64 -D PayloadDir=/payload -o "$msi" /probe/onepane-candidate.wxs
test -s "$msi"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
msiinfo tables "$msi" > "$scratch/tables.txt"
for table in Property File Component ServiceInstall ServiceControl Registry Upgrade; do
  grep -Eq "(^|[[:space:]])${table}([[:space:]]|$)" "$scratch/tables.txt" || {
    echo "Missing required MSI table: $table" >&2; exit 1;
  }
  msiinfo export "$msi" "$table" > "$scratch/$table.tsv"
done
grep -F "OnePane Linux wixl Candidate - DO NOT INSTALL" "$scratch/Property.tsv"
grep -F "C771A020-BC81-46D6-92C3-35B80E101102" "$scratch/Property.tsv"
grep -F "OnePaneWixlCandidate" "$scratch/ServiceInstall.tsv"
grep -F "OnePaneWixlCandidate" "$scratch/ServiceControl.tsv"
grep -F "OnePaneWixlCandidate" "$scratch/Registry.tsv"
if grep -F "9F7D57AE-B2C4-44E2-96E4-F09E145AE235" "$scratch/Property.tsv"; then
  echo "FAIL: Candidate accidentally inherited the production OnePane upgrade identity" >&2; exit 1
fi
mkdir -p "$scratch/extracted"
msiextract -C "$scratch/extracted" "$msi" > "$scratch/extraction.txt"
for name in OnePane.Backend.exe OnePane.Service.exe OnePane.Desktop.exe OnePane.ico; do
  matches="$(find "$scratch/extracted" -type f -name "$name" | wc -l)"
  if [[ "$matches" -ne 1 ]]; then
    echo "FAIL: Expected one extracted $name, found $matches" >&2; exit 1
  fi
  extracted="$(find "$scratch/extracted" -type f -name "$name" -print -quit)"
  cmp "/payload/$name" "$extracted" || {
    echo "FAIL: MSI payload differs from cross-compiled binary $name" >&2; exit 1;
  }
done
echo "ONEPANE_WIXL_REAL_PAYLOAD_CANDIDATE_OK: service, upgrade table, registry, 4 exact payloads"
echo "NOT A RELEASE: does not validate custom storage UI, existing-data upgrades, Windows install or service start"
