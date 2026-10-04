# OnePane v0.1.0-alpha.3

This repository is the first executable scaffold for the local-first autonomous AI harness design.

## Implemented in this scaffold

- Go module and package boundaries through **M35**, including managed Local AI, Routine/Watchdog, trusted Project runtime/preview ingress, WebGUI/authentication, Ubuntu appliance packaging, one-click Local AI install, autonomous agent execution, durable budgets, independent V1-V5 assurance, and broad provider/harness/SaaS integration registries.
- Frozen `0001_initial.sql` domain schema.
- Additive `0002_agent_runtime_connections.sql` for bring-your-own agent harnesses.
- Additive `0003_agent_runtime_invocations.sql` for durable external-runtime execution records.
- Additive `0004_project_workspaces.sql` for collaborative Project Runtime/application/change/routine state.
- Additive `0005_operation_coordinator.sql` for durable mutation authorization/resource-lease snapshots.
- Additive `0006_local_ai_bootstrap.sql` for hardware profiles, local-model install plans, and harness-managed runtimes/models.
- Additive `0007_local_runtime_supervisor.sql` for managed inference-process state and empirical qualification runs.
- Additive `0008_project_endpoint_routes.sql` for V2+-verified loopback Project endpoint routing.
- Additive `0022_alpha3_control_plane.sql` for first-class Project Workspaces, Library grants, immutable model specifications, global resource/runtime coordination, compute placement, fleet policy, and movable Project storage.
- SQLite migration runner with immutable migration checksums.
- Transaction abstraction, append-only Event Ledger, and durable Outbox.
- SystemMode, stable local HarnessNode bootstrap, Task/TaskAttempt state machines, Policy + CapabilityLease, Artifact/Observation evidence, ToolGateway, Verification/Checkpoint, inference catalog/execution, deterministic Context Compiler, canonical Agent Protocol, and external Agent Runtime dispatch.
- OpenAI-compatible inference transport plus deterministic fake transport.
- Canonical external-agent HTTP transport for custom harnesses/bridges.
- Bootstrap wiring for local inference, external-agent runtimes, the M12 scheduler, provider onboarding, Project Workspaces, REST/SSE, OperationCoordinator/recovery, approval gating, built-in Vault/Secret Broker, Local AI bootstrap, and the Project Runtime reconciler.
- Rootless Podman/Docker sandbox adapter with independent inspect probes and an allow-listed `EXECUTE_SANDBOXED` app-command path.
- Project Routine bindings can execute an already-materialized Task inside a verified running app without granting authority themselves.
- M17 durable single-use human approvals plus governed compensation Operations.
- M18 built-in envelope-encrypted Vault/Secret Broker; inference and external-runtime transports now resolve `vault:` references directly rather than requiring environment variables.
- Native Local AI hardware detection/recommendation/provisioning: quantization/context/storage-aware model fit planning, harness-managed runtime/model downloads, managed `llama-server` supervision, restart identity checks, empirical protocol/context qualification, and scheduler-ready compatibility profiles.
- Deterministic Routine Engine with interval/daily timezone-aware triggers, bounded catch-up semantics, atomic occurrence+Task materialization, and Project routine integration.
- Deterministic Watchdog heartbeat enforced by Policy: read/observe remain diagnostic-safe, while sandbox execution/mutations/external sends fail closed when control-plane health is stale.
- M21 Project runtime services: workspace-scoped Vault secret injection at the trusted sandbox boundary, dedicated rootless internal Project networks with loopback-only published ingress, and an autonomous Routine `app_command` worker that completes Tasks only through Observation → independent Verification → Checkpoint.
- M22 trusted preview ingress: V2+-verified loopback routes, per-endpoint browser origins, one-time preview grants, HttpOnly preview sessions, credential/membership rechecks, and a loopback-only reverse proxy that never forwards OnePane control-plane credentials.
- M23 first-run product surface: local Admin bootstrap, PBKDF2 password identity, opaque server-side Web sessions, SameSite/CSRF protections, embedded no-CDN WebGUI shell, typed Vault provider-credential management, OmniRoute probe/connect, and Local AI hardware detection/recommendation endpoints.
- M24 Ubuntu appliance packaging: static release binaries/checksums, dedicated unprivileged `onepane` service identity, apt-managed rootless Podman prerequisites, systemd hardening compatible with rootless UID/GID mapping, loopback-first installation, rootless runtime verification, health-gated startup, and backup/rollback-aware upgrades.
- Dependency-free validators and standard-library unit tests through M35 plus the sandbox, Local AI, Routine, Watchdog, runtime-services, trusted ingress, appliance packaging, autonomous worker, durable budgets, independent assurance, cloud-provider, external-harness, and SaaS connector foundations.

## Deliberately not implemented yet

anonymous/public endpoint publication and controlled outbound-egress proxy, microVM execution,
privileged-helper operations, memory/knowledge compilation,
and browser-driven OAuth/refresh-token onboarding for SaaS plugins remain later milestones. Alpha 3 now adds first-class Project Workspaces, Direct/Team/Council model-stack configuration, global resource coordination, CPU/GPU/hybrid runtime scheduling, project storage relocation, and remote compute-fleet policy on top of the Alpha 2 control plane. M29 connectors are functional with Vault-backed API keys/access tokens today; the polished OAuth UX belongs in the final WebUI phase.

M26 now consumes bounded `AgentRequest`/`AgentResponse` exchanges from both model
deployments and eligible external agent runtimes. Tool proposals still carry no
authority: they require an existing task-bound CapabilityLease, and mutation
proposals enter the normal OperationCoordinator/Verification path. A model/runtime
`complete` proposal can establish only V0 declaration evidence unless the Task's
completion contract requires a stronger independent verification path. Direct
unmanaged runtimes remain excluded from autonomous dispatch.

## Validate the schema and milestone invariants

```bash
python3 scripts/validate_schema.py
python3 scripts/validate_task_lifecycle.py
python3 scripts/validate_m5_storage.py
python3 scripts/validate_m6_evidence.py
python3 scripts/validate_m7_tooling.py
python3 scripts/validate_m8_assurance.py
python3 scripts/validate_m9_inference.py
python3 scripts/validate_m10_inference_requests.py
python3 scripts/validate_m11_agent_runtime.py
python3 scripts/validate_m12_scheduler.py
python3 scripts/validate_m13_project_workspace.py
python3 scripts/validate_m13_api.py
python3 scripts/validate_m14_vertical_slice.py
python3 scripts/validate_m15_operations.py
python3 scripts/validate_m16_recovery.py
python3 scripts/validate_m17_approvals.py
python3 scripts/validate_m18_vault.py
python3 scripts/validate_local_ai_bootstrap.py
python3 scripts/validate_m19_local_runtime.py
python3 scripts/validate_routine_engine.py
python3 scripts/validate_watchdog.py
python3 scripts/validate_sandbox_runner.py
python3 scripts/validate_m21_runtime_services.py
python3 scripts/validate_m22_ingress.py
python3 scripts/validate_m23_webgui.py
python3 scripts/validate_m24_install.py
python3 scripts/validate_m25_local_ai_install.py
python3 scripts/validate_m26_agent_worker.py
python3 scripts/validate_m27_budgets.py
python3 scripts/validate_m28_assurance.py
python3 scripts/validate_m29_integrations.py
python3 scripts/validate_m30_gateway.py
python3 scripts/validate_m31_team_mode.py
python3 scripts/validate_m32_bot_runtime.py
python3 scripts/validate_m33_node_federation.py
python3 scripts/validate_m34_remote_models.py
python3 scripts/validate_m35_hardware_testbed.py
python3 scripts/validate_alpha3_source.py
python3 scripts/validate_release_candidate.py
```

