#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
ui=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js"); css=read("internal/webui/static/style.css")
worker=read("internal/agentworker/service.go")+read("internal/agentworker/execution.go")+read("internal/agentworker/workspace_policy.go")+read("internal/agentworker/helpers.go")
scheduler=read("internal/scheduler/types.go")
checks=[]
def ck(n,c): checks.append((n,bool(c)))
ck("Settings retains inheritance-only defaults", "workspace_defaults" in ui and "Existing Workspaces are never changed here" in ui)
ck("Workspace Settings remains a reusable component", "Workspace settings" in ui and "qa7SaveWorkspaceSettings" in ui)
ck("Chat modes remain Direct Team Council", all(x in ui for x in ["Direct","Team","Council"]) and "data-qa7-chat-mode" in ui)
ck("routing toggle remains Workspace scoped", "data-qa7-routing" in ui and "workspace.routing" in ui)
ck("remote reasoning remains Workspace scoped", "data-qa7-remote" in ui and "Allow qualified enrolled-node / cloud reasoning workers" in ui)
ck("remote workspace access remains brokered", "access_mode:'brokered'" in ui and "Remote models never receive a host filesystem mount" in ui)
ck("worker enforces Workspace capability policy", all(x in worker for x in ["workspaceToolAllowed","browser capability is disabled for this workspace","computer capability is disabled for this workspace","internet access is disabled for this workspace"]))
ck("routing disabled suppresses delegation", "delegation is disabled by workspace routing policy" in worker and "automatic escalation is disabled by workspace routing policy" in worker)
ck("remote disabled constrains scheduler", "LocalOnly: !rp.AllowRemote" in worker and "remote candidate disallowed by workspace policy" in scheduler)
ck("Follow reports execution target", "Execution target" in ui and "candidate_kind" in worker)
ck("legacy fixed chat surface removed", "$('.workspace-chat-panel')?.remove()" in ui)
ck("Workspace components use deterministic xywh layout", "a31NormalizeLayout" in ui and all(x in ui for x in ["item.x","item.y","item.width","item.height"]))
ck("Workspace components have pointer drag resize", all(x in ui for x in ["data-pw-drag","data-pw-resize","a31BindLayout"]) and ".layout-resize-handle" in css)
ck("Workspace layout persists once through Project policy", "qa4SaveProjectWorkspaces" in ui and "a31RefreshProjectGrid" in ui)
ck("mobile suppresses freeform handles", "@media(max-width:700px)" in css and ".layout-resize-handle,.dashboard-drag{display:none!important}" in css)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
 print(f"\nWORKSPACE COMPONENTS V2: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
 for n in failed: print(" - "+n,file=sys.stderr)
 sys.exit(1)
print(f"\nWORKSPACE COMPONENTS V2: ALL {len(checks)} CHECKS PASSED")
