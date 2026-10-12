#!/usr/bin/env node
'use strict';

/* RC11 shared UX alignment contract. This tests durable semantics and
   stylesheet precedence without claiming pixel-perfect visual QA. */
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const root=path.resolve(__dirname,'..');
const read=file=>fs.readFileSync(path.join(root,file),'utf8');

const html=read('internal/webui/static/index.html');
const css=read('internal/webui/static/ux-alignment.css');
const shared=read('internal/webui/static/ui-consistency.css');
const foundation=read('internal/webui/static/app-foundation.js');
const canonical=read('internal/webui/static/app.js');
const workspace=read('internal/webui/static/project-workspace-page.js');
const document=read('docs/rc11-product-ux-alignment.md');

const pageCss=['style.css','ui-consistency.css','project-development-page.css',
 'web-chat-page.css','workspace-collaboration.css','workspace-library.css'];
const uxLink='<link rel="stylesheet" href="/ux-alignment.css">';
assert.equal(html.split(uxLink).length,2,'shared design alignment stylesheet must load exactly once');
const after=html.indexOf(uxLink);
for(const file of pageCss){
 const href='href="/'+file+'"';
 assert(html.includes(href),'missing view-specific stylesheet '+file);
 assert(html.indexOf(href)<after,'shared design layer must follow view style '+file);
}
const tokens=['--ux-control-height','--ux-control-radius','--ux-focus-size',
 '--ux-section-gap','--ux-control-gap',
 'var(--text)','var(--muted)','var(--border)','var(--accent)',
 'var(--on-accent)','var(--panel)','var(--panel-2)','var(--focus-ring)'];
for(const token of tokens)assert(css.includes(token),'missing design token '+token);
assert(!/#[0-9a-f]{3,8}\b/i.test(css),'shared alignment layer must not hardcode theme colours');
const primitives=['.page-heading','.page-actions','.toolbar','.panel-card',
 '.page-subtitle','.btn.primary',':focus-visible','@media (max-width: 900px)',
 '@media (max-width: 520px)','@media (prefers-reduced-motion: reduce)'];
for(const p of primitives)assert(css.includes(p),'missing shared UI primitive '+p);
for(const p of ['[data-goal-review-approve]','[data-goal-probe-approve]',
 'data-goal-probe-results','data-goal-review-results']){
 assert(css.includes(p),'Level 5 distinct authority surfaces not aligned '+p);
}
assert(!/height\s*:\s*(?:100vh|100dvh)/.test(css),
 'shared page design may not override Logs/Inspector height ownership');
assert(!/\.main-shell\s*\{/.test(css),
 'UX layer must not change shell grid; that owns drawer resizing');
assert(/@media \(prefers-reduced-motion: reduce\)/.test(shared),
 'existing motion-reduction theme styling must remain');
assert(canonical.includes('parentActive=route===r&&!expandable'),
 'canonical navigation must not double-highlight expandable parents');
assert(workspace.includes("legacyWorkspace.remove()"),
 'nested Projects navigation must not keep the redundant legacy Workspace entry');
assert(workspace.includes("isWorkspace?workspaceID===qa4ProjectHub.activeWorkspaceID:!qa4ProjectHub.activeWorkspaceID"),
 'active Project ancestor must not be highlighted alongside a Workspace child');
for(const marker of ["aria-current","route==='workspaces'&&!activeWorkspaceLeaf",
 "route==='models'&&!activeModelLeaf","[data-mobile-route]"]){
 assert(workspace.includes(marker),'missing nested and accessible navigation invariant '+marker);
}

for(const id of Array.from({length:10},(_,n)=>'UX-'+String(n+1).padStart(2,'0'))){
 assert(document.includes(id),'UI/UX review manifest missing '+id);
}
for(const principle of ['local-first','Level 4','Level 5',
 'Windows/Ubuntu','Inspector','Colibri','OmniRoute']){
 assert(document.toLowerCase().includes(principle.toLowerCase()),
  'OnePane UX vision constraint missing: '+principle);
}
assert(foundation.includes('pageHeader('),'shared page-header component not retained');

console.log('RC11 shared UX alignment: tokens, precedence, responsive shell and scope contract PASS');
