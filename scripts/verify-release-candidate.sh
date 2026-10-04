#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo '[1/6] Structural validators'
for v in scripts/validate_*.py; do
  python3 "$v"
done

echo '[2/6] Shell syntax'
for s in scripts/*.sh; do
  bash -n "$s"
done

echo '[3/6] Python syntax'
python3 -m py_compile scripts/validate_*.py

echo '[4/6] Resolve and verify pinned Go modules'
go mod download
go mod verify

echo '[5/6] Go tests, integration tests, vet, race detector'
go test ./...
go test -tags=integration ./...
go vet ./...
go test -race ./...

echo '[6/6] Release build'
rm -rf dist
ONEPANE_VERSION="${ONEPANE_VERSION:-v0.1.0-rc8}" scripts/build-release.sh

echo 'OnePane release-candidate verification: PASS'
