# RC11 — Trusted Node and Cloud Routing Contract

**Vision decision approved 2026-10-11.** Trusted, paired compute owned by
the user is first-class local-first infrastructure. Cloud remains distinct
and separately permitted. This is an implementation specification; it
does not imply existing cross-node routing is complete or secure.

## Execution classes and trust

1. **Host-local:** The deployment and inference transport execute on
   the current host. A local OpenAI-compatible endpoint must be verified
   by endpoint and local host binding, not by its display name.
2. **Trusted paired Node:** Independently authenticated, explicitly
   enrolled OnePane Node, accessed over an authenticated link and
   permitted by the Project/Workspace transfer policy. LAN/Tailscale
   connectivity by itself is not equivalent to authorization.
3. **External/cloud:** Other inference or hosting endpoints. Require
   provider-specific cloud routing consent, data-flow clearance and
   approved spend; no cloud fallback based on scheduler convenience.

Route classification must follow the **actual resolved destination** and
transport type for each dispatch, not just ProviderConnectionID or
user-editable model labels. An OpenAI-compatible server on another machine
is not automatically host-local: it must be a verified trusted Node or
otherwise use the explicitly permitted external path.

## Model and Worker routing

- Schedule eligible model/tool capability first, subject to required
  protocol, requested role, trust and Workspace/Agent grants.
- Prefer host-local if available, otherwise an authorized paired Node
  when transfer and security policy allow it. Waiting for resources,
  swapping unloaded models and using Colibri remain legitimate outcomes.
- Hardware-aware admission must account for CPU, RAM, GPU, VRAM,
  available model weights, cold-load/hot-swap cost, node health,
  queue latency, and local transport throughput without relying on
  unverified user-supplied capacity claims.
- Use separate settings for **Compute preference** (local and paired
  Nodes first), **Allowed Nodes** (explicit allowlist), **Allow
  off-host data transfer** (bound to Project/Workspace and resource
  sensitivity), and **Allow cloud inference** (disabled until opt-in).
- An off-host inference request may transport only the scoped input
  and minimum required artifacts, never unrestricted Project files,
  system credentials or unrelated Workspace data.
- If no eligible model is available on permitted hardware, retain
  the Task in a truthful waiting state, with resource/capability
  reason and retry/queuing information. Never silently substitute a
  paid provider, weaker tool-incapable model or different research seat.

## Integrity and failure handling

- Record Node enrollment identity, current trust version,
  authenticated transport fingerprint, chosen deployment, capability
  contract and authorization snapshot for provenance.
- Revalidate Node trust, Workspace transfer policy and current Agent
  grants before dispatch. Revocation prevents **new** transfers.
- Remote execution has explicit outcome-known, outcome-unknown and
  cancellation-confirmed states. On disconnect/restart, reconcile
  before retrying external side effects; do not assume that a lost
  response means an action failed.
- A Node cannot mint access to another Node, expand Workspace mounts,
  inject secrets, or grant itself a new model/tool policy.
- Preserve conservative limits for data, rate, inflight runs and
  spend; historical logs and diagnostics must not disclose prompts.

## Workstream manifest

- [ ] **NODE-01:** Canonical host-local, verified Node and external
      endpoint classification. Remove naive provider-record heuristics.
- [ ] **NODE-02:** Authentication, enrollment trust state, revocation
      and bounded Node-capability/health record verification.
- [ ] **NODE-03:** Add Project/Workspace off-host transfer grant,
      allowed Node list and per-Agent/Task restrictions.
- [ ] **NODE-04:** Integrate hardware-aware routing with Colibri,
      model hot swap, queue admission, capability/tool requirements
      and separately authorized cloud fallback.
- [ ] **NODE-05:** Enforce last-boundary dispatch destination checks
      and protect remote files, tools, secrets and model contexts.
- [ ] **NODE-06:** Durable unknown-outcome reconciliation and
      cancellation for disconnected/retired Nodes.
- [ ] **NODE-07:** Simple Nodes and model-routing UI with compute
      preferences, explicit transfer/cloud toggles and cost/status.
- [ ] **NODE-08:** Synthetic trust and endpoint-classification tests
      plus local/paired/cloud integration, offline/retry and restart QA.
- [ ] **NODE-09:** Second audit, cross-platform CI and approved
      installed Windows/Ubuntu real-device QA.

## Negative-path acceptance

- Local OpenAI-compatible server bound to loopback is recognised as
  host-local when verified; a provider record alone never marks a
  local deployment cloud.
- A WAN server called "local" cannot bypass cloud or remote-transfer
  policy using a friendly label or private IP spoofing.
- A paired Node may not receive classified data when Project or
  Workspace disallows off-host transfer even if its model is faster.
- Removing Node trust while a Worker is queued prevents dispatch
  without losing the Task; an in-flight unknown outcome must be
  reconciled rather than duplicated.
- Rate limits or unavailable Colibri/GPUs lead to safe queueing;
  paid cloud inference happens only with a deliberate approved grant.
- A delegated Worker changing its route cannot recover a revoked
  tool or filesystem privilege.
- A slow resource-constrained machine can still complete a
  multi-stage task with model swapping and checkpoint preservation.

**Gate:** Complete the design-aligned second code review before any
installer, merge or user acceptance sign-off. No claim of end-to-end
implementation follows from recording this requirement.

## Implementation checkpoint (2026-10-11)

The routing catalogue now derives a `TrustedNodeVerified` signal only from an active paired Node and fresh capability manifest, and the scheduler accepts an explicit `AllowTrustedNodes` plus exact `AllowedTrustedNodeIDs` for `LocalOnly` requests. These parameters default to denied. **No Worker or API currently supplies a new positive transfer grant from persisted Workspace policy**; do not enable this automatically or infer data transfer approval from a Task JSON field. Completing that binding and destination verification remains NODE-03 through NODE-05. Provider-backed models remain conservatively classified as non-local pending verified endpoint implementation.
