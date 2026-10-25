#!/usr/bin/env node
'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/webui/static/evidence-audit-page.js','utf8');
function deferred(){let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}}
function element(){return {innerHTML:'',textContent:'',value:'',insertAdjacentHTML(where,text){this.innerHTML+=text}}}
function host(){const h={isConnected:true,dataset:{},nodes:{}};Object.defineProperty(h,'innerHTML',{set(value){this.markup=value;this.nodes=Object.fromEntries(['Refresh','Type','Filter','Count','List'].map(id=>['#a51Audit'+id,element()]))},get(){return this.markup}});h.querySelector=selector=>h.nodes[selector];return h}
function fixture(){
 const h=host(),requests=[];let currentHost=h,tab={id:'evidence-a',route:'evidence',state:'active'};
 const context=vm.createContext({onepaneWorkspace:'workspace-a',qa31ViewEpoch:0,Date,Set,JSON,Number,String,Array,
  $:selector=>selector==='#viewHost'?currentHost:null,currentTab:()=>tab,
  escapeHtml:value=>String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;'),pageHeader:()=>'',
  renderNav(){},renderActiveView:async()=>{},apiRequest(url){const request=deferred();requests.push({...request,url});return request.promise}
 });vm.runInContext(source,context);
 return {context,h,requests,render:()=>vm.runInContext('a51RenderAudit()',context),wrapper:()=>vm.runInContext('renderActiveView()',context),rows:()=>vm.runInContext('JSON.stringify(a51AuditRows)',context),scope:()=>vm.runInContext('typeof a51AuditScope==="undefined"?null:a51AuditScope',context),paint:()=>vm.runInContext('a51PaintAudit()',context),workspace:value=>{context.onepaneWorkspace=value},tab:value=>{tab=value},replaceHost:()=>{currentHost=host();return currentHost}};
}
const row=(id,sequence=1)=>({id,sequence,event_type:'task.updated',aggregate_type:'task',aggregate_id:id,payload:{private:id},occurred_at:1});
let count=0;const failures=[];
async function check(name,run){try{await run();count++;console.log('PASS '+name)}catch(error){failures.push(name+': '+error.message);console.error('FAIL '+name)}}
async function main(){
 await check('newer refresh wins over late success in the same Workspace',async()=>{
  const f=fixture(),old=f.render(),fresh=f.render();f.requests[1].resolve([row('new',2)]);await fresh;f.requests[0].resolve([row('old')]);await old;
  assert.match(f.rows(),/new/);assert.doesNotMatch(f.rows(),/old/);assert.match(f.h.nodes['#a51AuditList'].innerHTML,/new/);
 });
 await check('late prior-Workspace result cannot replace the current Workspace ledger',async()=>{
  const f=fixture(),old=f.render();f.workspace('workspace-b');const fresh=f.render();f.requests[1].resolve([row('current-private')]);await fresh;
  f.requests[0].resolve([row('old-private')]);await old;assert.match(f.rows(),/current-private/);assert.doesNotMatch(f.rows(),/old-private/);
  assert.match(f.requests[0].url,/workspace-a/);assert.match(f.requests[1].url,/workspace-b/);
 });
 await check('late old errors cannot clear a newer success',async()=>{
  const f=fixture(),old=f.render(),fresh=f.render();f.requests[1].resolve([row('current')]);await fresh;
  f.requests[0].reject(Error('stale denial'));await old;assert.match(f.h.nodes['#a51AuditList'].innerHTML,/current/);assert.doesNotMatch(f.h.nodes['#a51AuditList'].innerHTML,/stale denial/);
 });
 await check('new load clears private retained rows before API completion',async()=>{
  const f=fixture(),first=f.render();f.requests[0].resolve([row('private')]);await first;const next=f.render();assert.equal(f.rows(),'[]');f.requests[1].reject(Error('denied'));await next;assert.equal(f.rows(),'[]');assert.match(f.h.nodes['#a51AuditList'].innerHTML,/denied/);
 });
 await check('Workspace mutation without replacement render discards returned bytes',async()=>{
  const f=fixture(),load=f.render();f.workspace('workspace-b');f.requests[0].resolve([row('private-old')]);await load;assert.equal(f.rows(),'[]');assert.doesNotMatch(f.h.nodes['#a51AuditList'].innerHTML,/private-old/);
 });
 await check('same-route tab replacement discards returned bytes',async()=>{
  const f=fixture(),load=f.render();f.tab({id:'evidence-b',route:'evidence',state:'active'});f.requests[0].resolve([row('private-tab-a')]);await load;assert.equal(f.rows(),'[]');assert.doesNotMatch(f.h.nodes['#a51AuditList'].innerHTML,/private-tab-a/);
 });
 await check('detached host and replaced host discard returned bytes',async()=>{
  for(const replace of [false,true]){const f=fixture(),load=f.render();if(replace)f.replaceHost();else f.h.isConnected=false;f.requests[0].resolve([row('private-host')]);await load;assert.equal(f.rows(),'[]')}
 });
 await check('same-host page replacement discards returned bytes without writing new controls',async()=>{
  const f=fixture(),load=f.render();f.h.innerHTML='replacement page';const replacement=f.h.nodes['#a51AuditList'];f.requests[0].resolve([row('private-old-view')]);await load;assert.equal(f.rows(),'[]');assert.equal(replacement.innerHTML,'');
 });
 await check('leaving Evidence invalidates in-flight reads and clears private cache',async()=>{
  const f=fixture(),first=f.render();f.requests[0].resolve([row('private')]);await first;f.tab({id:'tasks',route:'tasks'});await f.wrapper();assert.equal(f.rows(),'[]');
  const g=fixture(),pending=g.render();g.tab({id:'tasks',route:'tasks'});await g.wrapper();g.requests[0].reject(Error('old private error'));await pending;assert.equal(g.rows(),'[]');assert.doesNotMatch(g.h.nodes['#a51AuditList'].innerHTML,/old private error/);
 });
 await check('stale filters cannot repaint retained private data after scope change',async()=>{
  const f=fixture(),first=f.render();f.requests[0].resolve([row('private')]);await first;f.workspace('workspace-b');f.paint();assert.equal(f.rows(),'[]');assert.equal(f.h.nodes['#a51AuditList'].innerHTML,'');assert.doesNotMatch(f.h.nodes['#a51AuditType'].innerHTML,/task.updated/);
 });
 await check('empty Workspace issues no request and retains no private rows',async()=>{
  const f=fixture(),first=f.render();f.requests[0].resolve([row('private')]);await first;f.workspace('');await f.render();assert.equal(f.rows(),'[]');assert.equal(f.requests.length,1);
 });
 await check('escaping filtering sorting and 100-row bound remain functional',async()=>{
  const f=fixture(),load=f.render();f.requests[0].resolve(Array.from({length:110},(_,i)=>row(i===0?'<script>private</script>':'event-'+i,i)));await load;
  assert.equal(JSON.parse(f.rows()).length,100);assert.equal(JSON.parse(f.rows())[0].sequence,99);assert.match(f.h.nodes['#a51AuditList'].innerHTML,/&lt;script&gt;/);assert.doesNotMatch(f.h.nodes['#a51AuditList'].innerHTML,/<script>/);
  f.h.nodes['#a51AuditFilter'].value='event-99';f.paint();assert.match(f.h.nodes['#a51AuditCount'].textContent,/1 of 100/);assert.doesNotMatch(f.h.nodes['#a51AuditList'].innerHTML,/event-98/);
 });
 if(failures.length)throw Error(failures.join('\n'));
 console.log(`PASS: ${count} actual Evidence controller scope/lifetime/privacy checks`);
}
main().catch(error=>{console.error(error);process.exitCode=1});
