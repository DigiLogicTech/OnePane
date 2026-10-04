#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding='utf-8')
ui=read('internal/webui/static/app.js')
css=read('internal/webui/static/style.css')
api=read('internal/api/server.go')
projects=read('internal/projectworkspace/service.go')
projrepo=read('internal/projectworkspace/repository_sql.go')
sched=read('internal/scheduler/types.go')+read('internal/scheduler/service.go')+read('internal/scheduler/catalog.go')
worker=read('internal/agentworker/service.go')+read('internal/agentworker/execution.go')
sandbox=read('internal/sandboxrunner/adapter.go')+read('internal/sandboxrunner/cli.go')+read('internal/sandboxrunner/types.go')
reconciler=read('internal/projectruntime/reconciler.go')

checks=[]
def ck(name, cond): checks.append((name,bool(cond)))

# Navigation / IA consolidation.
ck('legacy Workspaces route aliases into Projects', "workspaces:'projects'" in ui)
ck('legacy Sandboxes route aliases into Projects', "sandboxes:'projects'" in ui)
ck('legacy Providers route aliases into Models', "providers:'models'" in ui)
ck('legacy Routines route aliases into Tasks', "routines:'tasks'" in ui)
ck('legacy top-level destinations removed from nav', "navItems.filter(([route])=>!Object.prototype.hasOwnProperty.call(QA4_ROUTE_ALIASES,route))" in ui)
ck('Projects product title is current', "pages.projects.title='Projects'" in ui and 'Projects & Workspaces' not in ui)
ck('Models product title is current', "pages.models.title='Models'" in ui and 'Models & Cloud' not in ui)

# Tasks and schedules.
ck('Tasks page includes current and scheduled tabs', 'Recurring / Scheduled' in ui and 'data-task-tab="scheduled"' in ui)
ck('scheduled task creation is real', "apiRequest('/v1/routines',{method:'POST'" in ui and 'func (s *Server) createRoutine' in api)
ck('routines list API is exposed', 'GET /v1/routines' in api and 'func (s *Server) listRoutines' in api)
ck('task-list widget exists', "['tasks','Task list']" in ui)
ck('scheduled-task widget exists', "['scheduled','Scheduled tasks']" in ui and 'qa4ScheduledCard' in ui)
ck('Operations can add scheduled-task widget', 'Scheduled tasks' in ui and 'operations' in ui)

# Projects -> nested workspaces + chat.
ck('project hub renders nested workspace tabs', 'workspace-tabs' in ui and 'data-qa4-workspace' in ui)
ck('projects can add workspaces', 'qa4AddWorkspace' in ui and 'Create workspace' in ui)
ck('project workspace layouts are durable policy', 'onepane_ui' in ui and 'qa4SaveProjectWorkspaces' in ui and 'UpdateProjectPolicy' in projects)
ck('workspace chat is a movable component', "QA6_COMPONENTS.chat" not in ui and "chat:{title:'Chat'" in ui and 'qa7ChatContent' in ui and "$('.workspace-chat-panel')?.remove()" in ui)
ck('workspace chat creates governed tasks', "apiRequest('/v1/tasks',{method:'POST'" in ui and "scheduling_class:'user_interactive'" in ui)
ck('workspace layout supports drag/move/size/remove/add', all(x in ui for x in ['data-pw-widget','data-pw-move','data-pw-size','data-pw-remove','data-qa4-add-component']))
ck('workspace layout uses bounded snap presets', all(x in ui for x in ["'small'","'medium'","'large'","'wide'","'full'"]) and 'workspace-grid' in css)

# Context configuration / routing.
ck('workspace settings is a movable inspector/workspace component', "QA6_COMPONENTS.settings={title:'Workspace settings'" in ui and "QA6_INSPECTOR_COMPONENTS.push('settings')" in ui)
ck('workspace sandbox controls include internet/LAN/computer/browser', all(x in ui for x in ['Internet access','LAN access','Browser capability','Computer capability']))
ck('visible chat modes are Direct Team Council', "const qa8VisibleModes=['direct','team','council']" in ui and "function qa8Mode(v)" in ui and 'data-qa7-chat-mode' in ui)
ck('role model selectors exist', 'data-qa7-role-model' in ui)
ck('role agent selectors exist', 'data-qa7-role-agent' in ui)
ck('worker count is configurable', 'data-qa7-worker-count' in ui)
ck('scheduler candidates API exists', 'GET /v1/scheduler/candidates' in api and 'func (s *Server) listSchedulerCandidates' in api)
ck('UI loads real scheduler candidates', '/v1/scheduler/candidates?workspace_id=' in ui and 'qa4ModelOptions' in ui and 'qa4AgentOptions' in ui)
ck('operator routing selection is placed into task completion', 'onepane_routing' in ui and 'candidate_id:selected.candidate_id' in ui)
ck('agent worker consumes selected candidate', 'IncludeCandidateIDs' in worker and 'routingPolicyFromCompletion' in worker)
ck('scheduler enforces selected candidate IDs', 'IncludeCandidateIDs' in sched and 'not_selected_by_request' in sched)

