#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
app=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js")
smoke=read("internal/webui/static/release-smoke.js")
html=read("internal/webui/static/index.html")
css=read("internal/webui/static/style.css")

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

def last_segment(src, start_token, end_token):
    start=src.rfind(start_token)
    if start < 0:
        return ""
    end=src.find(end_token, start+len(start_token))
    return src[start:] if end < 0 else src[start:end]

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
ck("Windows desktop consumes the canonical WebUI through the backend proxy", not (ROOT/"packaging/windows/desktop/static").exists())
ck("Exactly one canonical Alpha 3.1 product layer", app.count("/* === Alpha 3.1 canonical product layer === */")==1 and "/* === Alpha 3 workspace/model/runtime consolidation === */" not in app)
ck("Route rendering keeps epoch invalidation", "let qa31ViewEpoch=0;" in app and "const epoch=++qa31ViewEpoch" in app and "epoch!==qa31ViewEpoch" in app)
ck("Single-element selector helper is never used as a collection", not single_selector_collection_calls(app))

ck("Primary navigation exposes Nodes and Skills", '["nodes","⬡","Nodes"]' in app and '["skills","✦","Skills"]' in app)
ck("Projects and Models have nested navigation", "project-nav-tree" in app and "model-nav-tree" in app and 'data-a31-model-view="local"' in app and 'data-a31-model-view="cloud"' in app)
ck("Primary navigation survives sidebar rerenders", 'nav.innerHTML=html' in app and '$("[data-route]",nav).forEach' in app and '$("[data-a34-nav-toggle]",nav).forEach' in app and '$("[data-a31-model-view]",nav).forEach' in app and '$("[data-a31-project-nav]",nav).forEach' in app)
ck("Nested navigation has a single active leaf", "parentActive=route===r&&!expandable" in app and "projectActive&&!qa4ProjectHub.activeWorkspaceID" in app and "projectActive&&w.id===qa4ProjectHub.activeWorkspaceID" in app)
ck("Persistent Control Chat shell exists", 'id="controlChatLauncher"' in html and 'id="controlChatPanel"' in html and "a31RenderControlChat" in app)
ck("Assistant and Project Orchestrator share persistent panel", 'id="controlChatAssistantTab"' in html and 'id="controlChatOrchestratorTab"' in html and "/orchestrator/turns" in app)
ck("OnePane Chat can persistently collapse", 'id="controlChatToggle"' in html and "a31SetControlChatCollapsed" in app and 'data-collapsed="true"' in css)

ck("Shared grid layout stores x y width height", "function a31NormalizeLayout" in app and "item.width" in app and "item.height" in app and "item.x" in app and "item.y" in app)
ck("Shared layout placement is CSP-safe", "function a31SetGridPlacement" in app and "gridColumnStart" in app and "gridColumnEnd" in app and "gridRowStart" in app and "gridRowEnd" in app and 'style="${a31GridStyle' not in app)
ck("Layout normalization rejects non-finite persisted geometry", "a31FiniteLayoutNumber" in app and "Number.isFinite(n)" in app and "a31FallbackLayoutSize" in app and "Math.round(rawW??fallback.width)" in app)
ck("Desktop components have real pointer resize handles", "function a31BindLayout" in app and 'data-op-resize' in app and 'data-pw-resize' in app and ".layout-resize-handle" in css)
ck("Operations add path updates grid without page rerender", "openOperationsComponentPicker=function" in app and "closeModal();a31RenderOperationsGrid()" in app)
ck("Project component mutations refresh only project grid", "a31RefreshProjectGrid(project,workspace" in app and "qa4SaveProjectWorkspaces" in app)
ck("Workspace deletion is confirmed revision-safe and guards the final workspace", "a32DeleteWorkspace" in app and "a32ConfirmDeleteWorkspace" in app and "A project must keep at least one workspace." in app and "workspace removed from durable project policy" in smoke)
ck("Project deletion is confirmed and lifecycle-safe", "a33DeleteProject" in app and 'method:"DELETE"' in app and "a33ConfirmDeleteProject" in app and "Project lifecycle delete persisted" in smoke)
ck("Nodes prefer federation machine name and mark the local host", "function a34NodeDisplayName" in app and 'n?.name||n?.hostname' in app and " (Local)" in app and "Nodes prefer machine name and mark local device" in smoke)

