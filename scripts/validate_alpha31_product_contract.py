#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding='utf-8')

app=read('internal/webui/static/app.js')
html=read('internal/webui/static/index.html')
css=read('internal/webui/static/style.css')
api=read('internal/api/server.go')+read('internal/api/assistant_orchestrator.go')
boot=read('internal/bootstrap/bootstrap.go')
assistant=read('internal/assistant/service.go')
orch=read('internal/projectorchestrator/service.go')
profiles=read('internal/agentprofile/service.go')
components=read('internal/localai/components.go')
managed=read('internal/localai/managed_deployments.go')
provider=read('internal/provideronboarding/omniroute.go')
agentworker=read('internal/agentworker/execution.go')
teamworker=read('internal/teamworker/service.go')
setup=read('packaging/windows/setup/main.go')
desktop=read('packaging/windows/desktop/main.go')
deb=read('scripts/build-ubuntu-deb.sh')
buildinfo=read('internal/buildinfo/buildinfo.go')
workflow=read('.github/workflows/alpha3.1-stabilization.yml')
m23=read('migrations/0023_alpha31_assistant_orchestrator.sql')
m24=read('migrations/0024_alpha31_agents_components.sql')

checks=[]
def ck(name,cond): checks.append((name,bool(cond)))

# Release identity / packaging
ck('central build identity exists', 'var (' in buildinfo and 'Version   = "dev"' in buildinfo and 'Revision  = "unknown"' in buildinfo)
ck('backend exposes build identity', 'GET /v1/about' in api and 'buildinfo.Version' in api)
ck('Windows setup consumes build identity', 'var version = buildinfo.Version' in setup)
ck('Windows desktop singleton is version-neutral', 'Local\\OnePaneDesktop' in desktop and 'OnePaneDesktop-v0.1.0-alpha.3' not in desktop)
ck('Ubuntu requires authoritative exact version', 'ONEPANE_VERSION is required' in deb and 'DEB_VERSION="${VERSION#v}"' in deb and '~alpha.' not in deb)

# Assistant / Project Orchestrator
ck('Assistant service and durable tables exist', 'type Service struct' in assistant and 'assistant_threads' in m23 and 'assistant_project_handoffs' in m23)
ck('Project Orchestrator service and durable table exist', 'type Service struct' in orch and 'project_orchestrators' in m23)
ck('Assistant routes are exposed', all(x in api for x in ['/v1/assistant/threads','/v1/assistant/threads/{threadID}/turns','/v1/assistant/threads/{threadID}/scope']))
ck('Project Orchestrator routes are exposed', all(x in api for x in ['/v1/projects/{projectID}/orchestrator','/v1/projects/{projectID}/orchestrator/turns','/v1/projects/{projectID}/orchestrator/handoffs']))
ck('Assistant supports explicit Global scope return', 'SetScope' in assistant and 'project_id' in api)
ck('Assistant and Orchestrator reason through model deployments only', 'AllowedKinds:[]scheduler.CandidateKind{scheduler.CandidateModel}' in assistant and 'AllowedKinds:[]scheduler.CandidateKind{scheduler.CandidateModel}' in orch)
ck('task creation is explicitly permission gated', 'AllowTaskCreation' in assistant and 'AllowTaskCreation' in orch and 'task.write' in api)
ck('new services are wired by bootstrap', all(x in boot for x in ['Assistant           *assistant.Service','ProjectOrchestrator *projectorchestrator.Service','AgentProfiles       *agentprofile.Service']))

# Agents / modes
ck('Agent Profiles are durable', 'agent_profiles' in m24 and 'onepane-default' in profiles and 'agent.md' in profiles)
ck('real Agent Sessions projection exists', 'ListSessions' in profiles and 'task_execution_profiles' in profiles)
ck('Profiles affect Direct execution without authority', 'Kind:"agent_profile"' in agentworker and 'Authoritative:false' in agentworker and 'grant no capabilities or permissions' in agentworker)
ck('Profiles affect Team/Council seats without authority', 'Kind:"agent_profile"' in teamworker and 'Authoritative:false' in teamworker and 'grant no capabilities or permissions' in teamworker)
ck('visible modes are Direct Team Council', "const qa8VisibleModes=['direct','team','council']" in app)
ck('new Workspace defaults expose only Direct Team Council', "['direct','team','council']" in app and 'Default Team/Council seats' in app)
ck('Agents active surface is Profiles Sessions Teams Councils', '/* === Alpha 3.1 authoritative Agents surface === */' in app and all(x in app for x in ["['profiles','Profiles']","['sessions','Sessions']","['teams','Teams']","['councils','Councils']"]))
ck('seeded demo Agent sessions are removed', 'chat-demo-1' not in app and 'Auth review' not in app and 'Code review' not in app)
ck('retired Bot UI terminology is removed', 'Bot Runtime' not in app and 'Provider Bots' not in app)
ck('no dormant top-level Defaults renderer remains', "pageHeader('Defaults'" not in app)

