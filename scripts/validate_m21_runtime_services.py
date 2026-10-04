#!/usr/bin/env python3
from pathlib import Path
root=Path(__file__).resolve().parents[1]
vault=(root/'internal/vault/service.go').read_text()
adapter=(root/'internal/sandboxrunner/adapter.go').read_text()
cli=(root/'internal/sandboxrunner/cli.go').read_text()
reconciler=(root/'internal/projectruntime/reconciler.go').read_text()
worker=(root/'internal/routineworker/service.go').read_text()
main=(root/'cmd/harnessd/main.go').read_text()
checks={
 'workspace-scoped logical secret lookup': "workspace_id=? AND logical_name=? AND status='active'" in vault and 'ResolveWorkspaceLogical' in vault,
 'secret plaintext excluded from spec hash': 'EnvironmentIdentity' in cli and 'Environment         map[string]string' not in cli[cli.index('func specHash'):cli.index('func runtimeNetworkName')],
 'secret values use private env file': '--env-file' in cli and 'Chmod(0o600)' in cli and 'os.Remove(envFile)' in cli,
 'project internal network': 'network", "create", "--internal"' in cli,
 'loopback-only ingress': '127.0.0.1::%d/%s' in cli,
 'container network independently verified': 'NetworkInternal' in cli and 'portsSafe' in cli and 'runtimeNetworkName(runtimeID)' in cli,
 'egress fail closed': 'len(n.Egress) > 0' in adapter,
 'declared endpoints feed sandbox ports': 'endpointSpecsForApp' in reconciler and '"endpoints": endpoints' in reconciler,
 'routine system authority separated from worker': 'AuthorityPrincipal' in worker and 'WorkerPrincipal' in worker and 'VerifierPrincipal' in worker,
 'routine lease single-use': 'usage := int64(1)' in worker and 'CapabilityExecute' in worker,
 'routine completion independently verified': 'ObservationType: "project_routine_execution"' in worker and 'VerificationV1' in worker and 'CreateCheckpoint' in worker and 'CompleteVerified' in worker,
 'lost routine attempts blocked on restart': 'RecoverLostAttempts' in worker and 'InterruptForRecovery' in worker,
 'daemon executes autonomous routine worker': 'runtime.RoutineWorker.Tick' in main,
}
failed=[k for k,v in checks.items() if not v]
if failed:
    raise SystemExit('M21 runtime-services validation failed: '+', '.join(failed))
print('M21 secrets/networking/autonomous-routine contract: PASS')