ck("Operations Activity Health Recovery are functional", '["activity","Activity"]' in app and '["health","Health"]' in app and '["recovery","Recovery"]' in app and "a31OperationsActivity" in app and "a31OperationsHealth" in app and "a31RecoveryContent" in app)
ck("Operations Logs is a true drawer toggle", "function a31ToggleLogs" in app and 'activeDrawerTab==="logs"' in app and "setDrawerOpen(false)" in app)
ck("Operations default layout is meaningful and collision-aware", "x:0,y:0,width:12,height:3" in app and "a31ResolveLayout(state.operationsWidgets,item.id)" in app and "a31RepairPersistedUIState()" in app and "a31OperationsLayoutBroken" in app)
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
ck("Tour uses spotlight focus with centred collision-aware placement", 'overlay.dataset.focus="target"' in app and "const center=clamp" in app and "if(collides(pos)&&targetRect)" in app and 'card.dataset.positioned="true"' in app and ".a31-tour-card" in css and "9999px" in css)
ck("Tour target remains crisp and outlined", ".tour-spotlight" in css and ".tour-target" in css and "filter:none!important" in css)
ck("Tour uses the canonical overlay root", 'const root=$("#overlayRoot")' in app and "qa31TourRoot" not in app and "qa31TourRoot" not in html)
ck("Tour cleanup removes live highlights and restores shell state", 'document.querySelectorAll(".tour-target")' in app and 'removeEventListener("keydown",onKeyDown)' in app and 'setInspectorOpen(originalInspector==="open")' in app and 'setDrawerOpen(originalDrawer==="open")' in app and 'e.key==="Escape"' in app)

ck("Shell collapse controls use orientation-aware geometry", ".drawer-edge-toggle" in css and "width:42px!important" in css and ".drawer-restore" in css and "width:54px!important" in css and ".inspector-restore" in css and "height:42px!important" in css and '.app-shell[data-inspector="closed"] .inspector-restore' in css and "height:54px!important" in css and ".panel-toggle-icon" in css and "stroke:currentColor" in css)
ck("Inspector and Logs edge controls are movable and persistent", "a32BindPanelToggleDrag" in app and "inspectorTogglePosition" in app and "drawerTogglePosition" in app and "Inspector toggle moves vertically" in smoke and "Logs toggle moves horizontally" in smoke)
ck("Logs action strip has no stray underline", ".drawer-actions{border-bottom:0!important}" in css)
ck("Collapsed sidebar hides brand icon", '.app-shell[data-sidebar="collapsed"] .brand-icon{display:none}' in css)
ck("Models headers grow with wrapped copy", ".models-page .card-header.models-card-header" in css and "height:auto" in css)

