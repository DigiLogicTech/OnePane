#!/usr/bin/env python3
from pathlib import Path
setup=Path("packaging/windows/setup/main.go").read_text()
ubuntu=Path("scripts/build-ubuntu-deb.sh").read_text()
checks={
 "windows uninstall exists":"func uninstallProduct() error" in setup,
 "kills desktop":"OnePane.Desktop.exe" in setup and 'taskkill.exe' in setup,
 "kills backend":"OnePane.Backend.exe" in setup,
 "kills service process":"OnePane.Service.exe" in setup,
 "deletes service":'"delete", serviceName' in setup,
 "removes startup registration":r'HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' in setup,
 "removes uninstall registration":r'HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\OnePane' in setup,
 "detached post-exit cleanup":"Wait-Process -Id" in setup and "Remove-Item -LiteralPath" in setup,
 "preserves durable data":"Never recursively remove ProgramData" in setup,
 "ubuntu package builder present":"dpkg-deb" in ubuntu,
}
bad=[k for k,v in checks.items() if not v]
for k,v in checks.items(): print(f"[{'PASS' if v else 'FAIL'}] {k}")
if bad: raise SystemExit("Lifecycle contract failed: "+", ".join(bad))
print(f"Alpha 3.1 package lifecycle contract: {len(checks)}/{len(checks)} passed")
