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
const vm=require('node:vm');
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
/* Palette contrast is enforced on the existing seven explicit dark themes.
   Custom themes and the system/light OS branches still need visual QA. */
function luminance(hex){
 const channels=hex.slice(1).match(/.{2}/g).map(part=>parseInt(part,16)/255);
 const linear=channels.map(value=>value<=0.04045?value/12.92:((value+0.055)/1.055)**2.4);
 return 0.2126*linear[0]+0.7152*linear[1]+0.0722*linear[2];
}
function contrast(hexA,hexB){
 const a=luminance(hexA),b=luminance(hexB);
 return (Math.max(a,b)+0.05)/(Math.min(a,b)+0.05);
}
for(const theme of ['dark','graphite','midnight','ocean','forest','violet','ember']){
 const block=shared.match(new RegExp(':root\\[data-theme="'+theme+'"\\]\\{([^}]+)\\}'));
 assert(block,'theme tokens unavailable for '+theme);
 const fields=Object.fromEntries(
  [...block[1].matchAll(/--([a-z-]+):\s*(#[a-f0-9]{6})/gi)].map(m=>[m[1],m[2]]));
 const contrastBody=contrast(fields.text,fields.panel);
 const contrastButton=contrast(fields.accent,fields['on-accent']);
 assert(contrastBody>=4.5,theme+' panel body text contrast below WCAG AA');
 assert(contrastButton>=4.5,theme+' primary action text contrast below WCAG AA');
}

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
assert(/@media\s*\(\s*prefers-reduced-motion\s*:\s*reduce\s*\)/.test(shared),
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

// Run the actual final navigation decorator with both desktop and mobile
// buttons. Source-marker checks missed querySelector(...).forEach(), which
// threw during the first-run language Continue action before setup advanced.
const navDecorator=workspace.slice(workspace.indexOf('const a35RenderNavBase=renderNav;'),workspace.indexOf('const a35ActivateTabBase=activateTab;'));
assert(navDecorator,'final navigation decorator missing');
const button=active=>({classList:{contains:()=>active},attributes:{},setAttribute(name,value){this.attributes[name]=value},removeAttribute(name){delete this.attributes[name]}});
const desktopButtons=[button(true),button(false)];
const mobileButtons=[button(false),button(true)];
const navRoot={querySelector:()=>null},mobileRoot={};
const sandbox={renderNav(){},currentTab:()=>({route:'operations'}),qa4ProjectHub:{},
 $:(selector)=>selector==='#primaryNav'?navRoot:selector==='#mobileNav'?mobileRoot:null,
 $$:(selector,root)=>selector==='[data-a31-project-nav]'?[]:root===navRoot?desktopButtons:root===mobileRoot?mobileButtons:[]};
vm.runInNewContext(navDecorator+'\nrenderNav();',sandbox,{timeout:1000});
assert.equal(desktopButtons[0].attributes['aria-current'],'page');
assert.equal(desktopButtons[1].attributes['aria-current'],undefined);
assert.equal(mobileButtons[0].attributes['aria-current'],undefined);
assert.equal(mobileButtons[1].attributes['aria-current'],'page');
const projectTaskHelper=foundation.slice(foundation.indexOf('function qa6ProjectTasks('),foundation.indexOf('function qa6ActiveTask('));
const scopedTask={id:'own',project_id:'project-a'};
const taskSandbox={liveOps:{tasks:[{id:'global'},scopedTask,{id:'other',project_id:'project-b'}]}};
vm.runInNewContext(projectTaskHelper+'\nthis.selected=qa6ProjectTasks({id:"project-a"});this.unscoped=qa6ProjectTasks(null);',taskSandbox);
assert.equal(taskSandbox.selected.length,1,'Project surfaces must exclude global and other-Project Tasks');
assert.equal(taskSandbox.selected[0],scopedTask);
assert.equal(taskSandbox.unscoped.length,0);
const migration=foundation.slice(foundation.indexOf('const QA4_ROUTE_ALIASES='),foundation.indexOf('let qa4ProjectHub='));
const migrationState={tabs:[{id:'workspace-tab',route:'workspaces',title:'Workspaces'}],activeTab:'workspace-tab'};
for(let reload=0;reload<2;reload++){
 vm.runInNewContext(migration,{state:migrationState,navItems:[],pages:{projects:{},models:{}},persist(){}},{timeout:1000});
 assert.equal(migrationState.tabs[0].route,'workspaces','Reload must preserve the dedicated Workspace route');
 assert.equal(migrationState.activeTab,'workspace-tab');
}
const themeBuilder=read('internal/webui/static/theme-builder.js');
const hexHelper=themeBuilder.slice(themeBuilder.indexOf('function qa8HexColor('),themeBuilder.indexOf('function qa8ThemeSeed('));
assert.equal(vm.runInNewContext(hexHelper+'\nqa8HexColor("rgba(103, 170, 255, 0.14)", "#000000", "#141c27")'), '#203045', 'custom theme must composite translucent highlights instead of making accent text invisible');
assert.equal(vm.runInNewContext(hexHelper+'\nqa8HexColor("#abc", "#000000")'), '#aabbcc');
assert.equal(vm.runInNewContext(hexHelper+'\nqa8HexColor("rgba(10,20,30,0)", "#000000", "#141c27")'), '#141c27');
for(const name of fs.readdirSync(path.join(root,'internal/webui/static')).filter(name=>name.endsWith('.js'))){
 const source=read('internal/webui/static/'+name);
 assert(!/(?<!\$)\$\((?:'[^']*'|"[^"]*")[^)]*\)\s*\.(?:forEach|filter|map|find|some|every)\b/.test(source),name+': single-element selector used as a collection');
}

console.log('RC11 shared UX alignment: tokens, precedence, responsive shell and scope contract PASS');
