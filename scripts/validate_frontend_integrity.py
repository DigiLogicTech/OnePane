#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
ui = (ROOT / "internal/webui/static/app.js").read_text(encoding="utf-8")
win = (ROOT / "packaging/windows/desktop/static/app.js").read_text(encoding="utf-8")

groups = {
    "tasks/schedules": ["Recurring / Scheduled", 'data-task-tab="scheduled"', "/v1/routines"],
    "project workspaces": ["workspace-tabs", "data-qa4-workspace", "qa4SaveProjectWorkspaces"],
    "workspace layout": ["data-pw-widget", "data-pw-move", "data-pw-size", "data-pw-remove", "data-qa4-add-component"],
    "routing": ["/v1/scheduler/candidates?workspace_id=", "onepane_routing", "candidate_id:selected.candidate_id"],
    "sandbox policy": ["mode:'external'", "mode:'deny_by_default'", "no_new_privileges:true"],
    "local model install": ["/v1/local-ai/install-jobs", "data-download-model"],
    "inspector": ["qa4InspectorChatForm", "qa4RemoveInspectorTab", "qa4MoveInspectorTab", "qa4SendInspectorChat"],
    "assistant": ["ONEPANE ASSISTANT", "qa31OpenAssistant", "/v1/assistant/threads/"],
    "project orchestrator": ["PROJECT ORCHESTRATOR", "qa31ProjectOrchestratorStrip", "/orchestrator/turns"],
    "agents": ["/v1/agent-profiles", "/v1/agent-sessions", "Profiles", "Councils"],
    "settings": ["Defaults for new Workspaces", "OnePane Assistant defaults", "/v1/about"],
    "colibri lifecycle": ["/v1/local-ai/component-jobs/", "qa31ColibriActionButtons", "Resume", "Repair"],
    "operations actions": ["qa31BindOperationCardActions", "Provider Health", "Recent Activity"],
}

failed = []
for name, needles in groups.items():
    missing = [n for n in needles if n not in ui]
    if missing:
        failed.append((name, missing))
        print(f"[FAIL] {name}: missing {missing}")
    else:
        print(f"[PASS] {name}")

for stale in ["chat-demo-1", "Provider Bots", "Bot Runtime"]:
    if stale in ui:
        failed.append(("stale demo/bot UI", [stale]))
        print(f"[FAIL] stale demo/bot UI: {stale}")
    else:
        print(f"[PASS] stale marker absent: {stale}")

if ui != win:
    failed.append(("canonical/Windows frontend parity", ["app.js differs"]))
    print("[FAIL] canonical/Windows frontend parity")
else:
    print("[PASS] canonical/Windows frontend parity")

if failed:
    print(f"\nFRONTEND INTEGRITY: {len(failed)} CHECK(S) FAILED", file=sys.stderr)
    sys.exit(1)
print("\nFRONTEND INTEGRITY: ALL CHECKS PASSED")
