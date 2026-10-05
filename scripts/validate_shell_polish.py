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
ck('All desktop panel controls use the shared control system',
   'id="sidebarToggle"' in html and 'shell-panel-toggle sidebar-panel-toggle' in html
   and 'shell-panel-toggle drawer-edge-toggle' in html
   and 'shell-panel-toggle panel-restore drawer-restore' in html
   and 'shell-panel-toggle panel-restore inspector-restore' in html
   and '.shell-panel-toggle{' in css and '.panel-toggle-icon{' in css)
ck('Panel controls use SVG rather than text arrows',
   html.count('class="panel-toggle-icon"') >= 4
   and "function qa31ChevronIcon(direction)" in app
   and "inspectorToggle.textContent" not in app
   and '&lt;|' not in html and '|>' not in html)
ck('Panel control directions describe the next action',
   "state.sidebar==='expanded'?'left':'right'" in app
   and "state.inspector==='open'?'right':'left'" in app
   and "qa31SetPanelToggle(drawerToggle,'down'" in app
   and "qa31SetPanelToggle(drawerRestore,'up'" in app)
ck('Logs close releases parent drawer height', "document.documentElement.style.setProperty('--drawer',open?" in app and ":'0px')" in app)
ck('Panel transitions are subtle', '180ms cubic-bezier(.2,.7,.2,1)' in css and 'opacity 130ms ease' in css)
ck('Reduced motion is respected', '@media (prefers-reduced-motion: reduce)' in css)
ck('Light theme uses restrained blue success/status colour', ':root[data-theme="light"] { --good:#2d78a8; }' in css)
ck('Projects expose nested workspace navigation',
   'function qa31ProjectNavTreeHTML()' in app
   and 'data-project-nav-workspace' in app
   and '.project-nav-tree{' in css
   and '.project-nav-workspace.active' in css)
ck('Model hardware controls have responsive compact layout',
   'qa31-local-models-header' in app and 'qa31-detect-hardware' in app
   and '.qa31-local-models-header{' in css and '.qa31-detect-hardware{' in css)
ck('Agent loading cannot remain indefinite',
   'function qa31AgentRequest(path,timeoutMs=12000)' in app
   and 'Agent data did not respond within 12 seconds.' in app
   and 'qa31RetryAgents' in app)
ck('Modern tour completion accepts legacy and canonical values',
   "['1',TOUR_COMPLETE_VALUE].includes(localStorage.getItem(TOUR_KEY))" in app
   and "localStorage.setItem(TOUR_KEY,TOUR_COMPLETE_VALUE)" in app)
ck('Alpha 3.1 tour card is visible when its pointer-blocking overlay is active',
   '#qa31TourRoot{pointer-events:auto;}' in css
   and '#qa31TourRoot .tour-card{' in css
   and 'visibility:visible;' in css[css.find('#qa31TourRoot .tour-card{'):])
ck('Mobile menu calls Settings Settings',
   '<span>Settings</span>' in app
   and '<span>Defaults</span>' not in app[app.find('function openMobileMore'):app.find('function openCommandPalette', app.find('function openMobileMore'))])

failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nSHELL POLISH: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(' - '+n,file=sys.stderr)
    sys.exit(1)
print(f"\nSHELL POLISH: ALL {len(checks)} CHECKS PASSED")
