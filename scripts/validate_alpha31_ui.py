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
ck("Primary navigation survives sidebar rerenders", "nav.dataset.delegatedNav" in app and 'e.target.closest("[data-route]")' in app)
ck("Nested navigation has a single active leaf", "parentActive=route===r&&!hasLeaf" in app and "projectActive&&!qa4ProjectHub.activeWorkspaceID" in app)
ck("Persistent Control Chat shell exists", 'id="controlChatLauncher"' in html and 'id="controlChatPanel"' in html and "a31RenderControlChat" in app)
ck("Assistant and Project Orchestrator share persistent panel", 'id="controlChatAssistantTab"' in html and 'id="controlChatOrchestratorTab"' in html and "/orchestrator/turns" in app)
ck("OnePane Chat can persistently collapse", 'id="controlChatToggle"' in html and "a31SetControlChatCollapsed" in app and 'data-collapsed="true"' in css)

ck("Shared grid layout stores x y width height", "function a31NormalizeLayout" in app and "item.width" in app and "item.height" in app and "item.x" in app and "item.y" in app)
ck("Desktop components have real pointer resize handles", "function a31BindLayout" in app and 'data-op-resize' in app and 'data-pw-resize' in app and ".layout-resize-handle" in css)
ck("Operations add path updates grid without page rerender", "openOperationsComponentPicker=function" in app and "closeModal();a31RenderOperationsGrid()" in app)
ck("Project component mutations refresh only project grid", "a31RefreshProjectGrid(project,workspace" in app and "qa4SaveProjectWorkspaces" in app)

ck("Operations Activity Health Recovery are functional", '["activity","Activity"]' in app and '["health","Health"]' in app and '["recovery","Recovery"]' in app and "a31OperationsActivity" in app and "a31OperationsHealth" in app and "a31RecoveryContent" in app)
ck("Operations Logs is a true drawer toggle", "function a31ToggleLogs" in app and 'activeDrawerTab==="logs"' in app and "setDrawerOpen(false)" in app)
ck("Operations default layout is meaningful and collision-aware", "x:0,y:0,width:12,height:3" in app and "a31ResolveLayout(state.operationsWidgets,item.id)" in app)
ck("Recovery header preserves action spacing", "recovery-card-header" in app and ".recovery-card-header" in css)

ck("Local Models distinguish trusted installability", "installable_quantizations" not in app or "model.installable" in app)
ck("Download action is Download & Install", app.count("Download & Install")>=2)
ck("Local Models expose prominent Detect Hardware", "a31-detect-large" in app and "⚙ Detect Hardware" in app)
ck("Per-deployment compute placement is exposed", "a31OpenCompute" in app and "Require GPU" in app and "Require CPU" in app and "Hybrid / CPU + GPU" in app)
ck("Managed local runtime explains zero manual dependency", "No separate llama.cpp installation is required" in app)
ck("Models component telemetry helper exists and is isolated", "async function qa5ComponentStatus()" in app and "/v1/local-ai/components?workspace_id=" in app and "qa5ModelComponents().catch(()=>({}))" in app)
ck("Colibri and OmniRoute managed lifecycle are present", 'a31ComponentButtons("colibri"' in app and 'a31ComponentButtons("omniroute"' in app)
ck("External OmniRoute provider lifecycle remains", "omniQA(false)" in app and "omniQA(true)" in app)

ck("Cloud Models expose OAuth when configured", "a31StartOAuth" in app and "/v1/provider-oauth/" in app and "OAuth not configured" in app)
ck("Agents expose Research Team configuration", "research_mode" in app and "independent_first_pass" in app and "full_provenance" in app)
ck("Agents expose DigiLogic Core profile metadata", "DigiLogic Core" in app and "Duplicate & customise" in app)
ck("Skills surface supports upload install assignments packages", "a31UploadSkill" in app and 'data-a31-skills-tab="assignments"' in app and 'data-a31-skills-tab="packages"' in app)
ck("Skills expose governed capability matrix", 'data-a31-skills-tab="matrix"' in app and "Effective Capability Matrix" in app and "Known model deployments" in app)
ck("Skills expose first-class Tool Bundles view", 'data-a31-skills-tab="bundles"' in app and "Role-scoped tool groupings" in app and "No Tool Bundles are registered." in app)

ck("Settings have canonical information architecture", all(x in app for x in ["General","Appearance","Defaults","Models & Compute","Providers & Auth","Nodes & Federation","Agents & Research","Skills & Tools","Security & Approvals","Updates & Diagnostics"]))
ck("Settings preserve new Workspace default semantics", "Existing Workspaces are never changed here" in app)
ck("Settings content is centered in remaining canvas", "#a31SettingsContent" in css and "justify-self:center" in css and "width:min(100%,1100px)" in css)
ck("Tour uses four-pane focus and stable card anchors", "tour-pane-top" in app and "bottom-center" in app and ".a31-tour-card" in css and "backdrop-filter:blur(2px)" in css)
ck("Tour target remains crisp and outlined", ".tour-spotlight" in css and ".tour-target" in css and "filter:none!important" in css)
ck("Tour uses the canonical overlay root", 'const root=$("#overlayRoot")' in app and "qa31TourRoot" not in app and "qa31TourRoot" not in html)
ck("Tour cleanup removes live highlights and restores shell state", 'document.querySelectorAll(".tour-target")' in app and 'removeEventListener("keydown",onKeyDown)' in app and 'setInspectorOpen(originalInspector==="open")' in app and 'setDrawerOpen(originalDrawer==="open")' in app and 'e.key==="Escape"' in app)

