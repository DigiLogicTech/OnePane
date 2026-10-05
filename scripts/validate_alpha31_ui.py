#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
app=read("internal/webui/static/app.js")
html=read("internal/webui/static/index.html")
css=read("internal/webui/static/style.css")
wapp=read("packaging/windows/desktop/static/app.js")
whtml=read("packaging/windows/desktop/static/index.html")
wcss=read("packaging/windows/desktop/static/style.css")

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

ck("No runtime RC patch layer ships", "/rc3.js" not in html and "ONEPANE_APPLY_RC3" not in app and not (ROOT/"internal/webui/static/rc3.js").exists())
ck("Canonical and Windows WebUI match", app==wapp and html==whtml and css==wcss)
ck("Route rendering uses one canonical epoch guard", "let qa31ViewEpoch=0;" in app and "const epoch=++qa31ViewEpoch;" in app and "qa31ProjectRenderEpoch!==qa31ViewEpoch" in app)
ck("Models expose Local and Cloud nested pages", 'data-model-nav-view="local"' in app and 'data-model-nav-view="cloud"' in app and "onepane:models-view" in app)
ck("Local Models owns Colibri lifecycle", "qa31ColibriActionButtons(colibri)" in app and 'qa5ComponentAction("colibri"' in app)
ck("OmniRoute never uses managed component install/remove", 'qa5ComponentAction("omniroute"' not in app)
ck("OmniRoute uses provider lifecycle", '"/v1/providers/"+encodeURIComponent(id)+"/revoke"' in app and 'omniQA(false)' in app and 'omniQA(true)' in app)
ck("Cloud API keys action is right aligned", 'class="header-actions"><button class="btn" id="qa4OpenSecrets">API keys</button>' in app)
ck("Assistant command surface has canonical panel styling", ".command-palette{" in css)
ck("Tour waits for routed targets", "function qa31WaitForTourTarget(step" in app and "currentTarget=await qa31WaitForTourTarget(step)" in app)
ck("Tour removes blur and leaves target crisp", "#qa31TourRoot .tour-mask{" in css and "backdrop-filter:none" in css and "pointer-events:none!important;" in css)
ck("All shell controls use the same 42x26 geometry", "width:42px!important;" in css and "height:26px!important;" in css)
ck("Collapsed sidebar hides brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none;}' in css)
ck("Models headers grow with wrapped copy", ".models-page .card-header.models-card-header{" in css and "height:auto;" in css)
ck("Project Orchestrator remains authoritative", "function qa31ProjectOrchestratorStrip" in app and "qa31ProjectOrchestrator" in app)

ck("Tour model-page switcher is defined", "function qa31SetModelView(view)" in app and "qa31SetModelView('local')" in app and "qa31SetModelView('cloud')" in app)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nALPHA 3.1 UI: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(" - "+n,file=sys.stderr)
    sys.exit(1)
print(f"\nALPHA 3.1 UI: ALL {len(checks)} CHECKS PASSED")