## Run unit tests that use only the Go standard library

```bash
GOPROXY=off go test \
  ./internal/id ./internal/clock ./internal/event ./internal/outbox \
  ./internal/system ./internal/node ./internal/task ./internal/authority \
  ./internal/policy ./internal/artifact ./internal/observation ./internal/tool \
  ./internal/verification ./internal/inference ./internal/agentprotocol \
  ./internal/contextcompiler ./internal/agentruntime ./internal/scheduler \
  ./internal/provideronboarding ./internal/accountaccess ./internal/browserworkspace \
  ./internal/projectworkspace ./internal/api ./internal/projectruntime \
  ./internal/projectroutine ./internal/routine ./internal/routineworker \
  ./internal/sandboxrunner ./internal/vault ./internal/watchdog ./internal/ingress \
  ./internal/webauth ./internal/webui ./internal/budget ./internal/assurance \
  ./internal/agentworker ./internal/verticalslice ./internal/connectors ./internal/gateway \
  ./internal/team ./internal/teamworker ./internal/botruntime ./internal/localai ./internal/nodefederation
```

## Full Go build

The runtime uses:

- `modernc.org/sqlite` for pure-Go SQLite access.
- `gopkg.in/yaml.v3` for the minimal bootstrap YAML file.

With normal network/module access:

```bash
go mod tidy
go test ./...
go run ./cmd/harnessd -config ./config.example.yaml
```

For development, point `storage.data_dir` at a user-writable private directory rather than `/var/lib/onepane`.

## Security notes

- The local node seed is a bootstrap identity seed, **not** the future federation TLS private key. Federation credentials will be brokered by the built-in Vault/Secret Broker milestone.
- The default listener is loopback-only.
- Manual SQLite edits are not a supported administration path.
- Events, Observations and Artifact revisions are protected as immutable by SQLite triggers.


## M4 Task invariants

The Task package intentionally exposes commands rather than a generic `SetState` API. Normal transitions are validated by the domain state machine and persisted with an expected revision. Starting a Task atomically creates one `TaskAttempt` and transitions the durable Task to `RUNNING`.

SQLite additionally enforces that a Task has at most one attempt in an active state (`created`, `queued`, `running`, or `waiting`). This is a database-level backstop against concurrent or buggy duplicate starts.

An execution incarnation interrupted by host/process recovery is not silently resumed. Its attempt becomes `INTERRUPTED`, the durable Task becomes `BLOCKED`, and a `task.recovery.required` outbox job is emitted. A future RecoveryCoordinator will reconcile durable/external state before re-admitting the Task and creating a fresh attempt.

## M5 Policy + CapabilityLease

M5 adds a deterministic authority/policy boundary without changing the frozen
`0001_initial.sql` migration.

Implemented invariants:

- Capability leases are explicit, revisioned authority grants. They are scoped
  to one workspace/principal, optionally one Task, one capability identifier,
  explicit action modes, and explicit resource references/prefixes (or an
  explicit `any_*` grant). Empty scope never means wildcard.
- Lease state is one-way: `active` may become `expired`, `revoked`, `exhausted`
  or `invalidated`; terminal leases cannot be reactivated.
- Lease use is bounded by `expires_at` and optional `usage_limit`. Reaching the
  limit atomically moves the lease to `exhausted`.
- Agent principals cannot directly issue authority to themselves.
- Lease issue/revoke/expire/invalidate/use changes and their causal Events are
  committed in the same SQLite transaction.
- The PolicyEngine fails closed on malformed input, unavailable system state,
  missing/corrupt lease state, inactive subjects, workspace/task mismatch,
  expiry/exhaustion, and scope mismatch.
- SystemMode is a hard platform gate above ordinary policy. `safe` and
  `read_only` permit only observe/read; commissioning/recovery/degraded also
  permit sandboxed execution; normal permits all action classes subject to
  authority; maintenance denies external send.
- Risk composes conservatively with action requirements. The strongest required
  verification and strongest required human approval win.
- Information-flow checks keep workspace, confidentiality and residency
  independent. Cross-workspace flow is denied; clearance is enforced;
  `TRUSTED_NODES` and `ORIGIN_NODE` residency are enforced; derived labels may
  become more restrictive but cannot declassify or relax residency.
- `VERIFIED_DERIVED` cannot be asserted merely by a model response; the policy
  input must state that verification was independently established.

Standard-library M5 unit tests run with:

```bash
go test ./internal/authority ./internal/policy
```

The real SQLite integration suite is intentionally tagged because it imports
`modernc.org/sqlite`:

```bash
go test -tags=integration ./internal/policy
```

That suite exercises valid authorization plus denial of expired, revoked,
out-of-scope and cross-workspace capability use, usage exhaustion, and rollback
of lease state if the causal Event cannot be committed.


## M6 Artifact + Observation

M6 adds the first durable evidence layer without changing the frozen schema.

- Artifact bytes are stored in a local SHA-256 content-addressed store under
  `storage.data_dir/artifacts`. Publishing uses an immutable hard-link step so
  an existing content address is never overwritten.
- Repeated identical content deduplicates to the same blob while each Artifact
  retains its own durable metadata/provenance row.
- Artifact storage references are validated before filesystem access; path
  traversal is rejected. `VerifyContent` recomputes both size and SHA-256.
- Artifact metadata creation validates workspace/project and creator scope, then
  writes the Artifact row and `artifact.created` Event atomically. A failed
  metadata transaction may leave an unreferenced content-addressed blob, which
  is safe and can be collected by a future GC.
- Observations are immutable point-in-time probe records. Their integrity hash
  is computed by the harness from probe identity, subject, value, source, data
  label and observation time; callers do not supply it.
- Observation sources are checked against current workspace membership (global
  system/recovery/watchdog principals remain eligible), and the row plus
  `observation.recorded` Event commit atomically.
