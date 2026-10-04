from pathlib import Path

root = Path(__file__).resolve().parents[1]
go_mod = (root / 'go.mod').read_text()
go_sum = (root / 'go.sum').read_text()
config = (root / 'config.example.yaml').read_text()
config_go = (root / 'internal/config/config.go').read_text()
build = (root / 'scripts/build-release.sh').read_text()

assert 'module github.com/DigiLogicTech/OnePane' in go_mod
assert 'github.com/example/harness' not in '\n'.join(
    p.read_text(errors='ignore')
    for p in root.rglob('*')
    if p.is_file() and '.git' not in p.parts and '__pycache__' not in p.parts and p.suffix != '.pyc' and p.name != 'validate_release_candidate.py'
), 'placeholder Go module/import path remains'
assert '/var/lib/harness' not in '\n'.join(
    p.read_text(errors='ignore')
    for p in root.rglob('*')
    if p.is_file() and '.git' not in p.parts and '__pycache__' not in p.parts and p.suffix != '.pyc' and p.name != 'validate_release_candidate.py'
), 'legacy state directory remains'
assert '/var/lib/onepane' in config
assert '/var/lib/onepane' in config_go
for token in [
    'gopkg.in/yaml.v3 v3.0.1 h1:',
    'gopkg.in/yaml.v3 v3.0.1/go.mod h1:',
    'modernc.org/sqlite v1.36.3 h1:',
    'modernc.org/sqlite v1.36.3/go.mod h1:',
]:
    assert token in go_sum, token
for token in ['go mod download', 'go mod verify', 'CGO_ENABLED=0', 'onepane-linux-${arch}', 'amd64 arm64']:
    assert token in build, token
print('OnePane release-candidate identity + reproducibility invariants: PASS')