# Project runtime sandbox policy actually enforced.
ck('project runtime policy PATCH API exists', 'PATCH /v1/project-runtimes/{runtimeID}/policy' in api and 'func (s *Server) updateRuntimePolicy' in api)
ck('runtime policy update is revision controlled', 'UpdateRuntimePolicy' in projects and 'revision=revision+1' in projrepo)
ck('workspace save persists workspace-owned policy without mutating shared project runtime', 'qa7SaveWorkspaceSettings' in ui and 'Saved for this workspace only.' in ui and 'workspace.routing={enabled:' in ui)
ck('sandbox isolated/external modes are encoded', "mode:'external'" in ui and "mode:'deny_by_default'" in ui)
ck('sandbox creates dedicated project network', 'EnsureNetwork' in sandbox and 'project network' in sandbox)
ck('reconciler checks network policy', 'network_policy' in reconciler and 'NetworkInternal' in reconciler)
ck('unsafe privilege escape remains blocked', all(x in ui for x in ['docker_socket:false','device_passthrough:false','host_mounts:[]','no_new_privileges:true']))
ck('UI exposes independent workspace Internet and LAN policy', 'Internet access' in ui and 'LAN access' in ui and 'workspace.sandbox.network=workspace.sandbox.internet||workspace.sandbox.lan' in ui)

# Models, direct cloud providers, and OmniRoute separation.
ck('Models page is split Local and Cloud', 'Local models' in ui and 'Cloud providers' in ui and 'models-cloud-layout' in ui)
ck('local models can be downloaded', 'data-download-model' in ui and '/v1/local-ai/install-jobs' in ui)
ck('cloud providers use real provider records', '/v1/providers?workspace_id=' in ui and 'GET /v1/providers' in api)
ck('cloud provider tiles expose connection status', 'OAuth connected' in ui and "'Connected'" in ui)
ck('cloud providers expose revoke', 'data-revoke-cloud' in ui and '/revoke' in ui and 'POST /v1/providers/{providerID}/revoke' in api)
ck('OmniRoute is visually and logically separate', 'omniroute-separate' in ui and 'This is separate from OmniRoute.' in ui and "p.id!=='omniroute'" in ui)
ck('Integrations cross-links Models', 'Cloud models & providers' in ui and 'Open Models' in ui and 'Open Models & Cloud' not in ui)
ck('OAuth is not falsely represented as complete', 'dedicated browser OAuth start/callback broker is not yet exposed' in ui)

# Operations contextual Inspector.
ck('Operations items are inspectable', 'data-inspect-kind' in ui and 'qa4Inspect' in ui)
ck('task/node/provider/event/routine inspection supported', all(f"kind==='{x}'" in ui for x in ['task','routine','node','provider','event']))
ck('Inspector supports notes and model chat tabs', "['notes','chat']" in ui and 'qa4InspectorChatForm' in ui and 'qa4InspectorNotes' in ui)
ck('Inspector optional tabs can be added', 'qa4InspectorAddTab' in ui)
ck('Inspector optional tabs can be removed', 'qa4RemoveInspectorTab' in ui and 'data-qa4-inspector-remove' in ui)
ck('Inspector optional tabs can be reordered', 'qa4MoveInspectorTab' in ui and 'data-qa4-inspector-move' in ui)
ck('Inspector chat is governed via Tasks', 'qa4SendInspectorChat' in ui and "scheduling_class:'user_interactive'" in ui)

# No stale wording implying only internal networks.
ck('sandbox status wording handles external project networks', 'project runtime workspace and project network prepared' in sandbox and 'create project network' in sandbox)

failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
    print(f"\nWINDOWS QA4 SOURCE: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for n in failed: print(' - '+n,file=sys.stderr)
    sys.exit(1)
print(f"\nWINDOWS QA4 SOURCE: ALL {len(checks)} CHECKS PASSED")