- Existing SQLite immutability triggers remain the durable backstop preventing
  Observation update/delete.
- Artifact and Observation labels use the frozen confidentiality, residency and
  trust dimensions and round-trip through the Policy label types.

M6 unit tests run without external dependencies:

```bash
go test ./internal/artifact ./internal/observation ./internal/policy
```

With normal Go module access, the tagged SQLite integration suites are:

```bash
go test -tags=integration ./internal/policy ./internal/artifact ./internal/observation
```


## M7 ToolInvocation + Tool Gateway

M7 adds the first trusted tool-execution boundary without changing the frozen schema.

- Tool identity, capability, action mode, adapter identity/version, risk, minimum verification and minimum approval are registered by trusted harness code. Invocation callers cannot override those fields.
- Every accepted execution attempt receives a durable `tool_invocations` row before authorization. Policy denials and other pre-execution failures are retained as terminal failed invocations for auditability.
- The gateway evaluates M5 Policy/CapabilityLease authority, validates Task/TaskAttempt execution context, then consumes exactly one lease use before an adapter can run. A failed consume never reaches the adapter.
- The durable authorization Event records the lease revision, built-in policy revision, required verification level and approval requirement used for that decision.
- Invocation lifecycle is explicit: `created -> authorized -> running -> succeeded|failed|timed_out|cancelled|interrupted`. SQL transitions use the expected prior status as the concurrency backstop.
- Adapter panics are contained and converted to durable failures. Results must be valid JSON. Inputs are canonicalized before SHA-256 hashing, so insignificant JSON formatting does not change `input_hash`.
- The original M7 boundary executed only `observe` and `read`. The current scaffold additionally permits `execute_sandboxed` only through bootstrap allow-listed trusted sandbox adapters, and permits `mutate`/`external_send` only when M15 supplies an exact short-lived ExecutionPermit for a durable Operation. Capability authority alone still cannot bypass those platform invariants.
- `builtin.synthetic` provides `synthetic.echo@1`, a deterministic read-only adapter/tool used to prove the full gateway path. Tool success remains an execution result, **not** external-state verification. M8 owns verification semantics.

Standard-library M7 tests:

```bash
GOPROXY=off go test ./internal/tool
```

With normal module access, the SQLite integration test is:

```bash
go test -tags=integration ./internal/tool
```


## M8 Verification + Checkpoint

M8 adds the first Assurance-plane completion evidence chain without changing the frozen schema.

- Verification records are revisioned and begin `pending`. They resolve to `pass`, `fail`, or `inconclusive`; resolved records may later become `stale` but cannot be rewritten back into a passing result.
- Verification levels retain the frozen `V0..V5` ordering. A `pass` is rejected unless the achieved level is at least the verification's required level. A second model agreeing is not itself a mechanism for raising the achieved level; callers that resolve verification are trusted Assurance components, not worker/model APIs.
- Verifiers must be active eligible principals in the Verification workspace. System/recovery/watchdog principals may act globally; ordinary human/agent/service principals require active workspace membership.
- A Checkpoint can only be created from a passed Verification bound to the same Task/workspace. Checkpoint state is immutable JSON evidence. Creating a new valid Checkpoint supersedes older valid Checkpoints and records the superseded IDs in the causal Event.
- Checkpoint creation fails closed while the Task has an Operation in `unknown_outcome` or `blocked_unknown_outcome`.
- `TaskService.CompleteVerified` is now the first and only public Task-completion command. It requires a valid Checkpoint backed by a passed Verification, rechecks for unknown mutation outcomes in the same completion transaction, and atomically marks a waiting active TaskAttempt `succeeded` with `Task -> complete`.
- A stale Verification no longer satisfies the completion SQL join even if its Checkpoint row has not yet been separately invalidated. A stale/superseded Checkpoint likewise cannot complete a Task.
- M15 now conservatively marks every still-valid Task Checkpoint stale when a mutation crosses `PREPARED -> EXECUTING`, before the trusted adapter is dispatched. This prevents completion from reusing evidence that predates an attempted external side effect, even when the call later fails or has an unknown outcome.

Standard-library M8 tests:

```bash
GOPROXY=off go test ./internal/verification ./internal/task
```

With normal module access:

```bash
go test -tags=integration ./internal/verification
```


## M9 Model catalog + bring-your-own agent harness

M9 implements the durable inference catalog while keeping **models** and
**agent harnesses/runtimes** as separate domain concepts.

### Models and providers

- `Model` is the identity of weights/API model metadata.
- `ModelDeployment` is a runnable placement of that model at a HarnessNode and/or
  ProviderConnection/runtime. Deployment fingerprints are computed by the
  harness from canonical runtime configuration rather than supplied by callers.
- `ProviderConnection` stores connection metadata plus a `secret_ref`; obvious
  raw secret fields in connection/runtime JSON are rejected.
- Newly registered ProviderConnections begin `unavailable`; connectivity is not
  asserted until a transport/probe establishes it.
- User-facing model registration may create `quarantined` or `user_trusted`
  models. The stronger system `trusted` state cannot be self-asserted through
  the normal command path.
- A quarantined model cannot move a deployment to `ready`.
- Generic registration code cannot set `context_max_verified`; that value is
  reserved for empirical deployment qualification performed by the managed-local
  runtime qualification subsystem.

### External agent runtimes

`AgentRuntimeConnection` is introduced by
`0002_agent_runtime_connections.sql`. It is intentionally neither a
`ModelDeployment` nor a paired `HarnessNode`.

Three operating modes exist:

- `proposal_only` — returns canonical proposals; tool proposals are forbidden.
- `gateway_mediated` — may return a canonical `tool` proposal for the origin
  harness to authorize/execute later through ToolGateway.
- `unmanaged` — opaque runtime with its own tools/state; excluded from autonomous
  dispatch.

An untrusted connection is also not autonomously schedulable. Explicit
`user_trusted` status is required before proposal-only/gateway-mediated dispatch.
External output remains `UNVERIFIED_DERIVED`; adapter registration never turns
agent output into trusted truth.

External runtimes fail closed to:

```json
{
  "max_confidentiality": "public",
  "allowed_residency": ["any"],
  "destination_kind": "untrusted",
  "allow_raw_secrets": false
}
```

See [`docs/external-agent-runtimes.md`](docs/external-agent-runtimes.md) for the
bring-your-own-harness contract and Hermes-specific boundary.

## M10 InferenceRequest + transports

M10 turns the M9 inference catalog into an executable inference path.

- Every valid attempt receives a durable `InferenceRequest` before dispatch.
  Information-flow/trust denial is therefore auditable rather than disappearing
  before a request record exists.
- M10 dispatch uses an **explicit ready `ModelDeployment`**. Automatic routing is
  intentionally deferred to M12.
