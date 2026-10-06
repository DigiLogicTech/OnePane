# OnePane v0.1 RC8 audit

Date: 2026-10-02

RC8 continues the reviewed M28 budget/assurance, M29 integrations, M30 messaging gateway, M31 Team Mode, M32 Bot Runtime, M33 paired HarnessNode federation and M34 remote model deployment with M35 backend-neutral multi-device placement, llmfit advisory metadata, model spec sheets and manual Testbed admission. It is suitable for the final connected Linux verification gate; it is not declared a production release until that gate completes successfully.

## Earlier audit fixes retained

- Correct Operation assurance freshness SQL uses the Event Ledger `event_type` column.
- Assurance freshness uses the maximum applicable Task-attempt and Operation-execution causal boundary.
- Observation source provenance is bound to the recording actor when supplied, preventing evidence-source relabelling for V3/V4.
- Pre-dispatch BudgetReservation failures release capacity; critical budget updates validate affected rows.
- Source identity is `github.com/DigiLogicTech/OnePane`, appliance state is `/var/lib/onepane`, and release builds verify pinned modules/checksums.
- M29 retains 27 cloud/provider presets, 33 harness/runtime presets and 20 SaaS connectors with separate Vault namespaces and host-pinned credential destinations.

## M30 messaging-gateway audit

- 32 built-in gateway presets cover the current Hermes messaging surface union, including Discord, Telegram, Slack, Google Chat, WhatsApp/Cloud API, Signal, Matrix, Mattermost, Email/SMTP, SMS/Twilio, Teams, LINE, ntfy, SimpleX, iMessage bridges, QQ/Yuanbao, Feishu/WeCom/Weixin, Open WebUI, Webhooks, Raft, IRC and Buzz.
- Gateway targets provide stable destination identity plus optional thread/topic references; routing is not embedded in prompts.
- Notification rules subscribe to the append-only Event Ledger and materialize idempotent durable deliveries.
- Routine notification bindings fire from verified `task.completed` events associated with the Routine occurrence rather than model completion declarations.
- `gateway.send` is an `EXTERNAL_SEND` ToolGateway capability. Each delivery receives an exact-target, one-use CapabilityLease and executes through OperationCoordinator.
- A successful gateway response establishes only V1 acceptance-receipt evidence; it does not claim human receipt/read status.
- Network-uncertain outcomes are marked `unknown` and are not automatically replayed. A restart while a delivery is `sending` also becomes `unknown` for reconciliation.
- Gateway credentials have an independent `gateway/<platform>/<kind>` Vault namespace and cannot be substituted with provider/plugin/harness credentials.
- Fixed bot/API presets pin credential-bearing requests to canonical hosts. Self-hosted presets require HTTPS except loopback HTTP. Relay mode allows connector-owned platform credentials and normalized `/v1/messages/send` delivery.
- Cross-workspace target and Routine-binding checks use authoritative resource workspace IDs rather than caller-supplied workspace claims.


## M31 Team Mode audit

- Team-mode Tasks are protected by a TaskService admission guard: READY/START fail closed until the linked TeamSession holds a human-accepted TeamPlan.
- The normal AgentWorker excludes unaccepted Team Tasks from automatic CREATED-task admission.
- Reusable Teams contain human/agent/supervisor roles with scheduler role/capability/protocol requirements rather than hard-wiring a role to one model.
- Explicit Team rounds create durable per-member turn requests; each member is routed independently through the existing scheduler and cost/budget policy.
- Deliberation is reasoning-only. Tool/delegate proposals are omitted from the permitted Agent Protocol set, so pre-task discussion cannot acquire side-effect authority.
- Human messages are USER_INSTRUCTION while agent/model contributions remain UNVERIFIED_DERIVED. The exact accepted TeamPlan alone becomes required AUTHORITATIVE_DATA in AgentWorker execution context.
- Blocking/critical objections stop acceptance unless an authenticated human explicitly accepts residual risk; that action and objection disposition are durable.
- Message/proposal/objection authorship is provenance-bound so a human cannot relabel content as if another Team agent authored it.
- Reopening deliberation pauses READY/RUNNING work through the ordinary Task lifecycle and later resumes the existing waiting attempt where possible, preserving verified progress.


## M32 Bot Runtime audit

