#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding='utf-8')
app=read('internal/webui/static/app.js')
html=read('internal/webui/static/index.html')
css=read('internal/webui/static/style.css')

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

ck('Inspector defaults to 360px', 'inspectorWidth:360' in app and '--inspector-open:360px' in css)
ck('Inspector resize is persistent and bounded', 'function bindInspectorResize()' in app and 'Math.max(280' in app and 'Math.min(720' in app and 'persist();' in app)
ck('Inspector divider double-click resets width', 'handle.ondblclick=()=>{state.inspectorWidth=360' in app)
ck('Inspector uses docked edge toggle', 'id="inspectorRestore"' in html and '&lt;|' in html and 'right:calc(var(--inspector) - 27px)' in css)
ck('Inspector restore is not the old floating pill', 'Inspector ‹' not in html and '.inspector-restore {\n  position:absolute;' in css)
ck('Logs close releases parent drawer height', "document.documentElement.style.setProperty('--drawer',open?\`\${state.drawerHeight}px\`:'0px')" in app)
ck('Logs use centered edge toggles', 'class="drawer-edge-toggle"' in html and '>⌃</button>' in html and 'left:50%' in css)
ck('Panel transitions are subtle', '180ms cubic-bezier(.2,.7,.2,1)' in css and 'opacity 130ms ease' in css)
ck('Reduced motion is respected', '@media (prefers-reduced-motion: reduce)' in css)
ck('Light theme uses restrained blue success/status colour', ':root[data-theme="light"] { --good:#2d78a8; }' in css)
ck('Mobile menu calls Settings Settings', '<span>Settings</span>' in app and '<span>Defaults</span>' not in app[app.find('function openMobileMore'):app.find('function openCommandPalette', app.find('function openMobileMore'))])

failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nSHELL POLISH: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(' - '+n,file=sys.stderr)
    sys.exit(1)
print(f"\nSHELL POLISH: ALL {len(checks)} CHECKS PASSED")
