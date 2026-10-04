# M34 Remote model deployment and residency management

M34 extends M33 federation so a paired OnePane node can be used as a managed
model host, not just as already-provisioned inference capacity.

## Trust boundary

Node pairing grants inference federation only. Remote software/model management
is separately disabled by default and must be enabled on the target node for a
specific paired peer. The target node remains authoritative for its hardware,
trusted signed Local AI catalogue, model pool, install jobs and qualification.
The origin may request a model/quantization, but the target recomputes fit and
rejects anything that is not recommended for its current hardware or cannot be
resolved from its trusted catalogue.

Remote model management never transfers cloud-provider credentials, Vault
secrets, Task leases or ToolGateway authority.

## Model pool and residency

Managed model bytes live in `local_ai.model_pool_path` when configured, otherwise
under `<storage.data_dir>/models/managed`. This can be a large local SSD or a
mounted NAS/share. The pool is persistent storage; VRAM/RAM is an execution cache.

Qualified llama.cpp deployments remain lazy-loadable. A routed local or federated
request starts a stopped runtime on demand. Active requests are reference-counted
and mark the runtime busy so it cannot be reaped or pressure-evicted mid-call.

Before starting a GPU-capable model, the residency manager compares its admitted
fit-plan memory estimate with the node's latest hardware profile and configured
`local_ai.residency_headroom_pct`. If the requested model does not fit alongside
currently resident managed models, OnePane stops least-recently-used healthy/idle
runtimes until it fits. Busy runtimes are never eviction candidates. The existing
idle reaper continues to unload unused models after `idle_unload_minutes`.

Because federated inference terminates at the remote node's normal local inference
transport, the same hot-swap policy applies to remote nodes automatically.

## Remote install flow

1. Pair nodes through M33.
2. On the target node, enable remote model management for the specific peer.
3. Origin requests recommendations for coding/reasoning/chat/etc.
4. Target detects its own hardware and computes recommendations locally.
5. Origin requests one model + quantization.
6. Target revalidates fit, resolves only its signed catalogue artifacts, creates a
   local durable install job, downloads/verifies the runtime/model and qualifies it.
7. Once READY, the normal M33 capability heartbeat advertises the new deployment
   to peers and the ordinary scheduler can route work to it.

## Operator API

```text
GET  /v1/nodes/{nodeID}/model-management        # inbound grant: allow this peer to manage this node
POST /v1/nodes/{nodeID}/model-management       # set inbound grant locally
GET  /v1/nodes/{nodeID}/remote-model-management # query whether target allows this node
POST /v1/nodes/{nodeID}/models/recommendations
POST /v1/nodes/{nodeID}/models/install
GET  /v1/nodes/{nodeID}/model-install-jobs/{jobID}
```

The target-side federation endpoints are mTLS-only. Grant direction is explicit: the local inbound grant means “allow this peer to manage models on this node”; the remote-status endpoint lets an origin determine whether the target has granted it access before enabling install controls.

The target-side federation endpoints are mTLS-only and install-job status is
bound to the peer that created the remote job.