- Bot Runtime is a separate human-interactive surface; it does not make native provider/harness Bots autonomous Scheduler candidates.
- Hermes Bot Mode uses the documented authenticated Sessions API and profile namespace, preserving the Hermes profile and remote canonical Bot Chat.
- Hermes human Bot chat uses `bot/hermes/api-key`, deliberately separated from `agent-runtime/hermes/*` proposal-bridge authority.
- Provider-hosted Bot presets include ChatGPT GPTs, Grok Bot, Gemini Gems, Microsoft Copilot agents and Poe Bots. Where no supported external Bot-session API exists, OnePane records a provider-pinned launch URL rather than scraping/browser-cookie impersonation.
- Relay-backed conversational runtimes use OnePane Bot Protocol v1 and currently have presets for Dify, Flowise, n8n, Open WebUI and custom bridges.
- Bot turns have durable idempotency state. Network-uncertain dispatch becomes `unknown` and is not automatically replayed because the remote Bot may already have executed native tools.
- Remote/relay credential-bearing endpoints require HTTPS except loopback HTTP, reject credential-bearing URLs and do not follow redirects. Hosted launch URLs for fixed provider presets are domain-pinned.
- The embedded UI includes a minimal Bots roster/chat surface. API/relay Bots chat in OnePane; hosted-only Bots open their provider's supported surface.
- SQLite integration coverage verifies hosted-only Bots cannot manufacture an in-app dispatch/turn and verifies cross-workspace Bot registration is rejected; this test executes in the connected release gate.


## M33 Node Federation audit

- Every OnePane installation remains capable of originating its own Tasks; the Task origin is the authoritative coordinator/master for Task state, Policy, budgets, secrets, ToolGateway and Verification. Peers provide inference/model capacity only.
- LAN discovery uses an IPv4 multicast beacon and can create only `discovered` nodes. Discovery cannot grant trust, import schedulable capacity, or overwrite a peer endpoint/certificate after pairing has begun.
- Pairing is explicit and two-sided. A high-entropy pairing token plus six-digit human comparison code are short-lived; both nodes must confirm before `harness_nodes.trust_state` becomes `paired`.
- Federation identities are node-bound Ed25519 self-signed certificates persisted under the private OnePane key directory. All post-pairing traffic is TLS 1.3 with client certificates and SHA-256 certificate pinning against the durable pairing record.
- Capability manifests advertise only physically local, ready/degraded, non-provider-backed model deployments. Cloud/provider deployments are deliberately excluded so a peer cannot consume another node's API credentials or subscription allowance.
- Remote deployments are projected into the ordinary Model/ModelDeployment/CompatibilityProfile catalog and therefore enter the existing scheduler as `trusted_node` destinations rather than a parallel router.
- Remote inference is one hop only. The receiving node's federated execution entry point rejects non-local, provider-backed and `remote-node` deployments, preventing recursive federation and authority/secret propagation.
- Inbound remote inference uses durable idempotency receipts keyed by peer + remote request ID. Successful duplicates return the stored result; executing/unknown outcomes are not blindly replayed.
- Heartbeat expiry marks the peer and imported deployments unavailable; a valid mTLS heartbeat can recover an unavailable-but-still-paired peer. Explicit revocation disables all imported peer deployments immediately.
- Operator node-management endpoints require an active Admin role. The federation listener is distinct from the normal control-plane listener.

## M34 Remote model deployment + residency audit

- Pairing alone still grants only inference. Remote model management is a distinct per-peer inbound grant on the target node and is disabled by default.
- Grant direction is explicit: a target locally enables “allow peer A to manage models here”; an origin may query the target's mTLS-protected management status before showing install controls.
- Recommendation requests never trust origin-supplied hardware. The target detects/persists its own hardware profile and computes model/quantization fit locally.
- Remote installs are limited to the target node's signed Local AI catalogue. The target revalidates fit, creates its own durable install plan/job, downloads/verifies artifacts and performs empirical qualification before READY.
- Remote install-job visibility is peer-bound through `node_remote_model_jobs`; one management peer cannot query another peer's or a local user's install job by ID.
- `local_ai.model_pool_path` allows persistent model bytes to live on a large local SSD or mounted NAS/share; the default remains `<storage.data_dir>/models/managed`.
- Managed GPU residency is pressure-aware. The runtime supervisor uses admitted fit-plan memory estimates plus `residency_headroom_pct` and stops least-recently-used healthy/idle runtimes when a newly selected model would otherwise exceed usable VRAM.
- Active inference is protected by a reference-counted busy state. Endpoint acquisition now obtains a runtime lease under the same residency lock used for load/eviction, eliminating the resolve-then-evict race. Busy runtimes are never idle-reaped or pressure-evicted.
- Federated inference terminates at the target node's ordinary local runtime transport, so remote nodes get exactly the same lazy load, pressure eviction and idle unload behavior as local inference.
- Once a remotely installed model reaches READY, M33's ordinary capability heartbeat advertises it back into peer scheduler catalogs; no separate model-routing path is introduced.

