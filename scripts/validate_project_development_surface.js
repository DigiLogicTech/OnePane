#!/usr/bin/env node
/* Static contract checks for the transitional Development-first Workspace view.
 * Full isolated runtime and GUI tests remain a separate release gate. */
'use strict';
const fs=require('fs');
const path=require('path');
const vm=require('vm');
const assert=require('node:assert/strict');
const root=path.resolve(__dirname,'..');
const read=p=>fs.readFileSync(path.join(root,p),'utf8');
const ui=read('internal/webui/static/project-development-page.js');
const css=read('internal/webui/static/project-development-page.css');
const index=read('internal/webui/static/index.html');
const workspace=read('internal/webui/static/project-workspace-page.js');
const foundation=read('internal/webui/static/app-foundation.js');
const architecture=read('docs/PROJECT_WORKSPACE_DEVELOPMENT_ENVIRONMENTS_RFC.md');

new vm.Script(ui,{filename:'project-development-page.js'});
const assertContains=(text,fragment,label)=>assert.ok(text.includes(fragment),label);
assertContains(index,'/project-development-page.js','development script must be loaded');
assertContains(index,'/project-development-page.css','development styling must be loaded');
assert.ok(index.indexOf('/project-workspace-page.js')<index.indexOf('/project-development-page.js'),'development module loads after workspace renderer');
assertContains(ui,'const a44RenderWorkspaceDashboard=renderWorkspaces','legacy dashboard must be retained');
assertContains(ui,'||"development"','new Workspace defaults to Development view');
assertContains(ui,'id="a44DashboardTab"','secondary dashboard must be reachable');
assertContains(ui,'Legacy shared Project sandbox (compatibility)','no false Workspace-level runtime-isolation claim');
assertContains(ui,'/v1/projects/','runtime is read from the server');
assertContains(ui,'/applications','application state is read from the server');
assertContains(ui,'runtime?.status','use observed runtime status, not local UI checkbox');
assertContains(ui,'container.isConnected','do not repaint a disposed page');
assertContains(workspace,'"Workspace isolation pending"','dashboard capability chip must not lie');
assertContains(foundation,'Requested Workspace capabilities','request flags must not imply security permissions');
assertContains(foundation,'not runtime permissions','do not misstate runtime policy');
assertContains(architecture,'project_runtimes.project_id','known schema blocker must be documented');
assertContains(architecture,'Workspace-environment ownership','required backend migration must be documented');
assert.ok(!/privileged[=:]true|--privileged/.test(ui),'UI must never enable privileged execution');
assert.ok(!/exec.Command|host executable/.test(ui),'UI does not trigger direct host commands');
assertContains(css,'var(--border)','development view must use shared theme borders');
assertContains(css,'var(--text)','development view must use shared theme text');
const links=read('internal/webui/static/workspace-collaboration.js');
const lib=read('internal/webui/static/workspace-library.js');
const libRoute=read('internal/webui/static/project-library-route.js');
const runtimeUI=read('internal/webui/static/workspace-runtime-controls.js');
const workflowUI=read('internal/webui/static/workspace-ai-workflow.js');
new vm.Script(workflowUI,{filename:'workspace-ai-workflow.js'});
new vm.Script(runtimeUI,{filename:'workspace-runtime-controls.js'});
const api=read('internal/api/project_library.go');
const routes=read('internal/api/server.go');
const migration=read('migrations/0036_workspace_artifact_links.sql');
new vm.Script(links,{filename:'workspace-collaboration.js'});
new vm.Script(lib,{filename:'workspace-library.js'});
new vm.Script(libRoute,{filename:'project-library-route.js'});
assertContains(index,'/workspace-collaboration.js','collaboration UI must be loaded');
assertContains(index,'/workspace-library.js','Project Library UI must be loaded');
assertContains(index,'/project-library-route.js','first-class Library route must be loaded');
assert.ok(index.indexOf('/workspace-collaboration.js')<index.indexOf('/workspace-library.js'),'Library loads after collaboration');
assertContains(libRoute,'pages.library','global Library navigation must register');
assertContains(ui,'a49MountDevelopmentTasks','Workspace must queue governed Project Tasks');
assertContains(workflowUI,'project_workspace_id:canonical.id','AI jobs must use canonical Workspace identities');
assertContains(workflowUI,'remote_models:values.allow_remote==="yes"',
 'Workspace Tasks must require explicit cloud routing opt-in');
assertContains(workflowUI,'name="allow_remote"','Workspace cloud choice must be visible and default unchecked');
assert.ok(!/name="allow_remote"[^>]*checked/.test(workflowUI),
 'Workspace Task cloud models must not be preapproved');
assertContains(workflowUI,'mode:"brokered",project_workspace_id:canonical.id',
 'Workspace Tasks must create a brokered capability envelope');
