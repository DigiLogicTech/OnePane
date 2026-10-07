#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
foundation=(ROOT/"internal/webui/static/app-foundation.js").read_text(encoding="utf-8")
canonical=(ROOT/"internal/webui/static/app.js").read_text(encoding="utf-8")
models=(ROOT/"internal/webui/static/models-page.js").read_text(encoding="utf-8")
ui=foundation+"\n"+canonical+"\n"+models
groups={
 "tasks/schedules":["Recurring / Scheduled","/v1/routines"],
 "project workspaces":["workspace-tabs","qa4SaveProjectWorkspaces","a31RefreshProjectGrid"],
 "workspace layout":["data-pw-widget","data-pw-drag","data-pw-resize","data-pw-remove"],
 "routing":["/v1/scheduler/candidates?workspace_id=","onepane_routing"],
 "sandbox policy":["mode:'external'","mode:'deny_by_default'","no_new_privileges:true"],
 "local model install":["/v1/local-ai/install-jobs","Download & Install","model.installable"],
 "compute placement":["a31OpenCompute","Require GPU","Require CPU","Hybrid / CPU + GPU"],
 "inspector":["qa4InspectorChatForm","qa4RemoveInspectorTab","qa4MoveInspectorTab","qa4SendInspectorChat"],
 "assistant/orchestrator":["a31RenderControlChat","/v1/assistant/threads/","/orchestrator/turns"],
 "agents/research":["/v1/agent-profiles","research_mode","full_provenance"],
 "skills":["a31UploadSkill","/v1/skills/packages","Assignments","Packages"],
 "nodes":["renderNodes=async function","/v1/nodes"],
 "settings":["Defaults for new Workspaces","Providers & Auth","Nodes & Federation","/v1/about"],
 "managed components":["/v1/local-ai/component-jobs/",'a31RuntimeCard("colibri"','a31RuntimeCard("omniroute"'],
 "operations":["a31OperationsActivity","a31OperationsHealth","a31RecoveryContent","a31ToggleLogs"],
}
failed=[]
for name,needles in groups.items():
 missing=[n for n in needles if n not in ui]
 if missing:
  failed.append((name,missing)); print(f"[FAIL] {name}: missing {missing}")
 else: print(f"[PASS] {name}")
for stale in ["chat-demo-1","Provider Bots","Bot Runtime"]:
 if stale in ui: failed.append(("stale demo/bot UI",[stale])); print(f"[FAIL] stale marker: {stale}")
 else: print(f"[PASS] stale marker absent: {stale}")
if (ROOT/"packaging/windows/desktop/static").exists():
 failed.append(("single-source frontend",["Windows static copy still exists"])); print("[FAIL] Windows static copy still exists")
else: print("[PASS] Windows desktop uses canonical backend WebUI only")
if failed:
 print(f"\nFRONTEND INTEGRITY: {len(failed)} CHECK(S) FAILED",file=sys.stderr); sys.exit(1)
print("\nFRONTEND INTEGRITY: ALL CHECKS PASSED")