## M35 Hardware-portable placement + Model Testbed audit

- Managed recommendations persist an explicit `PlacementPlan`; OnePane no longer treats arbitrary accelerator VRAM as one anonymous pool. Single-device placement is preferred when possible; multi-device sharding requires a shared backend.
- CPU is a first-class placement. Runtime catalogue backends include CPU, CUDA, ROCm/HIP, Vulkan, SYCL, Metal, OpenCL, MUSA and Ascend/CANN; concrete runtime device selectors and split arguments are generated from the placement plan.
- Runtime residency and pressure eviction account against the exact devices in the placement. Active inference holds a runtime lease, so an in-flight model cannot be reaped while another model is loading.
- llmfit integration is optional, loopback-only over plaintext, redirect-denying, and advisory. Current fit/quant/context/disk/capability/TP/performance fields are retained with `llmfit` provenance; OnePane qualification remains authoritative for scheduler admission.
- Successful automated qualification creates/refreshes a durable model spec sheet but leaves a managed model in `pending` production admission. A changed hardware profile or placement resets prior admission.
- `accepted` and `restricted` admission require a completed manual Testbed session for the exact current hardware profile and placement; an older trial cannot admit a changed placement. Synthetic Testbed tool probes expose a fake tool definition only and never enter ToolGateway or obtain CapabilityLease authority.
- Restricted admission can cap schedulable context, deny capability IDs and disable model tool callbacks. Pending/rejected managed models are excluded from ordinary local scheduling and federation export/inference.
- The master can inspect, Testbed and admit an eligible remote managed deployment through the existing paired-node mTLS channel, but the target node runs the Testbed and owns the resulting admission state. Remote model-management permission remains a separate per-peer grant.
- SQLite integration coverage exercises the pending -> completed Testbed -> restricted-admission boundary and spec-sheet enrichment. It runs in the connected release gate because this sandbox does not contain the `modernc.org/sqlite` module archive.

## V1-V5 assurance state

- V1: deterministic result checks and/or content-integrity-verified immutable Artifacts.
- V2: fresh direct resulting-state Observation.
- V3: V2 plus a distinct integration path with independently sourced Observation evidence.
- V4: V3 plus multiple evidence layers, paths and sources; model agreement alone does not qualify.
- V5: V4 plus an active human Approver/Admin acceptance bound to the exact evidence hash.
- Task completion remains PASS -> valid Checkpoint -> CompleteVerified; mutation completion remains Observation -> PASS -> Operation commit.

## Verification completed in this review environment

- All Python structural/milestone validators through M35: PASS.
- SQLite schema application/constraints: PASS (109 durable tables, 6 triggers).
- Dependency-free Go package test matrix, including M30 gateway, M31 Team/TeamWorker, M32 Bot Runtime/API, M33 node federation, M34 remote-model code and M35 placement/Testbed code: PASS.
- `go vet` on the dependency-free core: PASS.
- Go race detector on the dependency-free core: PASS.
- Shell syntax and Python validator syntax compilation: PASS.
- Archive integrity: PASS after RC8 packaging.

## Required connected Linux gate

This execution environment cannot fetch the uncached `modernc.org/sqlite` and `gopkg.in/yaml.v3` module archives, so SQLite-tagged Go integration tests and the complete `harnessd` release build cannot be truthfully certified here. On a network-connected Linux host run:

```bash
scripts/verify-release-candidate.sh
```

The candidate becomes binary-installable only if that command completes PASS. It resolves/verifies the pinned module graph, runs full and integration Go tests, vet and race detection, then produces checksum-protected amd64/arm64 Linux release binaries.

## Deferred UX / gateway ingress work

M30 implements outbound durable delivery and publishes inbound/media/thread capability metadata. Full conversational inbound gateway sessions, platform setup wizards, target discovery, OAuth/browser consent, and rich media composers belong in the final WebUI/onboarding phase. Relay-mode platforms can already receive outbound notifications without requiring OnePane to own their platform sockets.
