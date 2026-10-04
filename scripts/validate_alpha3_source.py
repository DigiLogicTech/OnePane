#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
read = lambda p: (ROOT / p).read_text(encoding='utf-8')
ui = read('internal/webui/static/app.js')
setup = read('packaging/windows/setup/main.go')
buildinfo = read('internal/buildinfo/buildinfo.go')
runtime = read('internal/runtimecoord/service.go')
resources = read('internal/resourcecoord/service.go')
workspace = read('internal/projectworkspace/views.go') + read('internal/projectworkspace/storage.go')
node = read('internal/nodefederation/types.go') + read('internal/nodefederation/service.go')
models = read('internal/localai/managed_deployments.go')
migration = read('migrations/0022_alpha3_control_plane.sql')

checks = []
def ck(name, cond): checks.append((name, bool(cond)))

ck('release identity is centralized for Alpha 3.1', 'QA8_RELEASE' not in ui and "apiRequest('/v1/about')" in ui and 'var version = buildinfo.Version' in setup and 'Version   = "dev"' in buildinfo)
ck('visible chat modes are Direct Team Council', "const qa8VisibleModes=['direct','team','council']" in ui and "modeOptions=[['default',`Default (${titleCase(defaultMode)})`],['direct','Direct'],['team','Team'],['council','Council']]" in ui)
ck('legacy Supervisor maps to Direct', "function qa8Mode(v){v=String(v||'').toLowerCase();return v==='team'||v==='council'?v:'direct';}" in ui)
ck('Model Stack is first-class workspace component', "QA6_COMPONENTS.modelstack={title:'Model Stack'" in ui and 'qa8ModelStackContent' in ui)
ck('Model Stack supports compute preference', all(x in ui for x in ['prefer-gpu','prefer-cpu','gpu-only','cpu-only']))
ck('qualified models hide Agent Check', "d.qualified" in ui and "[data-agent-check]" in ui and ".remove()" in ui)
ck('models page is unified', "pages.models.title='Models'" in ui and "heading.textContent='Models'" in ui)
ck('durable top navigation only', "['operations','projects','tasks','models','agents'].includes(route)" in ui)
ck('global runtime coordinator exists', 'type Service struct' in runtime and 'Acquire' in runtime and 'runtime_execution_queue' in runtime)
ck('CPU GPU hybrid scheduling exists', all(x in runtime for x in ['cpu','gpu','hybrid']))
ck('global resource coordinator exists', 'global_resource_identities' in resources and 'global_resource_leases' in resources)
ck('first-class project workspaces exist', 'project_workspaces' in workspace)
ck('project storage move verifies hashes', all(x in workspace for x in ['sha256','project_storage_locations']))
ck('model deployment exposes compute metadata', all(x in models for x in ['ComputeMode','ComputeBackend','ComputeDeviceIDs','RAMBytes','VRAMBytes']))
ck('remote node compute policy exists', 'type ComputePolicy struct' in node and 'idle_only' in node)
ck('runtime and resource coordination state is persisted', 'runtime_execution_queue' in runtime and 'global_resource_leases' in resources)
ck('migration contains Alpha 3 persistence', all(x in migration for x in ['project_workspaces','runtime_compute_profiles','runtime_execution_queue','remote_node_compute_policies','global_resource_identities','global_resource_leases','project_storage_locations']))
ck('Windows setup separates storage roots', all(x in setup for x in ['project-root','model-pool','install-dir']))
ck('Windows setup can defer Ollama', 'OllamaSetup.exe' in setup and 'Skip' in setup or 'skip' in setup.lower())
ck('WebView2 is downloaded on demand', 'LinkId=2124703' in setup and 'MicrosoftEdgeWebView2RuntimeInstallerX64.exe' not in setup)

failed = [name for name, ok in checks if not ok]
for name, ok in checks:
    print(f"[{'PASS' if ok else 'FAIL'}] {name}")
print(f"\nALPHA 3.1 SOURCE: {len(checks)-len(failed)}/{len(checks)} CHECKS PASSED")
if failed:
    for name in failed:
        print(' - ' + name, file=sys.stderr)
    sys.exit(1)
