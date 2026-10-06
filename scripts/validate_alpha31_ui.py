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

def single_selector_collection_calls(src):
    out=[]; i=0; methods=("forEach","map","filter","some","every","find","reduce")
    while i < len(src)-1:
        if src[i]=="$" and src[i+1]=="(" and (i==0 or src[i-1]!="$"):
            j=i+2; depth=1; quote=None; esc=False
            while j < len(src) and depth:
                ch=src[j]
                if quote is not None:
                    if esc: esc=False
                    elif ch=="\\": esc=True
                    elif ch==quote: quote=None
                else:
                    if ch in ("'", '"', "`"): quote=ch
                    elif ch=="(": depth+=1
                    elif ch==")": depth-=1
                j+=1
            if depth==0:
                tail=src[j:]
                for method in methods:
                    if tail.startswith("."+method+"("):
                        out.append((src.count("\n",0,i)+1,method)); break
        i+=1
    return out

ck("No runtime RC patch layer ships", "/rc3.js" not in html and "ONEPANE_APPLY_RC3" not in app and not (ROOT/"internal/webui/static/rc3.js").exists())
ck("Canonical and Windows WebUI match", app==wapp and html==whtml and css==wcss)
ck("Exactly one canonical Alpha 3.1 product layer", app.count("/* === Alpha 3.1 canonical product layer === */")==1 and "/* === Alpha 3 workspace/model/runtime consolidation === */" not in app)
ck("Route rendering keeps epoch invalidation", "let qa31ViewEpoch=0;" in app and "const epoch=++qa31ViewEpoch" in app and "epoch!==qa31ViewEpoch" in app)
ck("Single-element selector helper is never used as a collection", not single_selector_collection_calls(app))

ck("Primary navigation exposes Nodes and Skills", '["nodes","⬡","Nodes"]' in app and '["skills","✦","Skills"]' in app)
ck("Projects and Models have nested navigation", "project-nav-tree" in app and "model-nav-tree" in app and 'data-a31-model-view="local"' in app and 'data-a31-model-view="cloud"' in app)
ck("Persistent Control Chat shell exists", 'id="controlChatLauncher"' in html and 'id="controlChatPanel"' in html and "a31RenderControlChat" in app)
ck("Assistant and Project Orchestrator share persistent panel", 'id="controlChatAssistantTab"' in html and 'id="controlChatOrchestratorTab"' in html and "/orchestrator/turns" in app)

ck("Shared grid layout stores x y width height", "function a31NormalizeLayout" in app and "item.width" in app and "item.height" in app and "item.x" in app and "item.y" in app)
ck("Desktop components have real pointer resize handles", "function a31BindLayout" in app and 'data-op-resize' in app and 'data-pw-resize' in app and ".layout-resize-handle" in css)
ck("Operations add path updates grid without page rerender", "openOperationsComponentPicker=function" in app and "closeModal();a31RenderOperationsGrid()" in app)
ck("Project component mutations refresh only project grid", "a31RefreshProjectGrid(project,workspace" in app and "qa4SaveProjectWorkspaces" in app)

ck("Operations Activity Health Recovery are functional", '["activity","Activity"]' in app and '["health","Health"]' in app and '["recovery","Recovery"]' in app and "a31OperationsActivity" in app and "a31OperationsHealth" in app and "a31RecoveryContent" in app)
ck("Operations Logs is a true drawer toggle", "function a31ToggleLogs" in app and 'activeDrawerTab==="logs"' in app and "setDrawerOpen(false)" in app)

ck("Local Models distinguish trusted installability", "installable_quantizations" not in app or "model.installable" in app)
ck("Download action is Download & Install", app.count("Download & Install")>=2)
ck("Local Models expose prominent Detect Hardware", "a31-detect-large" in app and "⚙ Detect Hardware" in app)
ck("Per-deployment compute placement is exposed", "a31OpenCompute" in app and "Require GPU" in app and "Require CPU" in app and "Hybrid / CPU + GPU" in app)
ck("Managed local runtime explains zero manual dependency", "No separate llama.cpp installation is required" in app)
ck("Colibri and OmniRoute managed lifecycle are present", 'a31ComponentButtons("colibri"' in app and 'a31ComponentButtons("omniroute"' in app)
ck("External OmniRoute provider lifecycle remains", "omniQA(false)" in app and "omniQA(true)" in app)

ck("Cloud Models expose OAuth when configured", "a31StartOAuth" in app and "/v1/provider-oauth/" in app and "OAuth not configured" in app)
ck("Agents expose Research Team configuration", "research_mode" in app and "independent_first_pass" in app and "full_provenance" in app)
ck("Agents expose DigiLogic Core profile metadata", "DigiLogic Core" in app and "Duplicate & customise" in app)
ck("Skills surface supports upload install assignments packages", "a31UploadSkill" in app and 'data-a31-skills-tab="assignments"' in app and 'data-a31-skills-tab="packages"' in app)

ck("Settings have canonical information architecture", all(x in app for x in ["General","Appearance","Defaults","Models & Compute","Providers & Auth","Nodes & Federation","Agents & Research","Skills & Tools","Security & Approvals","Updates & Diagnostics"]))
ck("Settings preserve new Workspace default semantics", "Existing Workspaces are never changed here" in app)
ck("Tour uses four-pane focus and stable card anchors", "tour-pane-top" in app and "bottom-center" in app and ".a31-tour-card" in css and "backdrop-filter:blur(2px)" in css)
ck("Tour target remains crisp and outlined", ".tour-spotlight" in css and ".tour-target" in css and "filter:none!important" in css)

ck("All shell collapse controls use 42x26 geometry", "width:42px!important" in css and "height:26px!important" in css)
ck("Collapsed sidebar hides brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none}' in css)
ck("Models headers grow with wrapped copy", ".models-page .card-header.models-card-header" in css and "height:auto" in css)

failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nALPHA 3.1 UI: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(" - "+n,file=sys.stderr)
    sys.exit(1)
print(f"\nALPHA 3.1 UI: ALL {len(checks)} CHECKS PASSED")