# Models / runtimes
ck('Colibri has durable lifecycle jobs', 'managed_component_jobs' in m24 and 'RequestComponentAction' in components and 'RecoverManagedComponents' in components)
ck('Colibri lifecycle exposes recovery actions', all(('case "'+x+'"') in components or ('"'+x+'"') in components for x in ['update','repair','retry','resume','remove']))
ck('Colibri has Windows and Linux manifests', all(x in components for x in ['windows-x86_64.zip','linux-x86_64.tar.gz','1d0cc6760a6e53fcafe77376ca20ec254c6ff8995e55fba1bcd883278787f794','9d5afd587d7c429b2cf0ce0c520d42f4c2883bdc9c560ed9fb5afa0b2c2e0d12']))
ck('Colibri model registration is platform-aware', 'colibriArtifact()' in managed and 'artifact.EngineRel' in managed)
ck('OmniRoute is not a managed local component', '"omniroute"' not in components.lower() and 'ProbeOmniRoute' in provider and 'AddOmniRoute' in provider)
ck('Models active UI distinguishes Colibri and OmniRoute', 'Alpha 3.1 Models lifecycle and Operations actions' in app and 'Provider connection lifecycle' in app)

# Settings / UI / tour
ck('top-level shell says Settings', 'data-route="settings">⚙ <span>Settings</span>' in html and 'data-route="settings">⚙ <span>Defaults</span>' not in html)
ck('active Settings has new Workspace Defaults only', 'Defaults for new Workspaces' in app and 'OnePane Assistant defaults' in app)
ck('active Settings About reads /v1/about', "apiRequest('/v1/about')" in app)
ck('unified Ask OnePane surface exists', 'openCommandPalette=qa31OpenAssistant' in app and 'Ask OnePane or run a command' in app)
ck('Project Orchestrator Project strip exists', 'Project Orchestrator Project-level surface' in app and 'qa31ProjectOrchestratorStrip' in app)
ck('Operations card actions are explicit', 'qa31BindOperationCardActions' in app and "dataset.qa31OperationAction" in app)
ck('Tour teaches Assistant and Orchestrator', 'Alpha 3.1 product tour' in app and "title:'OnePane Assistant'" in app and "title:'Project Orchestrator'" in app)
ck('Tour distinguishes Colibri and OmniRoute', "title:'Colibri Large Model'" in app and "title:'OmniRoute'" in app and 'external routing/provider connection' in app)
ck('Tour restores Project and panel context', all(x in app for x in ['projectID:typeof qa4ProjectHub','projectWorkspaceID:typeof qa4ProjectHub','inspectorWidth:state.inspectorWidth','drawerHeight:state.drawerHeight']))
ck('shell polish remains present', 'Alpha 3.1 shell polish: docked panels' in css and '--inspector-open:360px' in css and ':root[data-theme="light"] { --good:#2d78a8; }' in css)
ck('classic-script bootstrap remains enforced', '<script src="/chat-commands.js"></script>' in html and '<script src="/app.js"></script>' in html and 'type="module"' not in html)
ck('native desktop emits UI-ready acknowledgement', 'onepane-ui-ready|' in app and 'ui-ready.txt' in desktop and 'onepane-ui-ready|' in desktop)
ck('Windows setup supports noninteractive installed-product QA', all(x in setup for x in ['--silent','--no-launch','--skip-optional-runtime']))
ck('CI includes Windows installed-product lifecycle', 'windows-installed-smoke:' in workflow and 'Install launch uninstall reinstall' in workflow and 'Assert-NativeUIReady' in workflow)
ck('CI includes Ubuntu installed-package lifecycle', 'ubuntu-installed-smoke:' in workflow and 'systemctl is-active --quiet onepane.service' in workflow and '/var/lib/onepane/qa-preserve.txt' in workflow)

failed=[name for name,ok in checks if not ok]
for name,ok in checks: print(f"[{'PASS' if ok else 'FAIL'}] {name}")
if failed:
    print(f"\nALPHA 3.1 PRODUCT CONTRACT: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
    for name in failed: print(' - '+name,file=sys.stderr)
    sys.exit(1)
print(f"\nALPHA 3.1 PRODUCT CONTRACT: ALL {len(checks)} CHECKS PASSED")