ck("Command palette exposes real actions", "function a31CommandRegistry()" in app and "Detect local hardware" in app and "New scheduled task" in app and "Restart product tour" in app)
ck("Health popover is live and never hard-codes inventory counts", "async function openHealthPopover" in app and "Not reported" in app and "5 / 8 active" not in app and "8 / 8" not in app)
ck("Operational telemetry distinguishes zero from unreported feeds", "Promise.allSettled" in app and "liveOps.reported" in app and "liveOpsAttentionReported()" in app and "Attention status not fully reported" in app and "Node feed unavailable" in app and "Provider feed unavailable" in app)
ck("Drawer telemetry distinguishes unreported feeds", "Event feed not reported." in app and "liveOpsAttentionReported()?String(ATTENTION_ITEMS.length):'Not reported'" in app and "Control plane ${escapeHtml(controlState)}" in app)
ck("Successful management fetches promote telemetry report state", "liveOps.reported.tasks=true;liveOps.reported.routines=true" in app and "liveOps.reported.nodes=true;syncLiveNotifications()" in app and "liveOps.providers=providerRows;liveOps.reported.providers=true" in app)
effective_cards=last_segment(app, "function qa4ScheduledCard()", "function bindViewActions")
effective_nodes=last_segment(app, "renderNodes=async function(){", "/* Agents, Teams, Research */")
effective_ops=last_segment(app, "function a31OperationsActivity()", "renderOperations=async function(){")
effective_project_layout=last_segment(app, "qa4BindWorkspaceEdit=function(project,workspace){", "const a31ProjectRenderBase=qa6RenderProjectsBase;")
effective_palette=last_segment(app, "function openCommandPalette(){", "/* QA hardening: Vault-backed provider selection")
project_saves=last_segment(app, "const qa4ProjectSaveQueues=new Map();", "async function qa4EnsureProjectRuntime")
ck("Effective Operations cards preserve unknown telemetry", all(x in effective_cards for x in ["liveOpsReported('routines')","liveOpsReported('tasks')","liveOpsReported('nodes')","liveOpsReported('events')","liveOpsAttentionReported()","liveOpsReported('providers')"]))
ck("Effective Operations Activity and Recovery preserve unknown telemetry", "Event feed not reported." in effective_ops and "componentsReported=false" in effective_ops and "Task recovery status not reported" in effective_ops)
ck("Final Nodes renderer consumes API envelope and launches pairing", "a31Array(out?.nodes)" in effective_nodes and '$("#a31AddNode").onclick=openPairNode' in effective_nodes)
ck("Project policy saves are serialized against latest revision", "qa4ProjectSaveQueues" in project_saves and "const current=qa4ProjectHub.projects.find" in project_saves and "expected_revision:Number(current.revision||1)" in project_saves)
ck("Project layout mutations rollback and refresh from saved revision", "refreshSaved" in effective_project_layout and "workspace.widgets.splice(0,workspace.widgets.length,...snapshot)" in effective_project_layout and "workspace.widgets=snapshot" in effective_project_layout and 'root.dataset.layoutSaving==="true"' in effective_project_layout)
ck("Operations telemetry refresh preserves the active layout DOM", "a31RefreshOperationsData" in app and "layoutRefreshPending" in app and "typeof a31RefreshOperationsData==='function'" in app)
ck("Operations geometry has explicit durable revisioning", "operationsLayoutRevision" in app and "a31PersistOperationsLayout" in app)
ck("Resize hit targets stay inside clipped dashboard cards", ".layout-resize-handle.resize-e{right:0}" in css and ".layout-resize-handle.resize-w{left:0}" in css and ".layout-resize-handle.resize-n{top:0}" in css and ".layout-resize-handle.resize-s{bottom:0}" in css)
ck("Layout generation participates in stale rollback protection", "root.dataset.layoutGeneration" in app and "String(generation)" in app and "root.dataset.layoutSaving==='true'" in app)
ck("Command palette cannot replace an active product tour", 'dataset.productTour==="active"' in effective_palette)

attention_start=app.find("async function openAttentionPopover")
attention_end=app.find("async function openHealthPopover", attention_start)
attention_popover=app[attention_start:attention_end] if attention_start >= 0 and attention_end > attention_start else ""
ck("Attention popover uses live operational data without fabricated incidents", "refreshOperationalDataQA(true)" in attention_popover and "ATTENTION_ITEMS" in attention_popover and "No attention items reported." in attention_popover and all(x not in attention_popover for x in ["T-1833","AI-Lab-02","Nightly Research"]))
ck("First-run setup requires an explicit language choice", 'id="setup-language-step"' in html and 'id="setup-language"' in html and "qa5PrepareFirstRunLanguage" in app and "QA5_SETUP_LANGUAGE_KEY" in app)
ck("Language remains changeable from Settings", 'id="a31Language"' in app and "qa5ApplyAuthLanguage(e.target.value)" in app)
ck("Theme-aware sleek scrollbars use shared tokens", all(x in css for x in ["--scrollbar-thumb","--scrollbar-thumb-hover","--scrollbar-thumb-active","::-webkit-scrollbar-thumb","scrollbar-gutter:stable"]))
ck("Theme accent contrast is tokenized", "--on-accent" in css and "color:var(--on-accent)" in css and 'data-theme="graphite"' in css)
ck("Dark and Midnight are deliberately distinct", 'data-theme="dark"' in css and 'data-theme="midnight"' in css and "--app-gradient:linear-gradient" in css)
ck("Workspace layout resolves collisions only at commit", "card.style.transform" in app and "previewRect" in app and "a31ResolveLayout(items,item.id)" in app and "const snapshot=items.map" in app and "Layout save failed:" in app)
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

