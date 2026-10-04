#!/usr/bin/env python3
from pathlib import Path
import re

ui = Path("internal/webui/static/app.js").read_text(encoding="utf-8")

checks = {
    "single active renderInspector declaration": len(re.findall(r"(?m)^function renderInspector\s*\(", ui)) <= 1,
    "single active renderDrawer declaration": len(re.findall(r"(?m)^function renderDrawer\s*\(", ui)) <= 1,
    "single active bindViewActions declaration": len(re.findall(r"(?m)^function bindViewActions\s*\(", ui)) <= 1,
    "popover cleanup state exists": "activePopoverCleanup" in ui,
    "modal opening cleans popover": bool(re.search(r"function openModal\s*\([^)]*\)\s*\{\s*closePopover\(\)", ui)),
    "modal closing cleans popover": bool(re.search(r"function closeModal\s*\([^)]*\)\s*\{\s*closePopover\(\)", ui)),
    "detached popover self-cleans": "!p.isConnected" in ui and "closePopover()" in ui,
    "drawer tabs route through shared open state": "data-drawer-tab" in ui and ("setDrawerOpen(true)" in ui or "state.drawer='open'" in ui),
}

failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
if failed:
    raise SystemExit("Alpha 3.1 shell regression contract failed: " + ", ".join(failed))
print(f"Alpha 3.1 shell regression contract: {len(checks)}/{len(checks)} passed")
