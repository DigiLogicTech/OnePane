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
assertContains(ui,'<details class="panel-card a44-legacy-runtime"','legacy shared sandbox must be collapsible, not the primary Workspace surface');
assertContains(ui,'a44-development-shortcuts','direct Workspace development actions must remain discoverable');
assert.ok(ui.indexOf('a44-development-shortcuts')<ui.indexOf('a44-legacy-runtime'),'Workspace action panel must precede legacy compatibility');
assertContains(ui,'New development tools belong in the dedicated Workspace sandbox above.','legacy compatibility must not invite shared installations');
assertContains(css,'.a44-legacy-runtime>summary','legacy disclosure control must use theme-aware styling');
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
const workspaceExecSource=read('internal/agentworker/workspace_execution_manifest.go');
const agentExecSource=read('internal/agentworker/execution.go');
assertContains(agentExecSource,'ID:"workspace-execution-manifest"','Agent Worker must supply Task-owned OCI execution inventory');
const delegateSource=read('internal/agentworker/execution.go');
const ancestrySource=read('internal/agentworker/delegation_ancestry.go');
assertContains(delegateSource,'verifyDelegationAncestry(ctx,tx,t)',
 'delegation must validate persisted ancestry inside child creation transaction');
assertContains(ancestrySource,'maxAutonomousDelegationDepth = 6',
 'untrusted recursive delegation must be capped');
assertContains(ancestrySource,'project_workspace_id,parent_task_id',
 'delegation ancestry must verify persisted Project Workspace ownership');
assertContains(delegateSource,'WaitDependencyInTransaction(ctx,tx',
 'delegated Task/Attempt wait must join child creation transaction');
assertContains(delegateSource,'s.journalInTransaction(ctx,tx',
 'delegated child journal must be part of the same durable transaction');
assertContains(delegateSource,"AND status='running' AND revision=?",
 'delegated Worker continuation must use a status/revision compare-and-swap');
assertContains(delegateSource,'return failedResult(res,err)',
 'failed delegation transaction must not overwrite a concurrent Worker incarnation');
const taskTransitionSource=read('internal/task/service.go');
assertContains(taskTransitionSource,'func (s *Service) WaitDependencyInTransaction(',
 'running Task/Attempt must expose a transaction-scoped dependency transition');
assert.ok(delegateSource.replace(/\s+/g,'').includes('ProjectWorkspaceID:t.ProjectWorkspaceID'),
 'delegated child Tasks must retain canonical Project Workspace ownership');
assertContains(delegateSource,'inheritOnePaneRouting(t.Completion, p.Completion)',
 'delegation must preserve parent model, sandbox and Vault restrictions');
assertContains(workspaceExecSource,'p.workspace_id=?','OCI execution inventory must respect Task tenant ownership');
assertContains(workspaceExecSource,'pw.id=? AND pw.status=','OCI execution inventory must enforce Task Project Workspace ownership');
assertContains(workspaceExecSource,'WHERE a.project_runtime_id=?','OCI applications must be scoped to the selected runtime before row limiting');
assertContains(workspaceExecSource,'"tool_id":"project.app.exec"','Workspace manifest must advertise actual governed sandbox command tool');
assert.ok(!workspaceExecSource.includes('environment_bindings_json') &&
 !workspaceExecSource.includes('secrets_json'), 'The model may not see raw Workspace credential bindings');
const reconcilerSource=read('internal/projectruntime/reconciler.go');
const bootstrapSource=read('internal/bootstrap/bootstrap.go');
assertContains(reconcilerSource,'if !localPlacementAllowed(r.localNodeID,runtime.NodeID)',
 'remote-assigned sandbox must be refused before local OCI execution');
assertContains(bootstrapSource,'verificationService, localNode.ID)',
 'OCI reconciler must receive its exact local Node identity from bootstrap');
const tasksAPI=read('internal/api/server.go');
const taskModelWaitProjection=read('internal/api/task_model_waits.go');
const workspaceTaskQueue=read('internal/webui/static/workspace-ai-workflow.js');
assertContains(tasksAPI,'loadTaskModelWaits(r.Context(),s.attentionDB,workspaceID,rows)',
 'Workspace Task API must derive wait state from persisted Worker data');
assertContains(taskModelWaitProjection,"AND t.workspace_id=? AND r.task_id IN (",
 'Task wait status must be tenant-scoped before revealing model details');
