#!/usr/bin/env node
// Current-source QA packaging only. Requiring this module performs no work.
'use strict';
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),cp=require('node:child_process');
const VERSION='0.1.0-alpha.3.3-rc.11';
const CONFIRMATION='AUTHORIZE_RC11_UBUNTU_QA';
const SHA=/^[a-f0-9]{40}$/;
const fail=message=>{throw Error(message)};
function command(root,env,name,args){return cp.execFileSync(name,args,{cwd:root,env,encoding:'utf8',stdio:['ignore','pipe','pipe'],maxBuffer:8<<20}).trim()}
function reference(value){return typeof value==='string'&&value.trim()===value&&value.length>0&&value.length<=1024&&!/[\u0000-\u001f\u007f]/.test(value)}
function fileHash(file){const stat=fs.lstatSync(file);if(!stat.isFile()||stat.isSymbolicLink())fail('Expected a regular package/source file');return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')}
function preflight({root,env,platform=process.platform,run=(name,args)=>command(root,env,name,args)}){
 if(env.GITHUB_ACTIONS!=='true'||env.GITHUB_EVENT_NAME!=='workflow_dispatch')fail('Only an authorized manual GitHub workflow may package Ubuntu QA');
 if(env.ONEPANE_OPERATOR_CONFIRMATION!==CONFIRMATION)fail('Explicit Ubuntu QA confirmation is required');
 const sha=env.ONEPANE_EXPECTED_SHA;
 if(!SHA.test(sha||'')||env.GITHUB_SHA!==sha)fail('A full exact workflow source SHA is required');
 if(env.GITHUB_WORKFLOW_SHA!==sha||!/^DigiLogicTech\/OnePane\/\.github\/workflows\/rc11-ubuntu-qa\.yml@refs\/(heads|tags)\/.+$/.test(env.GITHUB_WORKFLOW_REF||''))fail('Reviewed current-source Ubuntu workflow identity is required');
 for(const key of ['ONEPANE_UBUNTU_QA_REVIEWED_SHA','ONEPANE_UBUNTU_QA_APPROVAL_SHA','ONEPANE_WINDOWS_QA_ACCEPTED_SHA'])if(env[key]!==sha)fail('Exact-source review and Windows-first acceptance declarations are required');
 for(const key of ['ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE','ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE'])if(!reference(env[key]))fail('Bounded review and Windows-first acceptance references are required');
 if(env.GITHUB_REPOSITORY!=='DigiLogicTech/OnePane')fail('Unexpected source repository');
 if(platform!=='linux')fail('Native Linux is required for Ubuntu QA packaging');
 if(env.ONEPANE_ARCH&&env.ONEPANE_ARCH!=='amd64')fail('Only the approved amd64 architecture is supported');
 if(env.ONEPANE_VERSION&&env.ONEPANE_VERSION!==VERSION)fail('The approved RC11 QA version must not be overridden');
 const top=run('git',['rev-parse','--show-toplevel']);
 if(fs.realpathSync(top)!==fs.realpathSync(root))fail('Packaging must use the current repository root');
 if(run('git',['rev-parse','HEAD'])!==sha)fail('Checked-out source differs from reviewed SHA');
 if(run('git',['status','--porcelain','--untracked-files=all']))fail('Refusing to package modified or untracked source');
 if(!env.RUNNER_TEMP||!path.isAbsolute(env.RUNNER_TEMP)||!fs.statSync(env.RUNNER_TEMP).isDirectory())fail('Disposable runner temporary directory is required');
 const output=path.join(fs.realpathSync(env.RUNNER_TEMP),'onepane-ubuntu-rc11-qa');
 const relative=path.relative(fs.realpathSync(root),output);
 if(!relative.startsWith('..'+path.sep)&&!path.isAbsolute(relative))fail('Candidate output must be outside the source checkout');
 if(fs.existsSync(output))fail('Candidate output already exists; refusing to overwrite');
 return {sha,output};
}
function build({root,env,platform=process.platform,run=(name,args,extraEnv)=>command(root,extraEnv||env,name,args),now=()=>new Date().toISOString().replace(/\.\d{3}Z$/,'Z')}={}){
 const {sha,output}=preflight({root,env,platform,run});
 if(env.ONEPANE_SOURCE_CI_SHA!==sha||env.ONEPANE_SOURCE_CI_STATUS!=='completed'||env.ONEPANE_SOURCE_CI_CONCLUSION!=='success'||!/^[1-9][0-9]*$/.test(env.ONEPANE_SOURCE_CI_RUN_ID||''))fail('Latest authoritative integration CI must have passed for this exact SHA');
 if(!/^[1-9][0-9]*$/.test(env.GITHUB_RUN_ID||'')||!/^[1-9][0-9]*$/.test(env.GITHUB_RUN_ATTEMPT||''))fail('Workflow provenance is unavailable');
 const buildTime=now(),goVersion=run('go',['version']),dpkgVersion=run('dpkg-deb',['--version']);
 const buildEnv={...env,GOWORK:'off',GOFLAGS:'-mod=readonly',ONEPANE_VERSION:VERSION,ONEPANE_REVISION:sha,ONEPANE_BUILD_TIME:buildTime,ONEPANE_ARCH:'amd64',ONEPANE_OUT:output};
 fs.mkdirSync(output,{recursive:true});
 run('bash',[path.join(root,'scripts','build-ubuntu-deb.sh')],buildEnv);
 const debName=`onepane_${VERSION}_amd64.deb`,deb=path.join(output,debName);
 const debHash=fileHash(deb);
 for(const [field,expected] of [['Package','onepane'],['Version',VERSION],['Architecture','amd64']])if(run('dpkg-deb',['--field',deb,field])!==expected)fail('Debian package identity differs from approved QA identity');
 const payload=path.join(output,'payload-inspection'),control=path.join(output,'control-inspection');
 run('dpkg-deb',['--extract',deb,payload]);run('dpkg-deb',['--control',deb,control]);
 const binary=path.join(payload,'usr','bin','onepane'),header=fs.readFileSync(binary).subarray(0,20);
 if(header.length<20||!header.subarray(0,4).equals(Buffer.from([127,69,76,70]))||header[4]!==2||header[5]!==1||header.readUInt16LE(18)!==62)fail('Package payload is not Linux ELF64 amd64');
 const binaryInfo=run('go',['version','-m',binary]);
 for(const item of [`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.Version=${VERSION}`,`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.Revision=${sha}`,`-X github.com/DigiLogicTech/OnePane/internal/buildinfo.BuildTime=${buildTime}`,`vcs.revision=${sha}`,'vcs.modified=false'])if(!binaryInfo.includes(item))fail('Binary build metadata differs from reviewed current source');
 const packagedFiles=[{name:'usr/bin/onepane',sha256:fileHash(binary),bytes:fs.statSync(binary).size}];
 for(const [relative,source] of [['lib/systemd/system/onepane.service','packaging/debian/onepane.service'],...['preinst','postinst','prerm','postrm'].map(name=>[`DEBIAN/${name}`,`packaging/debian/${name}`])]){
  const actual=relative.startsWith('DEBIAN/')?path.join(control,path.basename(relative)):path.join(payload,relative);
  const hash=fileHash(actual);if(hash!==fileHash(path.join(root,source)))fail('Packaged service or lifecycle hook differs from reviewed source');
  packagedFiles.push({name:relative,sha256:hash,bytes:fs.statSync(actual).size});
 }
 if(run('git',['rev-parse','HEAD'])!==sha||run('git',['status','--porcelain','--untracked-files=all']))fail('Source changed while packaging');
 const sourceFiles=['scripts/build-ubuntu-deb.sh','scripts/build-rc11-ubuntu-qa.js','.github/workflows/rc11-ubuntu-qa.yml','go.mod','go.sum',...['onepane.service','preinst','postinst','prerm','postrm'].map(name=>'packaging/debian/'+name)].map(name=>({name,sha256:fileHash(path.join(root,name))}));
 const manifest={schema_version:1,kind:'unsigned_qa_candidate',version:VERSION,source_commit:sha,build_time_utc:buildTime,platform:'linux-amd64',package:{name:debName,package_name:'onepane',version:VERSION,architecture:'amd64',sha256:debHash,bytes:fs.statSync(deb).size},workflow:{repository:env.GITHUB_REPOSITORY,run_id:env.GITHUB_RUN_ID,attempt:env.GITHUB_RUN_ATTEMPT,url:`https://github.com/${env.GITHUB_REPOSITORY}/actions/runs/${env.GITHUB_RUN_ID}`},integration_ci_run_id:env.ONEPANE_SOURCE_CI_RUN_ID,operator_declarations:{reviewed_sha:env.ONEPANE_UBUNTU_QA_REVIEWED_SHA,approval_sha:env.ONEPANE_UBUNTU_QA_APPROVAL_SHA,approval_reference:env.ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE,windows_accepted_sha:env.ONEPANE_WINDOWS_QA_ACCEPTED_SHA,windows_acceptance_reference:env.ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE},tools:{go:goVersion,dpkg_deb:dpkgVersion},source_files:sourceFiles,packaged_files:packagedFiles,signing_status:'unsigned_qa',installed_acceptance:'not_performed',physical_model_acceptance:'not_established_by_packaging'};
 fs.writeFileSync(path.join(output,'candidate-manifest.json'),JSON.stringify(manifest,null,2)+'\n');
 fs.writeFileSync(path.join(output,'QA-README.txt'),`OnePane RC11 Ubuntu amd64 QA candidate (unsigned)\nVersion: ${VERSION}\nSource: ${sha}\nVerify the .deb SHA-256 against its sidecar, SHA256SUMS and candidate-manifest.json.\nReview and Windows-first references are operator declarations, not independent test evidence.\nThis package has not been installed or qualified by this packaging job.\nUse the approved Ubuntu clean-install/upgrade/rollback/data-preservation checklist.\nBack up existing configuration, models, runtimes, Projects and state before installed QA.\nNo model, Node or Level 5 execution is authorized by packaging.\n`);
 fs.writeFileSync(deb+'.sha256',`${debHash}  ${debName}\n`);
 fs.writeFileSync(path.join(output,'SHA256SUMS'),[debName,'candidate-manifest.json','QA-README.txt'].map(name=>`${fileHash(path.join(output,name))}  ${name}`).join('\n')+'\n');
 return manifest;
}
if(require.main===module){try{const root=path.resolve(__dirname,'..');if(process.argv.length>3||(process.argv[2]&&process.argv[2]!=='--check-only'))fail('Usage: build-rc11-ubuntu-qa.js [--check-only]');if(process.argv[2]==='--check-only')preflight({root,env:process.env});else build({root,env:process.env});console.log('Reviewed Ubuntu QA guard/source checks passed')}catch(error){console.error(error.message);process.exitCode=1}}
module.exports={preflight,build,VERSION,CONFIRMATION};
