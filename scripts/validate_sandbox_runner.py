from pathlib import Path

root = Path(__file__).resolve().parents[1]
cli = (root/'internal/sandboxrunner/cli.go').read_text()
adapter = (root/'internal/sandboxrunner/adapter.go').read_text()
types = (root/'internal/sandboxrunner/types.go').read_text()
gateway = (root/'internal/tool/gateway.go').read_text()
project = (root/'internal/projectruntime/reconciler.go').read_text()
routine = (root/'internal/projectroutine/executor.go').read_text()

checks = {
    'rootless engine required': 'Rootless' in cli and 'ErrRootlessRequired' in adapter,
    'read only rootfs': '"--read-only"' in cli,
    'all capabilities dropped': '"--cap-drop=ALL"' in cli,
    'no new privileges': '"--security-opt=no-new-privileges"' in cli,
    'dedicated internal network': '"network", "create", "--internal"' in cli and 'runtimeNetworkName' in cli,
    'loopback-only published ingress': '127.0.0.1::%d/%s' in cli,
    'egress remains denied': 'len(n.Egress) > 0' in adapter,
    'no implicit pull': '"--pull=never"' in cli,
    'managed workspace only': 'filepath.Join(a.dataDir, "projects", in.RuntimeID, "workspace")' in adapter,
    'inspect tools observe only': 'ToolRuntimeInspect' in adapter and 'Mode: authority.ActionObserve' in adapter,
    'isolation independently verified': 'IsolationVerified' in types and 'st.IsolationVerified =' in cli,
    'sandbox exec tool exists': 'ToolAppExec' in types and 'ActionExecuteSandboxed' in adapter,
    'sandbox adapter allowlist': 'EnableSandboxAdapter' in gateway and 'sandboxAdapters[key]' in gateway,
    'project mutation serialized by runtime': 'ResourceRef: "project_runtime:" + r.ID' in project,
    'routine requires task and lease': 'TaskID' in routine and 'CapabilityLeaseID' in routine and 'ToolAppExec' in routine,
}
failed = [k for k,v in checks.items() if not v]
if failed:
    raise SystemExit('sandbox-runner validation failed: ' + ', '.join(failed))
print('sandbox-runner/project-runtime security contract: PASS')