- The trusted deployment owns model identity. The OpenAI-compatible transport
  overwrites any caller-supplied `model` field with the registered `Model.model_ref`.
- InformationFlow is evaluated before dispatch. Provider clearance/residency
  metadata cannot override platform residency constraints.
- Remote-node deployments remain fail-closed unless the target HarnessNode is explicitly paired and currently healthy; M33 supplies pinned mTLS federation and scheduler-visible remote deployments.
- Successful output is stored as an immutable Artifact carrying the inherited
  confidentiality/residency label with trust reduced to `UNVERIFIED_DERIVED`.
- `builtin.fake` provides deterministic end-to-end inference for tests.
- `builtin.openai_compatible` supports authenticated HTTP endpoints with:
  redirect denial, credential-bearing URL rejection, response-size limits,
  bounded timeouts, non-streaming JSON, model pinning, and `secret_ref` lookup.
- `EnvSecretResolver` (`env:NAME`) is a transitional development resolver only;
  M18 replaces this with the built-in Vault/Secret Broker.

M10 does not yet implement BudgetReservation or scheduler selection; those are
separate control-plane responsibilities.

## M11 Context Compiler + Agent Protocol + external runtime execution

M11 adds the canonical worker protocol and makes a safe bring-your-own harness
path executable.

### Context Compiler

- Context is deterministic and byte-bounded in v0.1.
- Required context is never silently truncated; overflow is an error.
- Non-historical/current context is retained ahead of optional historical
  context.
- Authoritative context is prioritized within the same retention class.
- The compiler emits an inclusion/drop manifest plus a SHA-256 hash of the
  compiled context.
- Context sections carry explicit trust labels; unknown trust labels fail closed.

### Agent Protocol v1

`AgentRequest` and `AgentResponse` implement the frozen proposal types:

```text
tool, delegate, replan, complete, human, escalate, wait, fail
```

Responses must match the request ID and one of the proposal types explicitly
permitted by the request. A `tool` proposal is valid only for a
`gateway_mediated` request.

A `complete` proposal is **not** Task completion. It is persisted as external
runtime evidence while the Task remains under the origin control plane. The
normal Verification -> Checkpoint -> `CompleteVerified` chain remains mandatory.

### Durable external-runtime invocation

`0003_agent_runtime_invocations.sql` adds a durable lifecycle:

```text
created -> authorized -> dispatched -> executing -> succeeded|failed|unknown|cancelled
```

- Caller/workspace/Task/TaskAttempt relationships are checked before execution.
- Connection data policy and platform InformationFlow are both enforced.
- `TRUSTED_NODES` external-runtime dispatch remains disabled until paired mTLS
  federation exists.
- `ORIGIN_NODE` dispatch requires an explicit binding to the local HarnessNode.
- Network/uncertain transport failures become `unknown`; known protocol/HTTP
  failures become `failed`.
- Successful `AgentResponse` JSON is stored as an immutable Artifact.
- The built-in `builtin.agent_protocol_http@1` transport defaults to
  `POST /v1/agent/run`, rejects redirects/credential-bearing URLs, caps response
  size/timeouts, and resolves auth through `secret_ref`.

For Hermes or another full agent system, use the canonical bridge for autonomous
integration. If the external system executes its own side-effecting tools before
returning a response, classify it as `unmanaged` rather than claiming that its
side effects passed through this harness's ToolGateway.

Standard-library M9-M11 tests:

```bash
GOPROXY=off go test \
  ./internal/inference ./internal/agentprotocol \
  ./internal/contextcompiler ./internal/agentruntime
```

With normal module access, the SQLite integration suites are:

```bash
go test -tags=integration ./internal/inference ./internal/agentruntime
```

## M12 Scheduler + provider onboarding

M12 adds deterministic candidate routing across qualified `ModelDeployment`s and
eligible external `AgentRuntimeConnection`s while preserving the control plane's
trust, residency, cost, and entitlement boundaries.

### Scheduler

- Candidates are filtered by workspace, schedulability, role, capability,
  protocol level, context envelope, compatibility qualification, residency, and
  InformationFlow policy before ranking.
- Untested, limited, mediated, and degraded candidates require their respective
  explicit route permissions.
- Local deployments and externally hosted deployments use the same candidate
  abstraction without erasing their different trust/data-flow properties.
- External unmanaged agent runtimes remain non-schedulable.
- Every route decision, including a no-candidate result, is appended to the Event
  Ledger for auditability.

### Cost and entitlement preservation

Autonomous routing fails closed against surprise resource consumption:

- Local and proven hard-$0 routes are the only default cost classes.
- `included_subscription` routes require `AllowSubscriptionUsage=true`.
- Paid, unknown, provider-managed, and soft/free-without-hard-stop routes require
  `AllowPotentialMonetarySpend=true`.
- Subscription permission never implies spend permission and vice versa.
- Even after subscription use is permitted, an eligible local/proven-$0 route is
  preferred before subscription allowance. Subscription is a fallback resource,
  not a synonym for free capacity.
- M27 `BudgetReservation` enforcement refines those permissions into bounded,
  durable capacity reservations without weakening the fail-closed default.

### OmniRoute first-run preset

`provideronboarding` exposes OmniRoute as the recommended first-run provider at
`http://127.0.0.1:20128/v1`. It probes `/v1/models` and, when readable, exported
OmniRoute settings. The default first-run intent is strict zero-cost operation.

The harness only marks OmniRoute as `free` with a hard zero-cost guarantee when
it verifies `freeAccessPolicy=strict`. Otherwise the connection is
`provider_managed`, so autonomous use requires explicit potential-spend
permission.

### ChatGPT plan and Browser Workspace

The public Sign in with ChatGPT OSS flow is retained as an optional automated
fallback. It is classified `included_subscription`, because requests consume the
user's included plan allowance; connecting the account does not authorize the
scheduler to use it.

Browser Workspace is intentionally different: it is a human-only browser
surface for normal subscription websites. Browser credentials stay inside the
browser profile, credential extraction is forbidden, subscription use is
user-initiated only, and the browser is never a scheduler candidate. Users can
explicitly import selected output back as unverified evidence.

See [`docs/provider-onboarding-and-browser.md`](docs/provider-onboarding-and-browser.md).

Standard-library M12 tests:

```bash
GOPROXY=off go test \
  ./internal/scheduler ./internal/provideronboarding \
  ./internal/accountaccess ./internal/browserworkspace
```

With normal module access, run the complete suite with `go test ./...`.

M13 implements the initial REST/SSE transport. M14 now proves the read-only end-to-end evidence path; M15 owns all external mutation permits/resource locks; M16 converts interrupted mutation execution into blocked unknown outcomes before any retry. The first rootless Project sandbox backend has therefore been brought forward and is layered on those semantics rather than bypassing them.


## M13 REST/SSE + Project Workspace foundation