assertContains(workflowUI,'secrets:"none"','Workspace Tasks must not implicitly expose Vault secrets');
assertContains(workflowUI,'/v1/tasks?workspace_id=','Workspace queue must read actual Task API state');
assertContains(workflowUI,'"&project_id="+encodeURIComponent(project.id)', 'Workspace Task inventory must query scoped backend');
assertContains(workflowUI,'"&project_workspace_id="+encodeURIComponent(canonical.id)', 'Workspace Task inventory must pass canonical Workspace selector');
const tasksAPI=read('internal/api/server.go');
const taskListMethod=tasksAPI.slice(tasksAPI.indexOf('func (s *Server) listTasks('),tasksAPI.indexOf('func (s *Server) createTask('));
assertContains(taskListMethod,'reader.ListProjectWorkspace(', 'Tasks API must list scoped Tasks before applying row limit');
assertContains(taskListMethod,'workspaceRow.ProjectID!=projectID','Tasks API must validate requested Workspace ownership');
assertContains(workflowUI,'t.project_workspace_id===canonical.id',
 'Workspace queue must filter strictly to the canonical Project Workspace');
assertContains(workflowUI,'t.project_id===project.id',
 'Workspace queue must exclude Tasks from other Projects');
assertContains(workflowUI,'id="a49RefreshTasks"','Workspace task inventory must be manually refreshable');
assertContains(workflowUI,'await loadTasks();','Task creation must refresh observed Task status');
assertContains(ui,'a48MountWorkspaceRuntime','canonical Workspace sandbox must be mounted');
assertContains(runtimeUI,'/desired-state','Workspace runtime lifecycle must use governed runtime endpoint');
assertContains(runtimeUI,'@sha256:','toolchains must require pinned images');
assertContains(runtimeUI,'data-a48-app-action','each provisioned tool must expose start/stop controls');
assertContains(runtimeUI,'expected_revision','app state changes must be revision-checked');
assertContains(routes,'s.setApplicationDesired','API must expose governed application lifecycle route');
const runtimeAccess=read('internal/api/workspace_runtimes.go');
assertContains(runtimeAccess,'if write && !s.authorize(w,r,i,p.WorkspaceID,"project.run")',
 'Workspace sandbox provisioning must require execution authority');
const within=(start,end)=>{const a=routes.indexOf(start),b=routes.indexOf(end,a+start.length);
 assert.ok(a>=0&&b>a,'Missing API handler boundary: '+start);return routes.slice(a,b)};
const initialRuntime=within('func (s *Server) createRuntime(', 'func (s *Server) getRuntimeByProject(');
const initialApp=within('func (s *Server) declareApplication(', 'func (s *Server) listApplications(');
assertContains(initialRuntime,'in.DesiredState == projectworkspace.RuntimeDesiredRunning',
 'Legacy initial Running runtime creation must be guarded');
assertContains(initialRuntime,'"project.run"','Legacy initial Running runtime needs project.run');
assertContains(initialApp,'if !s.authorize(w, r, i, p.WorkspaceID, "project.run")',
 'All application installations must require project.run, even if initially stopped');
assertContains(ui,'a45MountCollaboration','Development renderer must mount collaboration explicitly');
assertContains(ui,'a46MountWorkspaceLibrary','Development renderer must mount Library explicitly');
assertContains(links,'async function a45MountCollaboration','collaboration must export a mount function');
assertContains(lib,'async function a46MountWorkspaceLibrary','Library must export a mount function');
assert.ok(!links.includes('a44LoadProjectEnvironment='),'collaboration cannot globally replace Development renderer');
assert.ok(!lib.includes('a44LoadProjectEnvironment='),'Library cannot globally replace Development renderer');
assertContains(links,'/workspace-links','connection source must use authorised API');
assertContains(links,'legacy_workspace_id','connection cannot confuse dashboard and canonical Workspace IDs');
assertContains(links,'/publications','publishers must use versioned channel publications');
assertContains(lib,'version_policy','Library must offer pinned and latest Workspace grants');
assertContains(lib,'X-OnePane-CSRF','uploads must include CSRF protection');
assertContains(api,'http.MaxBytesReader','uploads must have bounded streaming size');
assertContains(api,'ResolveWorkspaceLibraryVersion','Library download must enforce explicit Workspace access');
assertContains(api,'VerifyContent','downloads must verify immutable artifact bytes');
assertContains(migration,'CHECK (source_workspace_id<>target_workspace_id)','self-link must fail');
assertContains(routes,'revokeProjectLibraryGrant','Library must support access revocation');
const evidence=read('internal/webui/static/evidence-audit-page.js');
new vm.Script(evidence,{filename:'evidence-audit-page.js'});
assertContains(index,'/evidence-audit-page.js','Evidence route must mount real backend event surface');
assert.ok(index.indexOf('/project-library-route.js')<index.indexOf('/evidence-audit-page.js'),
 'Evidence final renderer must load after the Project Library route');
assertContains(evidence,'/v1/events?workspace_id=','Evidence events must come from authorised backend API');
assertContains(evidence,'onepaneWorkspace','Evidence read must use current tenancy scope');
assertContains(evidence,'a51PreviousRenderActiveView=renderActiveView',
 'Evidence route must preserve existing route chain');
assertContains(evidence,'slice(0,100)','Evidence view must bound client-side rows');
console.log('PASS: Development + Library/Workspace connection UI syntax, permissions and route contracts');