assertContains(workspaceTaskQueue,'Waiting for local model',
 'Workspace Task queue must distinguish model waits from dependency/approval waits');
const taskListMethod=tasksAPI.slice(tasksAPI.indexOf('func (s *Server) listTasks('),tasksAPI.indexOf('func (s *Server) createTask('));
assertContains(taskListMethod,'reader.ListProjectWorkspace(', 'Tasks API must list scoped Tasks before applying row limit');
assertContains(taskListMethod,'workspaceRow.ProjectID!=projectID','Tasks API must validate requested Workspace ownership');
assertContains(workflowUI,'t.project_workspace_id===canonical.id',
 'Workspace queue must filter strictly to the canonical Project Workspace');
assertContains(workflowUI,'t.project_id===project.id',
 'Workspace queue must exclude Tasks from other Projects');
const dependencyAPI=read('internal/api/task_dependency_evidence.go');
assertContains(routes,'loadWorkspaceTaskDependencies(r.Context(),s.attentionDB,',
 'canonical scoped Task list must derive durable dependency evidence from backend');
assertContains(dependencyAPI,'t.project_workspace_id=?',
 'prerequisite parent query must remain bound to canonical Workspace');
assertContains(dependencyAPI,'dep.project_workspace_id=t.project_workspace_id',
 'foreign child state must not be disclosed in prerequisite counts');
assertContains(dependencyAPI,'d.dependency_type=\'hard\'',
 'dependency projection must not falsely treat soft edges as blockers');
assertContains(workflowUI,'Other Workspace prerequisite identities and states are not disclosed here.',
 'Workspace queue must disclose restricted dependency scope without leaking private child states');
assertContains(workflowUI,'The parent cannot safely complete until its dependencies are resolved.',
 'failed or blocked prerequisites must surface manual review');
const checkpointAPI=read('internal/api/task_worker_progress.go');
assertContains(routes,'loadTaskExecutionProgress(r.Context(),s.attentionDB,',
 'scoped Workspace Tasks must expose real persisted Worker progress');
assertContains(checkpointAPI,'t.project_workspace_id=?',
 'Task checkpoint SQL must independently enforce canonical Workspace scope');
assertContains(checkpointAPI,'r.workspace_id=?',
 'Task checkpoint SQL must confirm run tenant ownership');
assertContains(checkpointAPI,'t.workspace_id=r.workspace_id',
 'Task checkpoint may never use a mismatched Worker run tenancy');
assertContains(workflowUI,'Persisted execution checkpoint',
 'Workspace Task UI must label journal progress as persisted, not a successful action');
assertContains(workflowUI,'External actions must not replay automatically.',
 'Interrupted Task must give operator a recovery integrity warning');
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
const previewAPI=read('internal/api/project_library_preview.go');
assertContains(routes,'s.previewWorkspaceLibraryVersion','Workspace text preview must be routed under Project+Workspace');
assertContains(previewAPI,'workspaceRuntimeContext(w,r,false)','preview must authenticate canonical Workspace access');
assertContains(previewAPI,'ResolveWorkspaceLibraryVersion','preview must recheck current version grants');
assertContains(previewAPI,'VerifyContent','preview must independently verify managed content');
assertContains(previewAPI,'text/plain; charset=utf-8','preview must not execute uploaded HTML');
assertContains(previewAPI,'nosniff','text preview response must resist MIME sniffing');
assertContains(lib,'pane.textContent=content','Workspace Library must render untrusted source as plain text');
assertContains(lib,'/preview','Workspace Library must use grant-scoped preview endpoint');
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
const qaPlan=read('docs/RC11_REMAINING_DEVELOPMENT_AND_REVIEW_GATES.md');
assertContains(qaPlan,'RC11-13 #85','Debug Centre must remain on the RC11 development worklist');
assertContains(qaPlan,'RC11-14 #86','three-phase audit/vision/review must gate packaging');
assertContains(qaPlan,'**First full code review:**','first code audit must precede vision session');
assert.ok(qaPlan.indexOf('**First full code review:**')<qaPlan.indexOf('**Vision alignment interview:**')&&
 qaPlan.indexOf('**Vision alignment interview:**')<qaPlan.indexOf('**Second code review and realignment:**'),
 'review gate must preserve user-required audit → questions → second review order');
const qaFailureMigration=read('migrations/0041_agentcheck_failure_observations.sql');
const qaFailureWriter=read('internal/localai/agentcheck_failure.go');
const qaTestbedWriter=read('internal/localai/testbed.go');
assertContains(qaFailureMigration,'CREATE TABLE model_agentcheck_failure_observations',
 'Agent Check structured failure migration is required');