M13 now exposes a small authenticated REST/SSE surface and adds the durable Project Runtime model needed for a native collaborative GUI.

- Projects may own one `ProjectRuntime` with separate desired and observed state. The runtime is declared against the trusted `sandbox_runner` backend, but M13 does **not** claim isolation or successful execution until that backend actually reconciles/observes the runtime.
- Runtime security defaults are fail-closed: ephemeral root, `/workspace`, no arbitrary host mounts, no Docker socket, no device passthrough, `no_new_privileges`, deny-by-default egress and proxy-only ingress. Runtime/application specs are rejected if they attempt privileged/host namespace/mount/device escape controls.
- Existing applications can be declared from OCI images, Git, packages, Artifact Store objects or Compose definitions. Imported apps default to `untrusted_content`.
- Environment bindings distinguish literals from `secret:` references. M21 resolves logical names against the active secret version in the same Workspace and delivers plaintext only at the trusted sandbox adapter boundary; container spec hashes retain only the Vault record/version identity.
- `ProjectChangeProposal` gives humans and agents one review model for code/content/runtime/application/endpoint/routine changes. The proposer cannot satisfy their own review gate. Future Git worktrees attach diffs to this object.
- `ProjectRoutineBinding` connects an existing Routine to an app command, internal HTTP action or tool capability. It grants no authority and does not run container-local cron. The Routine Engine now creates normal Tasks; M21 autonomously executes verified `app_command` bindings through a single-use task-bound lease and the normal Assurance path.
- Project/runtime/app/change/binding state has REST read/write routes. Workspace-authorized SSE streams the append-only Event Ledger using durable event sequence IDs, providing the live update feed for the future GUI.
- Protected REST routes take actor identity from scoped API credentials; request JSON cannot choose the actor. Workspace and capability scopes are both enforced. The anonymous surface is limited to `/v1/health`.
- The default API bind remains loopback. Non-loopback configuration requires an HTTPS `public_origin`, making reverse-proxy/TLS deployment explicit.

See `docs/project-workspaces.md` and `docs/rest-sse.md`.


## M14 read-only vertical slice

`internal/verticalslice.ReadOnlyRunner` proves one complete non-mutating Task path: Task admission/start -> scoped read lease -> ToolGateway read -> immutable Observation -> integrity check -> independent V1 Verification -> Checkpoint -> `CompleteVerified`. Mutation authority is never present in this slice.

## M15 OperationCoordinator + MutationGate

External mutation now uses durable `Operation` + exclusive `ResourceLease` state and an ephemeral single-use ExecutionPermit. The ToolGateway rejects `mutate`/`external_send` calls without that permit. Adapter success only reaches `OBSERVING`; independent Verification is required before `COMMITTED` releases the writer lock.

See `docs/operations-and-recovery.md`.

## M16 recovery/reconciliation

Bootstrap treats any Operation left `EXECUTING` across process restart as an unknown external outcome and moves it through `UNKNOWN_OUTCOME -> BLOCKED_UNKNOWN_OUTCOME`. There is no direct unknown-outcome retry transition. A direct V2+ observation can either accept that the desired state already exists or prove a retry is safe; safe retry renews the existing writer lease.

## Project sandbox execution foundation

The Project Runtime now has a real trusted adapter for rootless Podman/Docker. Application lifecycle mutations use M15 Operations and serialize at the Project Runtime resource boundary. Independent OBSERVE tools verify resulting state and container isolation before Project/Application observed status advances.

`project.app.exec` supplies the first `EXECUTE_SANDBOXED` primitive. ToolGateway enables that mode only for explicitly allow-listed sandbox adapters. Project Routine `app_command` bindings can invoke it from an already-materialized normal Task and scoped CapabilityLease.

Project app secrets and local ingress are now partially operational: `secret:<logical-name>` bindings resolve only inside the Project Workspace and are passed through a short-lived private env file; each Project Runtime owns a rootless `--internal` container network, and declared app ports publish only to dynamic `127.0.0.1` host ports. Direct egress remains fail-closed. M22 can expose verified HTTP endpoints through authenticated, per-endpoint preview origins, but declarations marked `public` still do **not** become anonymous or bind to LAN/WAN; public publication remains a separate approval/Operation path. Non-OCI build/install resolution and broader Routine action kinds remain fail-closed.

See `docs/sandbox-runner.md` and `docs/project-routines.md`.


## Local AI bootstrap

The harness now owns the beginning of the zero-prerequisite local-AI setup path. On Linux it can persist a hardware profile covering CPU/RAM/storage, GPU/VRAM/driver/backend information and existing runtime probes, then rank local models by use case, quantization, context envelope, estimated execution path, fit level and disk headroom.

The finished setup flow is intended to be `detect -> recommend -> choose -> install runtime -> download model -> qualify -> schedule`. The default managed runtime is llama.cpp; existing Ollama/LM Studio/vLLM installations may later be adopted but are not prerequisites. Managed runtime downloads require a trusted digest; model downloads without one remain quarantined. Installation only creates a `QUALIFYING` ModelDeployment—empirical qualification must still prove it before scheduler use.

See `docs/local-ai-bootstrap.md`.

## M17 approvals + compensation

Policy-required approvals are durable, expiring and single-use. They bind to the exact Operation intent and Policy revision, reject self-approval, and require an eligible human Approver/Admin. Consumption happens atomically with exclusive ResourceLease acquisition. Compensation creates a second governed Operation linked to the committed original rather than executing privileged rollback code.

See `docs/approvals-and-compensation.md`.

## M18 built-in Vault / Secret Broker

The normal product no longer depends on environment variables for provider credentials. Secret values are envelope-encrypted with per-secret random DEKs and a local 256-bit KEK, versioned through `secret_records`, and resolved by trusted transports using `vault:<secret_record_id>` references.

See `docs/vault.md`.


## M21 Project runtime services

M21 closes three runtime gaps needed before the GUI can safely present self-hosted applications as usable services:

- **Workspace-scoped Vault injection:** sandbox environment bindings use `secret:<logical-name>`. Resolution selects only the active secret in the Task Workspace. Plaintext is handed directly from the built-in Vault to the trusted sandbox adapter; it is excluded from the container spec hash, which records only `vault:<record-id>:v<version>`. The container engine receives values through a short-lived `0600` env file that is removed immediately after launch. Multiline environment secrets fail closed until file-secret mounts exist.
- **Internal Project networking:** every rootless Project Runtime gets a dedicated container network created with `--internal`. Applications have no direct external egress. Declared app endpoints are translated into dynamic loopback publications such as `127.0.0.1::<container-port>`; inspection verifies the network label/internal bit and rejects any published host IP other than loopback. Public/LAN/WAN exposure still requires the future trusted ingress proxy.
- **Autonomous Routine app-command worker:** materialized Project Routine Tasks are admitted and started by a dedicated system worker, using a distinct system authority principal and independent verifier principal. Each command receives a one-use `project.app.execute` CapabilityLease bound to the Task and Project Runtime. Success becomes an immutable Observation, receives independent V1 verification, creates a Checkpoint, and only then reaches `CompleteVerified`. On daemon restart, any lost worker attempt is interrupted and BLOCKED rather than automatically replayed because the sandbox command may have produced an unknown side effect.