ck("Inspector Overview is implicit rather than a permanent rail", 'root.dataset.tabMode=tabs.length>1?"multi":"single"' in app and 'inspector[data-tab-mode="single"]>.inspector-tabs' in css)

ck("Product tour cannot create an invisible click-blocking overlay", "visibility:visible!important" in css and "Product tour failed" in app and "Tour card is visible" in smoke)

ck("Operations render self-heals overlapping persisted geometry", "if(a31OperationsLayoutBroken(state.operationsWidgets))" in app and "Operations render repairs injected overlap" in smoke)

ck("Product tour is centred first and only repositions for focus collisions", 'const center=clamp' in app and 'if(collides(pos)&&targetRect)' in app and 'card.dataset.positioned="true"' in app)
ck("Product tour reveals only after positioning and defaults to centre", 'visibility:hidden!important' in css and 'data-positioned="true"' in css and "Tour defaults to page centre" in smoke)

ck("Product tour visibly isolates the active target", "9999px" in css and 'data-focus="target"' in css and "Tour target spotlight visible" in smoke and "proven surrounding focus shade" in smoke)

ck("Operations and Workspaces share smooth pixel drag preview", 'data-op-drag=' in app and 'data-pw-drag=' in app and 'translate3d(' in app and 'card.style.willChange="transform,width,height"' in app)
ck("Shared resize previews continuously then snaps once on release", "previewResize" in app and "a31ResizeRect(start,edge,dx,dy,constraints)" in app and "Object.assign(item,previewRect)" in app)
ck("Installed smoke performs real pointer drag and resize", 'const gesture=async' in smoke and "Operations pointer resize committed" in smoke and "Workspace pointer drag changes geometry" in smoke and "Workspace resized geometry survives project reload" in smoke)

ck("Tour spotlight owns top stacking layer", ".tour-overlay{z-index:2000!important" in css and ".tour-target{z-index:auto!important}" in css and "Tour spotlight owns top stacking layer" in smoke)
ck("OnePane Chat launcher toggles and panel moves vertically", "a33ToggleControlChatPanel" in app and "a33BindControlChatDrag" in app and "controlChatVerticalPosition" in app and "Chat moves vertically" in smoke and "Chat launcher closes open chat" in smoke)
ck("OnePane Chat chevron reflects collapse direction", ".control-chat-chevron{transform:rotate(180deg)}" in css and '.control-chat-panel[data-collapsed="true"] .control-chat-chevron{transform:none}' in css and "Chat chevron direction matches collapse state" in smoke)
ck("Inspector expanded and collapsed controls are explicitly distinct", '.app-shell[data-inspector="open"] #inspectorRestore' in css and '.app-shell[data-inspector="closed"] #inspectorRestore' in css)

ck("Expanded Inspector and Logs controls share one visual geometry", "Inspector and Logs expanded controls share rotated geometry" in smoke and "Inspector and Logs expanded controls share shape" in smoke and '.app-shell[data-inspector="open"] #inspectorRestore' in css)
ck("Collapsed sidebar preserves Chat and Command icons", "sidebar-action-icon" in html and "Collapsed sidebar keeps OnePane Chat icon" in smoke and "Collapsed sidebar keeps Command icon" in smoke)
ck("Projects Models and per-Project Workspaces are persistently collapsible", "a34NavTreeState" in app and "data-a34-nav-toggle" in app and "data-a34-project-toggle" in app and "Projects collapse persists" in smoke and "Workspace list collapses per Project" in smoke)
ck("Workspace settings native selects receive explicit theme color scheme", ':root[data-theme="dark"] select' in css and ':root[data-theme="light"] select' in css and ':root[data-theme="system"] select' in css and "Workspace settings selects use dark native color scheme" in smoke)
ck("Tour Navigation outline uses exact sidebar bounds", 'target:".sidebar",padding:0' in app and "Tour Navigation outline hugs sidebar bounds" in smoke)
ck("Node widgets use canonical hostname display", "function a34NodeDisplayName" in app and "Operations Nodes component uses hostname" in smoke and "a34NodeDisplayName(n)" in app)
