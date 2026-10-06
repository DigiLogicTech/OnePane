#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
ui=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js"); css=read("internal/webui/static/style.css"); api=read("internal/api/server.go")
projects=read("internal/projectworkspace/service.go")+read("internal/projectworkspace/repository_sql.go")
sched=read("internal/scheduler/types.go")+read("internal/scheduler/service.go")+read("internal/scheduler/catalog.go")
worker=read("internal/agentworker/service.go")+read("internal/agentworker/execution.go")
sandbox=read("internal/sandboxrunner/adapter.go")+read("internal/sandboxrunner/cli.go")+read("internal/sandboxrunner/types.go")
checks=[]
def ck(n,c): checks.append((n,bool(c)))
ck("Tasks retain current and scheduled paths", "Recurring / Scheduled" in ui and "/v1/routines" in ui and "GET /v1/routines" in api)
ck("Projects retain nested Workspaces", "workspace-tabs" in ui and "qa4SaveProjectWorkspaces" in ui and "UpdateProjectPolicy" in projects)
ck("Workspace layout has real drag resize remove add", all(x in ui for x in ["data-pw-widget","data-pw-drag","data-pw-resize","data-pw-remove","qa4AddWorkspaceComponent"]) and "layout-resize-handle" in css)
ck("Workspace settings stay per Workspace", "qa7SaveWorkspaceSettings" in ui and "Saved for this workspace only." in ui)
ck("Workspace sandbox controls remain", all(x in ui for x in ["Internet access","LAN access","Browser capability","Computer capability"]))
ck("scheduler candidates API remains", "GET /v1/scheduler/candidates" in api and "/v1/scheduler/candidates?workspace_id=" in ui)
ck("scheduler selected candidates remain enforced", "IncludeCandidateIDs" in worker and "IncludeCandidateIDs" in sched and "not_selected_by_request" in sched)
ck("sandbox network isolation remains", "EnsureNetwork" in sandbox and "no_new_privileges" in ui)
ck("Models are Local and Cloud", 'data-a31-model-view="local"' in ui and 'data-a31-model-view="cloud"' in ui)
ck("local models require trusted installability", "model.installable" in ui and "installable_quantizations" in api)
ck("cloud providers use durable provider records", "/v1/providers?workspace_id=" in ui and "GET /v1/providers" in api)
ck("OAuth is real PKCE infrastructure", "/v1/provider-oauth/{presetID}/start" in api and "/v1/provider-oauth/callback" in api and "a31StartOAuth" in ui)
ck("OmniRoute exposes managed and external modes", 'a31ComponentButtons("omniroute"' in ui and "omniQA(false)" in ui and "omniQA(true)" in ui)
ck("Operations has functional Activity Health Recovery", all(x in ui for x in ["a31OperationsActivity","a31OperationsHealth","a31RecoveryContent"]))
ck("Inspector governed chat remains", "qa4SendInspectorChat" in ui and "scheduling_class:'user_interactive'" in ui)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
 print(f"\nWINDOWS QA4 SOURCE: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
 for n in failed: print(" - "+n,file=sys.stderr)
 sys.exit(1)
print(f"\nWINDOWS QA4 SOURCE: ALL {len(checks)} CHECKS PASSED")
