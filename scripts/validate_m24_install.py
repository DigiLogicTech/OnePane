from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
install=(ROOT/'scripts/install-ubuntu.sh').read_text()
upgrade=(ROOT/'scripts/upgrade-ubuntu.sh').read_text()
unit=(ROOT/'packaging/systemd/onepane.service').read_text()
checks={
 'checksum verification':'sha256sum -c' in install and 'sha256sum -c' in upgrade,
 'dedicated user':'User=onepane' in unit and 'useradd --system' in install,
 'rootless podman':'podman uidmap' in install and 'subuid' in install and 'subgid' in install,
 'loopback default':'127.0.0.1:8080' in install and '127.0.0.1:8081' in install,
 'rootless mapping boundary':'CapabilityBoundingSet=CAP_SETUID CAP_SETGID' in unit and 'User=onepane' in unit and 'NoNewPrivileges=no' in unit,
 'rootless runtime check':"podman info --format '{{.Host.Security.Rootless}}'" in install,
 'upgrade backup':'pre-upgrade-' in upgrade and 'harness.db' in upgrade,
 'health gate':'/v1/health' in install and '/v1/health' in upgrade,
}
failed=[k for k,v in checks.items() if not v]
if failed: raise SystemExit('M24 FAIL: '+', '.join(failed))
print('M24 Ubuntu installer/upgrader contract: PASS')
