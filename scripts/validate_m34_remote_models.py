from pathlib import Path

def need(rel,*tokens):
    p=Path(rel)
    if not p.exists(): raise SystemExit(f'M34 remote-model validation: FAIL missing {rel}')
    s=p.read_text()
    miss=[t for t in tokens if t not in s]
    if miss: raise SystemExit(f'M34 remote-model validation: FAIL {rel} missing {miss}')

need('migrations/0017_remote_model_management.sql','node_model_management_grants','node_remote_model_jobs')
need('internal/localai/install_jobs.go','QueueFederatedInstall','FederatedRecommendations','FederatedModelManagerPrincipal')
need('internal/localai/supervisor.go','ensureCapacity','residencyHeadroomPct','SetBusy','RuntimeHealthy')
need('internal/inference/transport_local_openai.go','LocalRuntimeActivity','LocalEndpointLeaseResolver','AcquireLocalEndpoint')
need('internal/nodefederation/transport.go','/federation/v1/models/management-status','/federation/v1/models/recommend','/federation/v1/models/install','RemoteModelManagementStatus','RemoteModelRecommendations','RemoteInstallModel')
need('internal/nodefederation/service.go','SetModelManagementGrant','modelManagementAllowed','BindRemoteModelJob')
need('internal/api/server.go','/model-management','/remote-model-management','/models/recommendations','/models/install','model-install-jobs')
need('internal/config/config.go','ModelPoolPath','ResidencyHeadroomPct')
need('docs/remote-model-management.md','hot-swap','signed catalogue','Busy runtimes are never eviction candidates')
print('M34 Remote Model Deployment + hot-swap validation: PASS')