The M21 worker is intentionally narrow: it handles Project Routine `app_command` bindings only. It is not yet the general autonomous agent orchestration loop.


## M22 trusted Project preview ingress

M22 turns the loopback-only port mappings established by M21 into safe browser previews without weakening the Project sandbox or the OnePane control-plane origin.

- A Project endpoint route is persisted only from the same V2+ `project_app` Observation/Verification that proved the container is running, isolated, and published on `127.0.0.1:<dynamic-port>`. The route records the application/endpoint revision plus container spec hash, so a restart or revision change invalidates stale routing.
- The control-plane API never proxies untrusted application HTML. `POST /v1/project-endpoints/{id}/preview-session` authorizes `project.read` and mints a one-time, short-lived preview grant instead.
- Each endpoint receives its own browser origin (`ep-<hash>.localhost:<port>` locally, or `ep-<hash>.<preview-domain>` remotely). This isolates Project application JavaScript/cookies from OnePane and from other Project applications, while allowing ordinary absolute `/assets`, `/login`, and WebSocket paths to behave normally.
- The preview grant exchanges into an HttpOnly, SameSite=Strict, host-only cookie. Current principal, API credential, Workspace membership, Workspace status, and credential scopes are rechecked on each proxied request.
- The trusted proxy dials only the durable verified `127.0.0.1:<port>` route. It does not resolve caller-provided upstream URLs, strips `Authorization`/proxy credentials, removes its reserved session cookie before forwarding, and prevents the sandbox app from overwriting that cookie.
- `public` endpoint declarations remain authenticated previews. Anonymous/public publication is intentionally still unimplemented and will require a separate governed Operation/approval plus TLS/DNS policy.
- Remote installations must configure a distinct HTTPS `preview_public_origin` with wildcard DNS/TLS routing (for example `*.preview.onepane.example`) to the loopback preview listener. The preview origin may never equal the control-plane origin.

See `docs/trusted-ingress.md`.


## M23 WebGUI, first-run auth, and provider credentials

M23 introduces the first embedded product surface. The first-run bootstrap transaction creates the initial Admin Principal, default Workspace, Admin role membership, local password identity, and opaque AuthSession; the system then advances from `bootstrap` to `commissioning`. Re-entry is denied once active human/auth state exists.

Browser sessions use an HttpOnly SameSite=Strict cookie and a separate per-session CSRF value. CLI/service bearer credentials remain supported. The WebGUI is embedded in `harnessd` and loads no third-party JavaScript or CSS. Project previews remain on the separate M22 per-endpoint origins.

Provider credentials created from the WebGUI/API are explicitly namespaced:

```text
provider/xai/api-key
omniroute/xai/api-key
omniroute/gateway/access-token
plugin/github/access-token
plugin/gmail/access-token
agent-runtime/claude-code/access-token
```

This keeps OmniRoute-managed credentials visibly and semantically separate from direct-provider credentials. The initial Models & Providers screen can also probe/connect the local OmniRoute gateway and invoke Local AI hardware detection/recommendation through authenticated control-plane APIs.


## M24 Ubuntu appliance packaging

The repository now includes `scripts/build-release.sh`, `scripts/install-ubuntu.sh`, `scripts/upgrade-ubuntu.sh`, and reference systemd/config/container files. The installer creates the dedicated `onepane` account and private state directory, installs rootless Podman prerequisites on apt-based hosts, verifies release SHA-256 sidecars, starts the daemon on loopback, verifies both `/v1/health` and rootless Podman operation, and prints an SSH-tunnel command for secure remote first-run access.

The upgrader stops the daemon, captures a quiescent SQLite/binary backup, swaps the executable atomically, and rolls the binary back if the restarted control plane fails its local health check.


## M25 signed Local AI catalogue + durable install jobs

M25 closes the gap between hardware recommendations and usable local inference.
A signed, monotonic model/runtime catalogue resolves an approved fit recommendation
to exact immutable download artifacts. One-click installs create a durable job
which survives daemon restart and advances through catalogue resolution, verified
runtime/model provisioning, managed `llama-server` startup, empirical qualification,
and scheduler `READY` state. Runtime versions install side-by-side and trusted
model bytes are content-addressed by SHA-256 so catalogue refresh cannot replace
files underneath a running deployment.

See `docs/local-ai-install-pipeline.md`.

## M26 autonomous agent worker

M26 adds the first general-purpose autonomous Task execution loop. Each Task
attempt owns a durable `agent_worker_run` plus an ordered immutable step journal.
The worker compiles bounded authoritative context, asks the scheduler for an
eligible ModelDeployment or external AgentRuntime, dispatches a canonical
`AgentRequest`, validates the resulting `AgentResponse`, and handles exactly one
bounded proposal at a time.

Safety boundaries remain outside the model:

- Autonomous routing protects subscription allowance and monetary spend by
  default; local/proven-$0 capacity is preferred and paid/subscription use needs
  an explicit route-policy change.
- The worker cannot issue itself a CapabilityLease. Tool execution requires an
  existing active Task-bound lease for the worker principal; otherwise the Task
  enters an authority/approval wait.
- OBSERVE/READ/sandbox proposals return through ToolGateway and immutable
  Observation evidence. MUTATE/EXTERNAL_SEND proposals use OperationCoordinator
  and stop at the independent observation/verification boundary before commit.
- Delegation creates a real child Task plus a hard dependency. Replan and
  escalation counts are hard-bounded; failed candidates are explicitly excluded
  from subsequent routing so escalation cannot immediately select the same
  deployment again.
- `complete` is a proposal only. V0 completion may be closed by a separate system
  verifier as declaration-level evidence; completion contracts requiring V1-V5
  remain blocked until the corresponding independent assurance path is supplied.
- Active worker runs lost across daemon restart become `interrupted`, and the
  Task/attempt is blocked/interrupted rather than blindly replaying a possibly
  billable inference call or side effect.
- Verified checkpoints are compiled ahead of historical continuation state so
  verified progress survives bounded retries/replanning without trusting a full
  transcript.

See `docs/agent-worker.md`.


## M27 durable budget enforcement

M27 turns paid/subscription/provider-managed routing into a durable capacity
reservation rather than a boolean permission. Budget reservations consume capacity
through their entire parent-account chain, bind to exactly one protected dispatch,
and are conservatively committed once an external call has been attempted. Active
work is protected from expiry both during candidate selection and again inside the
expiry transaction.