assertContains(qaFailureMigration,'model_agentcheck_failure_no_update',
 'typed observations must be immutable');
assertContains(qaFailureMigration,'model_agentcheck_failure_no_delete',
 'typed observations must not be deletable');
assert.ok(!qaFailureMigration.includes('error_text')&&!qaFailureMigration.includes('payload_json')&&
 !qaFailureMigration.includes('reason TEXT'),'failure journal must not contain free text');
assertContains(qaFailureWriter,'agentCheckCategory(stage,cause)',
 'typed failures must classify from code path, not expose error text');
assertContains(qaFailureWriter,'2*time.Second',
 'best-effort failure record must have a fixed DB timeout');
assert.ok(!qaFailureWriter.includes('cause.Error()'),
 'error messages must never be stored in typed QA observations');
assertContains(qaTestbedWriter,'s.recordAgentCheckFailure(ctx,sess,"inference_dispatch",err)',
 'inference error code path must persist typed Agent Check evidence');
assertContains(qaTestbedWriter,'s.recordAgentCheckFailure(ctx,sess,"runtime_release",err)',
 'runtime cleanup failure must persist typed Agent Check evidence');
assertContains(qaModel,'LEFT JOIN model_agentcheck_failure_observations f',
 'Agent Check QA projection must expose machine-coded stage evidence');
assertContains(qaModel,'f2.deployment_id=s.deployment_id',
 'failure observation must be restricted by both session and deployment');
assertContains(qaModel,'qaModelFailureCategory(category.String)',
 'unknown failure categories must be sanitized');
const qaModel=read('internal/api/qa_model_evidence.go');
const qaModelHandler=read('internal/api/qa_model_evidence_handler.go');
const qaAgentCheckUI=read('internal/webui/static/model-agentcheck-diagnostics.js');
assertContains(routes,'GET /v1/qa/model-deployments/{deploymentID}/agent-check',
 'QA Agent Check evidence must be routed through authenticated backend');
assertContains(qaModelHandler,'s.authenticate(w,r)',
 'QA model report must authenticate principal');
assertContains(qaModelHandler,'s.authorizeManagedDeployment(w,r,i,dep,"model.read")',
 'QA model report must honor managed deployment scope and policy');
assertContains(qaModel,'qaModelSessionCap=10',
 'Agent Check QA session projection must be bounded');
assertContains(qaModel,'WHERE s.deployment_id=?',
 'Agent Check QA must not enumerate other deployments');
assert.ok(!qaModel.includes('request_json')&&!qaModel.includes('response_json')&&
 !qaModel.includes('notes,')&&!qaModel.includes('placement_json')&&
 !qaModel.includes('last_error'),
 'QA Agent Check projection must not read raw user/model content');
assertContains(qaAgentCheckUI,'a55ShowAgentCheckEvidence(id)',
 'Agent Check UI must expose governed read-only evidence');
assertContains(qaAgentCheckUI,'panel.textContent=report',
 'QA Agent Check evidence must be rendered as plain text');
assertContains(qaAgentCheckUI,'Download reviewed JSON',
 'QA model export must be reviewed before download');
const qaSource=read('internal/api/qa_snapshot.go');
const qaAPI=read('internal/api/qa_snapshot_handler.go');
const qaUI=read('internal/webui/static/workspace-ai-workflow.js');
const incidentRecorder=read('internal/webui/static/qa-incident-capture.js');
new vm.Script(incidentRecorder,{filename:'qa-incident-capture.js'});
require('node:child_process').execFileSync(process.execPath,
 [path.join(root,'scripts/validate_qa_incident_capture.js')],
 {cwd:root,stdio:'inherit'});
assertContains(index,'/qa-incident-capture.js','QA capture module must be loaded in WebUI');
assert.ok(index.indexOf('/qa-incident-capture.js')<index.indexOf('/workspace-ai-workflow.js'),
 'capture module must load before Workspace QA UI');
assertContains(incidentRecorder,'MAX_EVENTS=120','capture must have a bounded in-memory event buffer');
assertContains(incidentRecorder,'MAX_DURATION_MS=10*60*1000','capture must automatically expire');
assertContains(incidentRecorder,'u.origin!==origin','cross-origin fetches must be excluded');
assertContains(incidentRecorder,'listeners=[]','capture lifecycle must restore DOM listeners');
assertContains(incidentRecorder,'if(wrappedFetch&&win.fetch===wrappedFetch)win.fetch=restoreFetch',
 'capture must restore the original fetch implementation');