ck("Shell collapse controls use orientation-aware geometry", ".drawer-edge-toggle,.drawer-restore" in css and "width:42px!important" in css and "height:26px!important" in css and ".inspector-restore" in css and "width:26px!important" in css and "height:54px!important" in css and ".panel-toggle-icon" in css and "stroke:currentColor" in css)
ck("Collapsed sidebar hides brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none}' in css)
ck("Models headers grow with wrapped copy", ".models-page .card-header.models-card-header" in css and "height:auto" in css)

ck("Command palette exposes real actions", "function a31CommandRegistry()" in app and "Detect local hardware" in app and "New scheduled task" in app and "Restart product tour" in app)
ck("Health popover is live and never hard-codes inventory counts", "async function openHealthPopover" in app and "Not reported" in app and "5 / 8 active" not in app and "8 / 8" not in app)
ck("Operational telemetry distinguishes zero from unreported feeds", "Promise.allSettled" in app and "liveOps.reported" in app and "liveOpsAttentionReported()" in app and "Attention status not fully reported" in app and "Node feed unavailable" in app and "Provider feed unavailable" in app)
ck("Drawer telemetry distinguishes unreported feeds", "Event feed not reported." in app and "liveOpsAttentionReported()?String(ATTENTION_ITEMS.length):'Not reported'" in app and "Control plane ${escapeHtml(controlState)}" in app)
ck("Successful management fetches promote telemetry report state", "liveOps.reported.tasks=true;liveOps.reported.routines=true" in app and "liveOps.reported.nodes=true;syncLiveNotifications()" in app and "liveOps.providers=providerRows;liveOps.reported.providers=true" in app)
attention_start=app.find("async function openAttentionPopover")
attention_end=app.find("async function openHealthPopover", attention_start)
attention_popover=app[attention_start:attention_end] if attention_start >= 0 and attention_end > attention_start else ""
ck("Attention popover uses live operational data without fabricated incidents", "refreshOperationalDataQA(true)" in attention_popover and "ATTENTION_ITEMS" in attention_popover and "No attention items reported." in attention_popover and all(x not in attention_popover for x in ["T-1833","AI-Lab-02","Nightly Research"]))
ck("First-run setup requires an explicit language choice", 'id="setup-language-step"' in html and 'id="setup-language"' in html and "qa5PrepareFirstRunLanguage" in app and "QA5_SETUP_LANGUAGE_KEY" in app)
ck("Language remains changeable from Settings", 'id="a31Language"' in app and "qa5ApplyAuthLanguage(e.target.value)" in app)
ck("Theme-aware sleek scrollbars use shared tokens", all(x in css for x in ["--scrollbar-thumb","--scrollbar-thumb-hover","--scrollbar-thumb-active","::-webkit-scrollbar-thumb","scrollbar-gutter:stable"]))
ck("Theme accent contrast is tokenized", "--on-accent" in css and "color:var(--on-accent)" in css and 'data-theme="graphite"' in css)
ck("Dark and Midnight are deliberately distinct", 'data-theme="dark"' in css and 'data-theme="midnight"' in css and "--app-gradient:linear-gradient" in css)
ck("Workspace layout resolves collisions only at commit", "function a31ApplyItemLayout" in app and "requestAnimationFrame(render)" in app and "a31ResolveLayout(items,item.id)" in app and "const snapshot=items.map" in app and "Layout save failed:" in app)
ck("Desktop layout exposes edge and corner resize handles", "A31_RESIZE_EDGES" in app and "data-resize-edge" in app and all(x in css for x in [".resize-n",".resize-e",".resize-se",".resize-nw"]))

ck("Research Integrity is visible from task Inspector", "A32_RESEARCH_INSPECTOR" in app and "/v1/tasks/" in app and "/team-session" in app and "/manifest" in app and "Research Integrity" in app and "Bound candidate" in app)
ck("Research Council exposes automatic critique rounds", "a32CritiqueRounds" in app and "Automatic Research flow" in app and "critique_rounds" in app)
ck("Paused Research rounds expose manual retry", "Research round paused" in app and "Retry this round" in app and "/research/retry" in app and "Automatic retry eligible" in app)
ck("Team configuration loads members after resolving Team ID", "const teamID=team.id||team.ID" in app and app.index("const teamID=team.id||team.ID") < app.index("members=await apiRequest(`/v1/teams/${encodeURIComponent(teamID)}/members`)"))
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nALPHA 3.1 UI: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(" - "+n,file=sys.stderr)
    sys.exit(1)
print(f"\nALPHA 3.1 UI: ALL {len(checks)} CHECKS PASSED")
