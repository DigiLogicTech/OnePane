#!/usr/bin/env node
/* Protective fence during Alpha 2/3 source consolidation.
 * Do not delete historic snapshots, and do not add new shadowed functions.
 * A separate reviewed cleanup can ratchet the legacy threshold downward.
 */
'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const root=path.resolve(__dirname,'..');
const read=p=>fs.readFileSync(path.join(root,p),'utf8');
const foundation=read('internal/webui/static/app-foundation.js');
const html=read('internal/webui/static/index.html');
const code=[
'internal/featurepolicy/policy.go',
'internal/agentrole/types.go',
'internal/deploymentcap/capabilities.go',
'internal/projectworkspace/workspace_links.go',
'internal/projectworkspace/library.go',
'internal/api/project_library.go'
];
for(const file of code)assert.ok(fs.existsSync(path.join(root,file)),`missing recovered/active subsystem: ${file}`);
assert.ok(!fs.existsSync(path.join(root,'internal/teamworker/service.go.orig')),'inactive .orig backup reappeared');
assert.ok(fs.existsSync(path.join(root,'.recovery/alpha3.1/orchestration-foundations.patch')),'historical recovery bundle deleted without archival review');
assert.ok(!/src=["']\/?\.recovery|href=["']\/?\.recovery/.test(html),'historical recovery patches cannot be loaded into the UI');
const definitions=[...foundation.matchAll(/\bfunction\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(/g)].map(m=>m[1]);
const counts=new Map();for(const d of definitions)counts.set(d,(counts.get(d)||0)+1);
const duplicates=[...counts.entries()].filter(([,n])=>n>1);
assert.ok(duplicates.length<=30,`new duplicate frontend globals added: ${JSON.stringify(duplicates)}`);
const workspaceJS=read('internal/webui/static/project-development-page.js');
const collab=read('internal/webui/static/workspace-collaboration.js');
const library=read('internal/webui/static/workspace-library.js');
assert.ok(workspaceJS.includes('a45MountCollaboration')&&workspaceJS.includes('a46MountWorkspaceLibrary'),'one Workspace renderer should own module mounts');
assert.ok(!collab.includes('a44LoadProjectEnvironment=')&&!library.includes('a44LoadProjectEnvironment='),'new UI module reintroduced wrapper override');
console.log(`PASS: legacy recovery intact; restored policy/agent/capability modules present; obsolete backup removed; existing shadowed functions ${duplicates.length} (maximum 30)`);
