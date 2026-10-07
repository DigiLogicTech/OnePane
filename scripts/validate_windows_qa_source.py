#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
ui=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js")+"\n"+read("internal/webui/static/models-page.js"); html=read("internal/webui/static/index.html"); css=read("internal/webui/static/style.css")
api=read("internal/api/server.go"); omni=read("internal/provideronboarding/omniroute.go"); webui=read("internal/webui/webui.go")
desktop=read("packaging/windows/desktop/main.go"); setup=read("packaging/windows/setup/main.go"); verify=read("packaging/windows/Verify-OnePane.cmd")
checks=[]
def ck(n,c): checks.append((n,bool(c)))
ck("tour uses crisp spotlight and stable card", "tour-spotlight" in ui and "tour-pane-top" in ui and ".a31-tour-card" in css)
ck("Windows uses Ctrl+K", "Ctrl+K" in ui and "⌘K" not in ui and "⌘K" not in html)
ck("notifications remain live", "attentionFromLiveData" in ui and "refreshOperationalDataQA" in ui)
ck("operations has deterministic 12-column grid", "operations-layout-grid" in css and "repeat(12" in css and "a31NormalizeLayout" in ui)
ck("desktop drag and pointer resize enabled", "a31BindLayout" in ui and "data-op-resize" in ui and "data-pw-resize" in ui and "layout-resize-handle" in css)
ck("collapsed drawer and inspector restore", "drawerRestore" in html and "inspectorRestore" in html and "syncPanelRestoreButtons" in ui)
ck("new task flow remains API backed", 'id="newTaskButton"' in ui and "apiRequest('/v1/tasks'" in ui)
ck("project flow remains API backed", 'id="configureProject"' in ui and "apiRequest('/v1/projects'" in ui)
ck("node pairing APIs remain available", "/pair/confirm" in ui and "GET /v1/nodes" in api)
ck("provider list API exposed", "GET /v1/providers" in api and "func (s *Server) listProviders" in api)
ck("OmniRoute external URL policy remains safe", 'u.Scheme != "https"' in omni and 'loopback && u.Scheme == "http"' in omni)
ck("trusted model catalogue is rendered", "/v1/local-ai/catalog" in ui and "Download & Install" in ui and "model.installable" in ui)
ck("model pool remains GUI-configurable", "/v1/settings/local-ai" in ui and "model_pool_path" in setup)
ck("desktop uses authoritative backend WebUI", "NewSingleHostReverseProxy" in desktop and "127.0.0.1:18181" in desktop)
ck("root WebUI is canonical and proxied", "index.html" in webui and 'mux.Handle("/", proxy)' in desktop and "fs.ReadFile(staticFS" not in desktop)
ck("desktop reserves native settings routes", "/desktop/settings" in desktop and "/desktop/settings/model-pool" in desktop)
ck("app icon and verifier remain integrated", "OnePane.ico" in desktop and "OnePane.ico" in setup and "Verify-OnePane.ps1" in verify)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
 print(f"\nWINDOWS QA SOURCE: {len(failed)} CHECK(S) FAILED",file=sys.stderr); sys.exit(1)
print(f"\nWINDOWS QA SOURCE: ALL {len(checks)} CHECKS PASSED")
