#!/usr/bin/env node
'use strict';
// Guard/provenance source tests only: no package process or installer executes.
const assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {preflight,build,VERSION,CONFIRMATION}=require('./build-rc11-ubuntu-qa');
const temporary=fs.mkdtempSync(path.join(os.tmpdir(),'onepane-ubuntu-guard-'));
function removeFixture(target){
 const absolute=path.resolve(target),scope=path.resolve(temporary),relative=path.relative(scope,absolute);
 assert.ok(absolute===scope||(!relative.startsWith('..'+path.sep)&&relative!=='..'&&!path.isAbsolute(relative)),'Recursive fixture deletion must stay in the private temporary directory');
 fs.rmSync(absolute,{recursive:true,force:true});
}
const root=path.join(temporary,'source'),runner=path.join(temporary,'runner');
fs.mkdirSync(root);fs.mkdirSync(runner);
const sha='a'.repeat(40),env={GITHUB_ACTIONS:'true',GITHUB_EVENT_NAME:'workflow_dispatch',GITHUB_REPOSITORY:'DigiLogicTech/OnePane',GITHUB_SHA:sha,GITHUB_WORKFLOW_SHA:sha,GITHUB_WORKFLOW_REF:'DigiLogicTech/OnePane/.github/workflows/rc11-ubuntu-qa.yml@refs/heads/reviewed',ONEPANE_EXPECTED_SHA:sha,ONEPANE_OPERATOR_CONFIRMATION:CONFIRMATION,ONEPANE_UBUNTU_QA_REVIEWED_SHA:sha,ONEPANE_UBUNTU_QA_APPROVAL_SHA:sha,ONEPANE_WINDOWS_QA_ACCEPTED_SHA:sha,ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE:'review://final-code-security-ui',ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE:'acceptance://windows-installed',RUNNER_TEMP:runner};
let calls=[];
function git(name,args){calls.push([name,...args]);assert.equal(name,'git','Denied packaging must never execute a build tool');if(args[0]==='status')return '';if(args[1]==='HEAD')return sha;if(args[1]==='--show-toplevel')return root;throw Error('Unexpected command')}
function denied(changes,pattern,options={}){calls=[];assert.throws(()=>preflight({root,env:{...env,...changes},platform:'linux',run:git,...options}),pattern);assert.equal(fs.readdirSync(runner).length,0)}
let count=0;
try{
 for(const [changes,pattern] of [
  [{GITHUB_ACTIONS:'false'},/manual GitHub/],[{GITHUB_EVENT_NAME:'pull_request'},/manual GitHub/],
  [{ONEPANE_OPERATOR_CONFIRMATION:''},/confirmation/],[{ONEPANE_EXPECTED_SHA:'main'},/full exact/],
  [{GITHUB_SHA:'b'.repeat(40)},/full exact/],[{ONEPANE_UBUNTU_QA_REVIEWED_SHA:''},/declarations/],
  [{ONEPANE_UBUNTU_QA_APPROVAL_SHA:'b'.repeat(40)},/declarations/],[{ONEPANE_WINDOWS_QA_ACCEPTED_SHA:''},/declarations/],
  [{ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE:''},/references/],[{ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE:'x\ny'},/references/],
  [{ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE:'x\u007fy'},/references/],[{ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE:' '+ 'x'},/references/],
  [{ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE:'x'.repeat(1025)},/references/],
  [{GITHUB_WORKFLOW_SHA:'b'.repeat(40)},/workflow identity/],[{GITHUB_WORKFLOW_REF:'DigiLogicTech/OnePane/.github/workflows/ubuntu-alpha3.yml@refs/heads/main'},/workflow identity/],[{GITHUB_REPOSITORY:'other/OnePane'},/repository/],[{ONEPANE_ARCH:'arm64'},/architecture/],
  [{ONEPANE_VERSION:'1.0.0'},/version/],[{RUNNER_TEMP:root},/outside/],[{RUNNER_TEMP:''},/temporary/]
 ]){denied(changes,pattern);count++}
 denied({},/Native Linux/,{platform:'win32'});count++;
 denied({},/Checked-out/,{run:(name,args)=>args[1]==='HEAD'?'b'.repeat(40):git(name,args)});count++;
 denied({},/modified/,{run:(name,args)=>args[0]==='status'?' M go.mod':git(name,args)});count++;
 const accepted=preflight({root,env,platform:'linux',run:git});assert.equal(accepted.sha,sha);assert.equal(accepted.output,path.join(runner,'onepane-ubuntu-rc11-qa'));count++;
 fs.mkdirSync(accepted.output);assert.throws(()=>preflight({root,env,platform:'linux',run:git}),/already exists/);fs.rmdirSync(accepted.output);count++;
 assert.throws(()=>build({root,env,platform:'linux',run:git}),/integration CI/);count++;
 const ci={...env,ONEPANE_SOURCE_CI_SHA:sha,ONEPANE_SOURCE_CI_STATUS:'completed',ONEPANE_SOURCE_CI_CONCLUSION:'success',ONEPANE_SOURCE_CI_RUN_ID:'123',GITHUB_RUN_ID:'456',GITHUB_RUN_ATTEMPT:'1'};
 for(const changes of [{ONEPANE_SOURCE_CI_SHA:'b'.repeat(40)},{ONEPANE_SOURCE_CI_STATUS:'in_progress'},{ONEPANE_SOURCE_CI_CONCLUSION:'failure'},{ONEPANE_SOURCE_CI_CONCLUSION:'cancelled'},{ONEPANE_SOURCE_CI_RUN_ID:'0'},{GITHUB_RUN_ATTEMPT:''}]){assert.throws(()=>build({root,env:{...ci,...changes},platform:'linux',run:git}),/integration CI|provenance/);count++}
 assert.equal(fs.readdirSync(runner).length,0);
 const sourceRoot=path.resolve(__dirname,'..');
 const sources=['scripts/build-ubuntu-deb.sh','scripts/build-rc11-ubuntu-qa.js','.github/workflows/rc11-ubuntu-qa.yml','go.mod','go.sum',...['onepane.service','preinst','postinst','prerm','postrm'].map(x=>'packaging/debian/'+x)];
 for(const file of sources){fs.mkdirSync(path.dirname(path.join(root,file)),{recursive:true});fs.copyFileSync(path.join(sourceRoot,file),path.join(root,file))}
 const buildTime='2026-10-25T03:00:00Z';
 function simulated(name,args,buildEnv){
  if(name==='git')return git(name,args);
  if(name==='go'&&args.length===1)return 'go version go1.23.12 linux/amd64';
  if(name==='go')return [`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.Version=${VERSION}`,`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.Revision=${sha}`,`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.BuildTime=${buildTime}`,`vcs.revision=${sha}`,'vcs.modified=false'].join('\n');
  if(name==='bash'){assert.equal(buildEnv.GOWORK,'off');assert.equal(buildEnv.GOFLAGS,'-mod=readonly');assert.equal(buildEnv.ONEPANE_OUT,accepted.output);fs.writeFileSync(path.join(accepted.output,`onepane_${VERSION}_amd64.deb`),'synthetic source-test bytes');return ''}
  assert.equal(name,'dpkg-deb');
  if(args[0]==='--version')return 'Debian dpkg-deb test double';
  if(args[0]==='--field')return {Package:'onepane',Version:VERSION,Architecture:'amd64'}[args[2]];
  const target=args[2];fs.mkdirSync(target,{recursive:true});
  if(args[0]==='--extract'){
   for(const [dest,src] of [['lib/systemd/system/onepane.service','packaging/debian/onepane.service']]){fs.mkdirSync(path.dirname(path.join(target,dest)),{recursive:true});fs.copyFileSync(path.join(root,src),path.join(target,dest))}
   const header=Buffer.alloc(20);Buffer.from([127,69,76,70,2,1]).copy(header);header.writeUInt16LE(62,18);fs.mkdirSync(path.join(target,'usr/bin'),{recursive:true});fs.writeFileSync(path.join(target,'usr/bin/onepane'),header);
  }else{assert.equal(args[0],'--control');for(const file of ['preinst','postinst','prerm','postrm'])fs.copyFileSync(path.join(root,'packaging/debian',file),path.join(target,file))}
  return '';
 }
 const manifest=build({root,env:ci,platform:'linux',run:simulated,now:()=>buildTime});
 assert.equal(manifest.source_commit,sha);assert.equal(manifest.installed_acceptance,'not_performed');assert.equal(manifest.kind,'unsigned_qa_candidate');assert.equal(manifest.packaged_files.length,6);assert.equal(manifest.source_files.length,sources.length);
 const lines=fs.readFileSync(path.join(accepted.output,'SHA256SUMS'),'utf8').trim().split('\n');assert.equal(lines.length,3);
 for(const line of lines){const [hash,name]=line.split('  ');assert.equal(hash,crypto.createHash('sha256').update(fs.readFileSync(path.join(accepted.output,name))).digest('hex'))}
 const persisted=JSON.parse(fs.readFileSync(path.join(accepted.output,'candidate-manifest.json'),'utf8'));assert.deepEqual(persisted,manifest);assert.match(fs.readFileSync(path.join(accepted.output,'QA-README.txt'),'utf8'),/not been installed/);count++;
 for(const [kind,pattern] of [['wrong-architecture',/package identity/],['wrong-binary-provenance',/build metadata/],['wrong-ELF',/ELF64/],['changed-hook',/lifecycle hook/],['source-drift',/Source changed/]]){
  removeFixture(accepted.output);let built=false;
  function adversarial(name,args,buildEnv){
   if(kind==='wrong-architecture'&&name==='dpkg-deb'&&args[0]==='--field'&&args[2]==='Architecture')return 'arm64';
   if(kind==='wrong-binary-provenance'&&name==='go'&&args.length>1)return 'unreviewed binary';
   if(kind==='source-drift'&&built&&name==='git'&&args[0]==='status')return ' M source';
   const result=simulated(name,args,buildEnv);
   if(name==='bash')built=true;
   if(kind==='wrong-ELF'&&name==='dpkg-deb'&&args[0]==='--extract')fs.writeFileSync(path.join(args[2],'usr/bin/onepane'),'wrong binary');
   if(kind==='changed-hook'&&name==='dpkg-deb'&&args[0]==='--control')fs.appendFileSync(path.join(args[2],'postinst'),'changed');
   return result;
  }
  assert.throws(()=>build({root,env:ci,platform:'linux',run:adversarial,now:()=>buildTime}),pattern);
  assert.equal(fs.existsSync(path.join(accepted.output,'candidate-manifest.json')),false);count++;
 }
 const workflow=fs.readFileSync(path.join(sourceRoot,'.github/workflows/rc11-ubuntu-qa.yml'),'utf8');
 for(const fence of ['environment: rc11-ubuntu-qa-approval',"github.event_name == 'workflow_dispatch'",'inputs.expected_commit_sha == github.sha',"inputs.operator_confirmation == 'AUTHORIZE_RC11_UBUNTU_QA'",'vars.ONEPANE_UBUNTU_QA_REVIEWED_SHA == github.sha',"workflow_id: 'project-workspace-development-checks.yml'","run.conclusion !== 'success'",'node scripts/build-rc11-ubuntu-qa.js --check-only','node scripts/test_rc11_ubuntu_qa_guard.js'])assert.ok(workflow.includes(fence),fence);
 assert.equal(/sudo|dpkg -i|apt-get|ubuntu-alpha3|alpha3-source/.test(workflow),false);count++;
 console.log(`PASS: ${count} Ubuntu QA guard/provenance source checks; no builder, package tool or installer executed`);
}finally{removeFixture(temporary)}
