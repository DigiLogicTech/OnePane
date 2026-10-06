#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
read=lambda p:(ROOT/p).read_text(encoding="utf-8")
ui=read("internal/webui/static/app-foundation.js")+"\n"+read("internal/webui/static/app.js")
setup=read("packaging/windows/setup/main.go")
buildinfo=read("internal/buildinfo/buildinfo.go")
runtime=read("internal/runtimecoord/service.go")
resources=read("internal/resourcecoord/service.go")
workspace=read("internal/projectworkspace/views.go")+read("internal/projectworkspace/storage.go")
node=read("internal/nodefederation/types.go")+read("internal/nodefederation/service.go")
models=read("internal/localai/managed_deployments.go")
compute=read("internal/localai/compute_policy.go")
catalog=read("internal/localai/catalog_bundled.go")
migration=read("migrations/0022_alpha3_control_plane.sql")+read("migrations/0025_alpha31_refinement.sql")
checks=[]
def ck(n,c): checks.append((n,bool(c)))
ck("release identity centralized", "apiRequest('/v1/about')" in ui and 'var version = buildinfo.Version' in setup and 'Version   = "dev"' in buildinfo)
ck("Direct Team Council remain execution modes", all(x in ui for x in ["Direct","Team","Council"]) and "research_mode" in ui)
ck("Nodes and Skills are first-class navigation", '["nodes","⬡","Nodes"]' in ui and '["skills","✦","Skills"]' in ui)
ck("Project workspaces remain durable", "project_workspaces" in workspace and "project_storage_locations" in workspace)
ck("runtime coordinator persists scheduling", "runtime_execution_queue" in runtime and all(x in runtime for x in ["cpu","gpu","hybrid"]))
ck("resource coordinator persists leases", "global_resource_identities" in resources and "global_resource_leases" in resources)
ck("remote node compute policy exists", "type ComputePolicy struct" in node and "idle_only" in node)
ck("managed deployments expose compute metadata", all(x in models for x in ["ComputeMode","ComputeBackend","ComputeDeviceIDs","RAMBytes","VRAMBytes"]))
ck("per-deployment placement policy is durable", "deployment_compute_policies" in migration and "SetComputePolicy" in compute and all(x in compute for x in ["require_gpu","require_cpu","hybrid"]))
ck("trusted local AI bootstrap exists", "BundledArtifactCatalog" in catalog and "llamacpp" in catalog and all(x in catalog for x in ['Backend: "cpu"','Backend: "cuda"','Backend: "vulkan"']))
ck("Windows setup separates storage roots", all(x in setup for x in ["project-root","model-pool","install-dir"]))
ck("WebView2 remains on-demand", "LinkId=2124703" in setup and "MicrosoftEdgeWebView2RuntimeInstallerX64.exe" not in setup)
failed=[n for n,o in checks if not o]
for n,o in checks: print(f"[{'PASS' if o else 'FAIL'}] {n}")
if failed:
 print(f"\nALPHA 3.1 SOURCE: {len(failed)} CHECK(S) FAILED",file=sys.stderr)
 for n in failed: print(" - "+n,file=sys.stderr)
 sys.exit(1)
print(f"\nALPHA 3.1 SOURCE: ALL {len(checks)} CHECKS PASSED")