## M28 independent V1-V5 assurance

M28 supplies the independent evidence path that M26 intentionally left open. The
Assurance service evaluates deterministic checks, immutable Artifact integrity,
fresh direct Observations, independent integration paths, multi-layer evidence and,
for V5, human Approver/Admin acceptance bound to the exact evidence hash. A second
LLM agreeing never raises the level. Passed Task verification still completes only
through Checkpoint + `CompleteVerified`; mutation verification still commits only
through OperationCoordinator. Resolved verifications are resumable after a crash so
verification cannot be durably PASS while finalization is stranded.

See `docs/budgets-and-assurance.md`.

## M29 provider, harness and SaaS integration registries

M29 turns OnePane's integration points into explicit, extensible registries while
preserving the same authority boundary used by the core runtime. The built-in
catalog currently exposes 27 provider presets, 33 external harness/runtime presets,
and 20 SaaS connectors.

- Cloud inference includes OpenAI, Anthropic, Gemini, Vertex AI, Azure OpenAI,
  Amazon Bedrock, xAI, Mistral, Groq, DeepSeek, OpenRouter, Together, Fireworks,
  Cerebras, Cohere, Perplexity, Cloudflare Workers AI, NVIDIA NIM, SambaNova,
  Hugging Face, Alibaba Model Studio, Nebius, Moonshot/Kimi, MiniMax, OmniRoute,
  ChatGPT-plan fallback, and custom OpenAI-compatible endpoints. Most providers
  share the hardened OpenAI-compatible transport; Anthropic has a native Messages
  adapter. Fixed providers pin credential-bearing requests to known provider hosts.
- External harness presets cover Hermes, Microsoft Agent Framework, Google ADK,
  Strands, OpenAI Agents SDK, LangGraph/Deep Agents, CrewAI, Pydantic AI,
  LlamaIndex, OpenHands, goose, OpenCode, OpenClaw, Claude Code, Codex CLI, Gemini
  CLI, Aider, Qwen Code, Kilo Code, Pi, Dify, Flowise, Langflow, n8n and common IDE
  agents. Autonomous eligibility requires a OnePane proposal-only bridge; opaque
  IDE/runtime registrations remain unmanaged and non-schedulable.
- SaaS plugins include Gmail, Google Calendar/Drive, Outlook Mail/Calendar,
  OneDrive, SharePoint, Teams, GitHub, GitLab, Slack, Discord, Notion, Linear,
  Dropbox, Asana, Jira, Confluence, HubSpot and Todoist. Each plugin exposes
  separate read, mutate, and where applicable external-send capabilities through
  ToolGateway. Send endpoints cannot be reached through the lower-risk mutate/read
  tools, and POST-based read APIs are explicitly allow-listed.
- Provider, plugin and external-runtime secrets use separate Vault namespaces.
  The generic HTTP adapters never accept caller-supplied origins, reject redirects
  for credential-bearing requests, and prevent reserved authentication-header
  override. Paid/provider-managed inference still requires M27 budget capacity.

See `docs/integrations.md`.

## M30 durable messaging gateway

M30 adds a Hermes-style messaging gateway for task and Routine notifications. The
built-in catalog covers 32 current messaging surfaces, including Discord, Telegram,
Slack, Google Chat, WhatsApp/WhatsApp Cloud, Signal, Matrix, Mattermost, Email, SMS,
Teams, LINE, ntfy, SimpleX, iMessage bridges, QQ/Yuanbao, Feishu/WeCom/Weixin,
Open WebUI, Webhooks, Raft, IRC and Buzz. Native HTTP/SMTP delivery is used where
appropriate; local-protocol integrations can use the normalized OnePane Relay mode.

Notification rules consume the append-only Event Ledger, create durable idempotent
deliveries, and route them to named GatewayTargets with optional thread/topic IDs.
Routine notification bindings fire from verified `task.completed` events associated
with that Routine's occurrences. Every external send runs through a one-use
CapabilityLease and OperationCoordinator, then commits only after V1 acceptance-receipt
verification. Unknown transport outcomes and restart-interrupted sends are never
automatically replayed. Fixed gateway credentials are host-pinned and use the
independent `gateway/<platform>/<kind>` Vault namespace.

See `docs/messaging-gateway.md`.

## Release-candidate verification

On a network-connected Linux build host, run the complete release gate before
installing or publishing a candidate:

```bash
scripts/verify-release-candidate.sh
```

It runs every structural validator, shell/Python syntax checks, verifies the pinned
Go module graph, executes the full unit and integration suites plus `go vet` and the
race detector, then builds checksum-protected Linux amd64/arm64 release artifacts.

## M31 Team Mode — human + multi-agent deliberation

M31 adds a third first-class orchestration pattern alongside normal Supervisor execution and the planned formal Council protocol. A reusable Team contains human, agent and supervisor roles; a TeamSession binds that team to one Task and provides a durable shared transcript before execution begins.

A Team Task is plan-gated: the Task cannot become READY or START until an authenticated human accepts a versioned TeamPlan. Blocking/critical objections remain durable and require explicit human resolution or recorded risk acceptance rather than being collapsed into synthetic consensus. Requested deliberation rounds fan out independently to all or selected non-human members; each role is routed through the ordinary scheduler and may use a different ModelDeployment or external AgentRuntime. Deliberation permits reasoning only—no tool/delegate side effects—and paid/subscription routes retain M27 BudgetReservation enforcement.

The accepted TeamPlan becomes required authoritative AgentWorker context. Reopening Team deliberation pauses the existing Task/attempt and later resumes it after a revised plan is accepted, preserving checkpoints and other verified progress.

See `docs/team-mode.md`.

## M32 provider/harness Bot Runtime

M32 adds a persistent conversational Bot surface without weakening OnePane's autonomous execution boundary. A Bot can represent a provider-hosted bot (for example a ChatGPT GPT or Grok Bot), a harness-native bot/profile (Hermes Bot Mode), or a relay-backed conversational runtime such as Dify/Flowise/n8n/Open WebUI.

- Hermes Bot Mode uses the authenticated Hermes Sessions API and preserves the selected Hermes profile plus its long-lived remote `Bot Chat`. Hermes keeps its native memory, skills, routines and tools.
- Hosted provider Bots remain on the provider-supported surface when no external Bot-session API exists. OnePane stores a pinned launch URL rather than scraping browser sessions or pretending API compatibility.
- Relay-backed Bots implement OnePane Bot Protocol v1 and keep their native remote conversation state.
- Bot chat is human-interactive and `native_runtime` authority by default. It is never admitted as an autonomous OnePane Scheduler candidate merely because it can use tools.
- Bot turns are durable and idempotent. A network-uncertain result becomes `unknown` and is not automatically replayed because the remote Bot may already have acted.
- Bot credentials use the independent `bot/<preset>/<kind>` Vault namespace. Hermes human Bot chat deliberately does not reuse `agent-runtime/hermes/*` proposal-bridge credentials.
- The embedded WebUI now includes a minimal Bots roster/chat pane: API/relay Bots chat directly in OnePane; hosted-only Bots open the vendor-supported surface.

