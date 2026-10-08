#!/usr/bin/env python3
from pathlib import Path
import re
import sys

ROOT=Path(__file__).resolve().parents[1]
workflow=(ROOT/".github/workflows/alpha3.1-stabilization.yml").read_text(encoding="utf-8")
required={
    "candidate CI is immutable by SHA": "group: alpha32-source-native-${{ github.sha }}" in workflow and "cancel-in-progress: false" in workflow,
    "frontend architecture gate is mandatory": "python3 scripts/validate_frontend_architecture.py" in workflow,
    "release-candidate identity gate is mandatory": "python3 scripts/validate_release_candidate.py" in workflow,
    "installed browser behavioural gate is mandatory": "ui-e2e.txt" in workflow and "ONEPANE_UI_E2E" in workflow and "release-smoke.js" in workflow,
    "Windows installed-product gate remains mandatory": "Windows installed-product smoke (release gate)" in workflow and "Install launch uninstall reinstall" in workflow,
    "Ubuntu installed-package gate remains mandatory": "Ubuntu installed-package smoke (release gate)" in workflow and "Install verify and preserve data" in workflow,
    "candidate artifact is SHA-addressed": bool(re.search(r"OnePane-alpha3[.\w-]+-source-native-\$\{\{\s*github\.sha\s*\}\}", workflow)),
    "candidate artifact carries provenance manifest": "candidate-manifest.json" in workflow and '"commit": os.environ["GITHUB_SHA"]' in workflow,
    "source validation must leave checkout clean": "git diff --exit-code" in workflow,
}
failed=[name for name,ok in required.items() if not ok]
for name,ok in required.items():
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
if failed:
    print(f"\nRELEASE PROCESS: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    sys.exit(1)
print("\nRELEASE PROCESS: ALL CHECKS PASSED")
