#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
app=read("internal/webui/static/app.js")
html=read("internal/webui/static/index.html")
css=read("internal/webui/static/style.css")
rc3=read("internal/webui/static/rc3.js")
wapp=read("packaging/windows/desktop/static/app.js")
whtml=read("packaging/windows/desktop/static/index.html")
wcss=read("packaging/windows/desktop/static/style.css")
wrc3=read("packaging/windows/desktop/static/rc3.js")

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

ck("RC3 loads before app.js", html.index('/rc3.js') < html.index('/app.js'))
ck("RC3 hook applies before boot", "window.ONEPANE_APPLY_RC3?.();" in app and app.index("window.ONEPANE_APPLY_RC3?.();") < app.rindex("bootOnePane();"))
ck("Canonical and Windows RC3 files match", app==wapp and html==whtml and css==wcss and rc3==wrc3)
ck("Route rendering uses an epoch guard", "let viewEpoch=0;" in rc3 and "const epoch=++viewEpoch;" in rc3 and "epoch!==viewEpoch" in rc3)
ck("Models expose Local and Cloud nested pages", 'data-model-nav-view="local"' in rc3 and 'data-model-nav-view="cloud"' in rc3 and "onepane:models-view" in rc3)
ck("Local Models owns Colibri lifecycle", "qa31ColibriActionButtons(colibri)" in rc3 and 'qa5ComponentAction("colibri"' in rc3)
ck("OmniRoute no longer calls managed component install/remove", 'qa5ComponentAction("omniroute"' not in rc3 and 'data-qa31-colibri-action' in rc3)
ck("OmniRoute uses provider lifecycle", '"/v1/providers/"+encodeURIComponent(id)+"/revoke"' in rc3 and 'omniQA(false)' in rc3 and 'omniQA(true)' in rc3)
ck("Cloud API keys action is in a right header action area", 'class="header-actions"><button class="btn" id="qa4OpenSecrets">API keys</button>' in rc3)
ck("Assistant surface has a visible command-palette base style", ".command-palette{" in css and "background:var(--panel);" in css[css.rfind(".command-palette{"):])
ck("Tour blur is disabled", "#qa31TourRoot .tour-mask{" in css and "backdrop-filter:none!important;" in css[css.rfind("#qa31TourRoot .tour-mask{"):])
ck("Tour targets remain crisp and non-intercepting", ".tour-target{" in css and "pointer-events:none!important;" in css[css.rfind(".tour-target{"):])
ck("All shell collapse controls use the same 42x26 geometry", ".sidebar-panel-toggle," in css and "width:42px!important;" in css and "height:26px!important;" in css)
ck("Collapsed sidebar hides the OnePane brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none;}' in css)
ck("Models card headers can grow instead of cutting through wrapped text", ".models-page .card-header.models-card-header{" in css and "height:auto;" in css)
ck("Project Orchestrator remains present", "function qa31ProjectOrchestratorStrip" in app and "qa31ProjectOrchestrator" in app)

failed=[name for name,ok in checks if not ok]
for name,ok in checks:
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
if failed:
    print(f"\nRC3 UI: {len(failed)} CHECK(S) FAILED", file=sys.stderr)
    for name in failed: print(" - "+name,file=sys.stderr)
    sys.exit(1)
print(f"\nRC3 UI: ALL {len(checks)} CHECKS PASSED")