See `docs/bot-runtime.md`.

## M33 HarnessNode federation + LAN discovery

M33 completes the multi-computer inference mesh that was reserved by the original
`HarnessNode`/`ModelDeployment.node_id` schema. Each OnePane node can originate its
own Tasks; for a given Task, the origin node remains authoritative for Task/Plan,
Policy, CapabilityLeases, budgets, Vault secrets, ToolGateway and Verification.
Paired peers supply bounded inference capacity only.

- LAN discovery is automatic by default using a OnePane IPv4 multicast discovery
  beacon. Discovery only creates/refreshes `discovered` nodes; it never grants
  trust or scheduler eligibility.
- Pairing is explicit and two-sided. Both operators confirm the same short pairing
  code before either node becomes `paired`. Federation uses a node-bound Ed25519
  self-signed certificate, certificate-fingerprint pinning and TLS 1.3 client
  certificates for all post-pairing traffic.
- Paired nodes exchange short-lived heartbeats/capability manifests. Only local,
  ready, non-provider-backed model deployments are exported. Their compatibility
  profiles are projected into local scheduler-visible remote deployments.
- Remote inference is one hop only. The peer executes the requested deployment only
  when it is physically local to that peer; it cannot recursively federate or use
  that peer's cloud-provider credentials. The origin stores the resulting inference
  Artifact and retains the normal trust/data-residency semantics.
- Unknown/stale peers are removed from scheduling by marking imported deployments
  unavailable. Revocation immediately disables all imported deployments for that
  peer.
- Inbound remote inference is idempotent by `(peer_node_id, remote_request_id)` and
  stores a durable receipt, so an uncertain network result is not blindly replayed.

Operator API:

```text
GET  /v1/nodes
GET  /v1/node-pairings
POST /v1/nodes/{nodeID}/pair
POST /v1/nodes/{nodeID}/pair/confirm
POST /v1/nodes/{nodeID}/revoke
GET  /v1/nodes/{nodeID}/capabilities
```

The dedicated federation listener is separate from the control-plane Web/API
listener and is configured under `node_federation`. See `docs/node-federation.md`.



## M34 remote model deployment + model-pool hot swapping

M34 makes paired nodes remotely manageable model hosts while preserving the M33
origin/peer authority split. Pairing alone still grants only inference; each target
node must explicitly enable remote model management for a specific peer.

- A target node detects its own hardware and computes model/quantization fit locally.
  The origin cannot forge the target hardware profile or force an unfit selection.
- Remote installs resolve only through the target node's trusted signed Local AI
  catalogue, then use the existing durable install -> verify -> qualify -> READY
  pipeline. The resulting deployment is advertised back through ordinary M33
  capability heartbeats.
- `local_ai.model_pool_path` can point at a large local disk or mounted NAS/share;
  model files remain persistent there while VRAM/RAM is treated as a cache.
- Managed runtimes are lazy-loaded for both local and federated inference. Active
  requests are marked busy and cannot be reaped or evicted. Under VRAM pressure,
  OnePane stops least-recently-used healthy/idle managed runtimes until the newly
  selected model fits within `residency_headroom_pct`.
- Remote install jobs are bound to the paired peer that created them, preventing a
  model-management peer from reading unrelated local or other-peer install jobs.

Operator API:

```text
GET  /v1/nodes/{nodeID}/model-management
POST /v1/nodes/{nodeID}/model-management
GET  /v1/nodes/{nodeID}/remote-model-management
POST /v1/nodes/{nodeID}/models/recommendations
POST /v1/nodes/{nodeID}/models/install
GET  /v1/nodes/{nodeID}/model-install-jobs/{jobID}
```

See `docs/remote-model-management.md`.


## M35 hardware-portable model placement + qualification Testbed

M35 makes managed Local AI placement device-aware instead of treating accelerator
memory as one pooled number. A recommendation now carries an explicit placement
plan that is persisted through installation, qualification, residency management
and the model spec sheet.

- CPU is a first-class execution target. Accelerator placement is backend-neutral
  and supports CUDA, ROCm/HIP, Vulkan, SYCL, Metal, OpenCL, MUSA and Ascend/CANN when
  the signed runtime catalogue contains a compatible managed runtime.
- Placement modes are `single_device`, `layer_sharded`, `row_sharded`,
  `tensor_sharded`, `cpu_offload` and `cpu_only`. Heterogeneous devices are never
  silently added into one VRAM bucket. Sharding is limited to devices sharing a
  compatible backend; tensor sharding is explicitly experimental.
- The llama.cpp managed launcher receives the concrete runtime device list and
  split plan. Residency accounting and pressure eviction operate against those
  exact devices, so another model can remain hot on an unaffected GPU.
- llmfit is an optional node-local advisory source. Its model-fit, quantization,
  context, disk, capability and performance metadata is retained with provenance
  as an estimate/benchmark input; it never overrides OnePane qualification or the
  signed catalogue.
- Successful automated qualification creates a durable model spec sheet in
  `pending` admission state. The sheet keeps catalogue claims, llmfit advisory
  data, exact placement, verified context and measured qualification results
  separately.
- A manual Testbed runs only against the exact installed deployment and placement.
  Synthetic tool probes exercise tool-call formatting but never enter ToolGateway
  and therefore carry no side-effect authority.
- Normal managed-model scheduling and federation export remain blocked until a
  completed Testbed session for the exact current hardware/placement is followed by
  human `accepted` or `restricted` admission. Restrictions can cap verified scheduling context, deny capability IDs
  and disable tool callbacks. Requalification onto a different hardware/placement
  resets admission to `pending`.
- The same spec-sheet, Testbed and admission workflow is available across an M33
  paired node through the separately granted M34 remote-model-management channel;
  the target executes the Testbed locally while the master UI/API remains the
  operator surface.

Operator API includes:

```text
GET  /v1/model-deployments/{deploymentID}/spec-sheet
POST /v1/model-deployments/{deploymentID}/testbed/sessions
GET  /v1/model-testbed/{sessionID}
GET  /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/complete
POST /v1/model-deployments/{deploymentID}/admission

GET  /v1/nodes/{nodeID}/models/{deploymentID}/spec-sheet
POST /v1/nodes/{nodeID}/models/{deploymentID}/testbed/sessions
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/complete
POST /v1/nodes/{nodeID}/models/{deploymentID}/admission
```

See `docs/model-placement-testbed.md`.

