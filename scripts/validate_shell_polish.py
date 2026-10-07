#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
app=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js")+"\n"+read("internal/webui/static/models-page.js"); html=read("internal/webui/static/index.html"); css=read("internal/webui/static/style.css")
checks=[]
def ck(n,c): checks.append((n,bool(c)))
ck("Inspector defaults to 360px", "inspectorWidth:360" in app and "--inspector-open:360px" in css)
ck("Inspector resize remains persistent and bounded", "function bindInspectorResize()" in app and "Math.max(280" in app and "Math.min(720" in app)
ck("shared collapse controls remain", all(x in html for x in ['id="sidebarToggle"','id="drawerToggle"','id="drawerRestore"','id="inspectorRestore"']) and "width:42px!important" in css and "height:26px!important" in css)
ck("Logs toggle releases drawer", "function a31ToggleLogs" in app and "setDrawerOpen(false)" in app)
ck("panel transitions are subtle", "180ms cubic-bezier(.2,.7,.2,1)" in css and "opacity 130ms ease" in css)
ck("reduced motion respected", "prefers-reduced-motion:reduce" in css or "prefers-reduced-motion: reduce" in css)
ck("light theme uses restrained blue", "--good:#2d78a8" in css)
ck("Projects expose nested Workspace navigation", "project-nav-tree" in app and "data-a31-workspace-nav" in app and ".project-nav-workspace.active" in css)
ck("Models expose Local Cloud nested navigation", "model-nav-tree" in app and 'data-a31-model-view="local"' in app and 'data-a31-model-view="cloud"' in app)
ck("Detect Hardware is prominent", "a31-detect-large" in app and ".a31-detect-large" in css)
ck("Control Chat is persistent left-side surface", 'id="controlChatLauncher"' in html and 'id="controlChatPanel"' in html and ".control-chat-panel" in css)
ck("Tour uses proven spotlight focus with centred collision-aware placement", 'overlay.dataset.focus="target"' in app and "const center=clamp" in app and "if(collides(pos)&&targetRect)" in app and 'card.dataset.positioned="true"' in app and "9999px" in css and ".a31-tour-card" in css)
ck("Tour target remains crisp", ".tour-target" in css and "filter:none!important" in css and ".tour-spotlight" in css)
ck("collapsed sidebar hides brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none}' in css)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
 print(f"\nSHELL POLISH: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
 for n in failed: print(" - "+n,file=sys.stderr)
 sys.exit(1)
print(f"\nSHELL POLISH: ALL {len(checks)} CHECKS PASSED")