assertContains(qaUI,'incident.start();paintIncident()','capture requires explicit user action');
assertContains(qaUI,'incident.mark();paintIncident()','QA operator must be able to mark a defect');
assertContains(qaUI,'incident.stop();','QA operator must be able to stop a capture');
assertContains(qaUI,'incident.clear();','QA operator must be able to clear a capture');
assertContains(qaUI,'incidentReviewedJSON=JSON.stringify(snapshot,null,2)',
 'QA incident trace must be previewed before export');
assertContains(qaUI,'onepane-browser-incident.json','local incident report must be user-initiated');
const qaSummary=read('internal/webui/static/qa-report-summary.js');
new vm.Script(qaSummary,{filename:'qa-report-summary.js'});
require('./validate_qa_report_summary.js');
assertContains(index,'/qa-report-summary.js','QA summary must be loaded in WebUI before Task controls');
assert.ok(index.indexOf('/qa-report-summary.js')<index.indexOf('/workspace-ai-workflow.js'),
 'QA summary formatter must be loaded before Workspace development UI');
assertContains(qaUI,'a52MakeQASummary(snapshot)','copyable summary must derive from authorised preview only');
assertContains(qaUI,'qaCopySummary.disabled=true','QA summary copy must remain disabled before review');
assertContains(qaUI,'navigator.clipboard.writeText(qaSummaryContent.value)',
 'manual user gesture must initiate sanitized QA summary copy');

new vm.Script(qaUI,{filename:'workspace-ai-workflow.js'});
assertContains(routes,'GET /v1/qa/workspace-snapshot','explicit QA preview route must be registered');
assertContains(routes,'POST /v1/qa/workspace-bundle','explicit QA export route must be registered');
assertContains(qaAPI,'s.authorize(w,r,i,tenant,"project.read")','QA export must check Project read policy');
assertContains(qaAPI,'s.authorize(w,r,i,tenant,"task.read")','QA export must check Task read policy');
assertContains(qaAPI,'view.ProjectID!=projectID','QA export must verify canonical Workspace ownership');
assertContains(qaAPI,'reader.ListProjectWorkspace','QA export must query canonical scoped Task reader');
assertContains(qaSource,'qaSnapshotTaskCap=50','QA snapshot Task count must remain bounded');
assertContains(qaSource,'qaSnapshotArchiveCap=128<<10','QA ZIP size must remain bounded');
assertContains(qaSource,'zip.NewWriter(&buf)','QA ZIP must remain on-device without filesystem temp churn');
assertContains(qaSource,'ExcludedCategories','QA ZIP must advertise deliberately omitted sensitive data');
const qaTimeline=read('internal/api/qa_event_timeline.go');
assertContains(qaTimeline,'qaTimelineEventCap = 96',
 'diagnostic event chronology must remain bounded');
assertContains(qaTimeline,"JOIN scoped s ON s.id=r.task_id",
 'Worker event lookup must re-authorise its persisted owning Task');
assertContains(qaTimeline,"AND t.workspace_id=?",
 'Worker event lookup must respect tenant ownership');
assertContains(qaTimeline,'qaKnownEventType(eventType)',
 'diagnostic export must allowlist event names');
assertContains(qaTimeline,'qaOpaqueRef("trace",traceID.String)',
 'diagnostic export must pseudonymise trace values');
assert.ok(!qaTimeline.includes('payload_json')&&!qaTimeline.includes('actor_principal_id'),
 'diagnostic chronology must not select raw audit payloads or actor identity');
assertContains(qaAPI,'loadQATimeline(r.Context(),s.attentionDB,',
 'QA preview and ZIP must include scoped Task/Worker chronology');
assertContains(qaSource,'CapturedTimelineEvents',
 'QA report schema must disclose timeline count and truncation');
assertContains(qaUI,'snapshot.timeline_truncated',
 'QA UI must disclose omitted older history');
assertContains(qaUI,'Review included data','export must present a reviewed preview');
assertContains(qaUI,'qaDownload.disabled=true','ZIP must remain disabled until review');
assertContains(qaUI,'"X-OnePane-CSRF":csrfCookie()','QA bundle request must send CSRF header');
console.log('PASS: Development + Library/Workspace connection UI syntax, permissions and route contracts');
