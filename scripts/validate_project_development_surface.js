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
assertContains(ui,'Current architecture: shared Project sandbox','no false Workspace-level runtime-isolation claim');
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
console.log('PASS: Workspace Development UI syntax, router order, runtime-state integrity, theme tokens and isolation disclaimers');
