#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding='utf-8')
ui=read('internal/webui/static/app.js')
css=read('internal/webui/static/style.css')
worker=read('internal/agentworker/service.go')+read('internal/agentworker/execution.go')+read('internal/agentworker/workspace_policy.go')+read('internal/agentworker/helpers.go')
scheduler=read('internal/scheduler/types.go')

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

ck('Settings retains Defaults inheritance semantics', "pages.settings.title='Settings'" in ui and 'workspace_defaults' in ui and 'Existing Projects and Workspaces retain their own policy' in ui)
ck('workspace defaults migrate legacy project defaults', 'legacy=stored.project_defaults||{}' in ui and 'workspace_defaults' in ui and 'delete next.project_defaults' in ui)
ck('workspace Settings is a reusable component', "QA6_COMPONENTS.settings={title:'Workspace settings'" in ui and "QA6_INSPECTOR_COMPONENTS.push('settings')" in ui)
ck('workspace Settings can be seeded into new layouts', "type:'settings',title:'Workspace settings'" in ui)
ck('workspace Settings persist per workspace', 'qa7SaveWorkspaceSettings' in ui and 'Saved for this workspace only.' in ui and 'qa4SaveProjectWorkspaces' in ui)
ck('chat mode selector lives inside Chat component', 'data-qa7-chat-mode' in ui and "['default',`Default (" in ui and all(x in ui for x in ["['direct','Direct']","['team','Team']","['council','Council']"]) and "const qa8VisibleModes=['direct','team','council']" in ui)
ck('chat continues using slash commands', "api.chatCommand" in ui and 'qa7DrawSlashSuggestions' in ui)
ck('model routing toggle is workspace scoped', 'data-qa7-routing' in ui and 'Enable model routing and automatic fallback' in ui and 'workspace.routing={' in ui)
ck('routing off collapses multi-agent chat modes', "workspace.routing?.enabled===false&&mode!=='direct'" in ui)
ck('team roles carry primary and fallback selections', all(x in ui for x in ['fallback_model','fallback_agent','fallback_candidate_ids','fallback_agent_profiles']))
ck('workspace sandbox is stored on workspace', 'workspace.sandbox=' in ui and 'qa7WorkspaceSandbox' in ui and 'Workspace sandbox' in ui)
ck('remote reasoning toggle is workspace scoped', 'data-qa7-remote' in ui and 'Allow qualified enrolled-node / cloud reasoning workers' in ui)
ck('remote workspace access is explicitly brokered', "access_mode:'brokered'" in ui and "mode:'brokered'" in ui and 'Remote models never receive a host filesystem mount' in ui)
ck('task carries workspace access policy', 'workspace_access:qa7WorkspaceAccess(workspace)' in ui and 'project_workspace_id:workspace.id' in ui)
ck('worker enforces brokered workspace capabilities', 'workspaceToolAllowed' in worker and 'browser capability is disabled for this workspace' in worker and 'computer capability is disabled for this workspace' in worker and 'internet access is disabled for this workspace' in worker and 'LAN access is disabled for this workspace' in worker and 'secret access is disabled for this workspace' in worker)
ck('delegated work inherits workspace routing and access policy', 'inheritOnePaneRouting' in worker and "p.Completion = inheritOnePaneRouting(t.Completion, p.Completion)" in worker)
ck('routing disabled suppresses delegation and escalation', 'delegation is disabled by workspace routing policy' in worker and 'automatic escalation is disabled by workspace routing policy' in worker and 'MaxEscalations = 0' not in worker and 'maxEscalations = 0' in worker)
ck('remote disabled constrains scheduler to local candidates', 'LocalOnly: !rp.AllowRemote' in worker and 'remote candidate disallowed by workspace policy' in scheduler)
ck('Follow reports actual execution target', 'candidate_kind' in worker and 'candidate_id' in worker and 'Execution target' in ui and 'qa7-follow-target' in css)
ck('fixed legacy chat surface is removed after workspace render', "$('.workspace-chat-panel')?.remove()" in ui)
ck('components retain generic move resize remove controls', all(x in ui for x in ['data-pw-move','data-pw-size','data-pw-remove','data-qa6-max']))
ck('Inspector supports tab and tiled component placement', 'Add Inspector tile' in ui and 'Add Inspector tab' in ui and 'qa6-inspector-panel-grid' in ui)

failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
print(f"\nWORKSPACE COMPONENTS V2: {len(checks)-len(failed)}/{len(checks)} CHECKS PASSED")
if failed:
    for n in failed: print(' - '+n,file=sys.stderr)
    sys.exit(1)
