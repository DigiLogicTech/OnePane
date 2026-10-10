# RC11 — remaining development and release gates

**Source of truth:** [draft integration PR #18](https://github.com/DigiLogicTech/OnePane/pull/18) and linked GitHub issues below. **Protected baseline:** `release/alpha3.3-rc10-msi-qa`. This file is a living execution plan, not evidence that any unchecked acceptance has passed.

## Remaining development work (complete before the consolidated QA build)

| Order | Workstream | Tracking | Outstanding release-level acceptance |
| --- | --- | --- | --- |
| 1 | Independent Workspace sandbox/runtime execution | [RC11-01 #19](https://github.com/DigiLogicTech/OnePane/issues/19) | Real rootless Node / two isolated Workspaces; trustworthy observed state |
| 2 | Toolchains, files, Git, terminal | [RC11-02 #20](https://github.com/DigiLogicTech/OnePane/issues/20) | Pinned image/toolchain, Godot sample build, safe mutations, restart |
| 3 | Selective Workspace collaboration | [RC11-03 #21](https://github.com/DigiLogicTech/OnePane/issues/21) | Explicit runtime service links, lifecycle and revocation, safe previews |
| 4 | Project Library and scoped evidence | [RC11-04 #22](https://github.com/DigiLogicTech/OnePane/issues/22) | Document ingestion, indexing/search, selected versions and access tests |
| 5 | Autonomous Project Orchestrator | [RC11-05 #23](https://github.com/DigiLogicTech/OnePane/issues/23) | Objective decomposition, dependency control, recovery approval, resource waits and CPU-only execution |
| 6 | Model lifecycle and routing | [RC11-06 #24](https://github.com/DigiLogicTech/OnePane/issues/24) | Agent Check residency, Colibri/llama.cpp offload, OmniRoute policy, existing-model adoption |
| 7 | Research Council | [RC11-07 #25](https://github.com/DigiLogicTech/OnePane/issues/25) | Strict pinned seats and isolation, multiple rounds, resumable rate limits, immutable provenance |
| 8 | Operations/Evidence/Nodes/Tasks | [RC11-08 #26](https://github.com/DigiLogicTech/OnePane/issues/26) | Real operator views and a traceable Task → verification → artifact history |
| 9 | UI and legacy consolidation | [RC11-09 #27](https://github.com/DigiLogicTech/OnePane/issues/27) | Complete canonical navigation, Inspector/drawer responsiveness, themes and safe source cleanup |
| 10 | Security, Vault, governance | [RC11-10 #28](https://github.com/DigiLogicTech/OnePane/issues/28) | Enforced least-privilege, no Workspace leaks, safe secret grants and adversarial tests |
| 11 | **Debug & QA Diagnostics Centre** | **[RC11-13 #85](https://github.com/DigiLogicTech/OnePane/issues/85)** | Time-bounded opt-in capture, correlated diagnostics, on-device sanitized report export, privacy/disk/performance tests |

These are interdependent workstreams, not a promise that each must be completed in strict numeric order. An implementation with green Go CI can still fail physical Node or installed Windows/Ubuntu QA. The Debug Centre must be complete before operator testing to make intermittent failures diagnosable.

## Mandatory user-directed review sequence — before packaging

**[RC11-14 #86](https://github.com/DigiLogicTech/OnePane/issues/86)** is now a **release blocker**, not an optional document review.

1. **First full code review:** inspect *all* source, legacy/recovery assets, workflows, migrations and actual feature wiring. Deliver implementation map, defects, risks and evidence; **user reviews the findings**.
2. **Vision alignment interview:** assistant asks targeted questions in manageable rounds from those findings. Reconfirm Projects and independently sandboxed Workspaces, selective cross-Workspace sharing, local-first autonomy on limited hardware, models, research, tool capabilities and UX. Record decisions and receive **explicit user approval**.
3. **Second code review and realignment:** map the agreed vision to code, remedy approved gaps, re-run relevant tests, document what is and is not verified, and receive **second user approval**.

**Do not cut, publish or present an RC11 consolidated QA installer before all three gates are signed off.** The operator should be able to challenge both the implementation **and whether it implements the right product**. Feature engineering and existing CI may continue before these reviews.

## Final packaging and QA

- **[RC11-11 #29](https://github.com/DigiLogicTech/OnePane/issues/29):** only after the three reviews; build and validate Windows MSI and Ubuntu package with RC10 upgrade, new installs, data/model/Project preservation, temp/cache controls, rollback and checksum/provenance.
- **[RC11-12 #30](https://github.com/DigiLogicTech/OnePane/issues/30):** complete end-to-end application/Node/Research/Model/Debug regressions, immutable QA artifacts, explicit unsupported-environment list and operator test checklist.

**No default cloud inference fallback; no invisible Project Workspace authority escalation; no physical test claim without physical execution.** The RC10 release branch remains untouched until operator acceptance.

## Rootless two-Workspace physical acceptance expansion (RC11-01/02)

The separately approved, digest-pinned **rootless Node** integration test `TestRealRootlessWorkspaceIsolation` now checks more than an initial stop:

- World and Story must start as distinct real OCI containers with independently inspected rootless isolation, private internal networks and writable Workspace roots.
- Stopping **World** must leave **Story** executing and reading its private file.
- Restarting World's **exact original runtime spec** must preserve its independently verified OCI identity and its pre-existing file.
- Rebuilding World's **versioned command spec** must replace only its originally verified owned container identity, preserve World's writable file, and leave Story's original process, file and isolated network unchanged.
- Both Workspaces must stop cleanly without image pruning, uncontrolled host-shell commands, or deletion of existing user roots. The explicit trusted test uses only fresh generated OCI names and disposable temporary directories.

This added acceptance is source-compiled by integration CI, but physical results remain **unverified** while the trusted self-hosted rootless job is **skipped**. The physical workflow still requires a deliberate repository administrator-provided digest-pinned pre-pulled image and verified unprivileged Podman identity. Do not substitute a hosted/unit simulation or a green source-compilation run for the actual rootless acceptance check. After the approved Node/image become available, run the existing opt-in workflow and record the actual result before closing #19/#20 or packaging RC11.

## RC11-02 — governed Godot 4 toolchain execution (2026-10-10)

A dedicated `project.app.godot.build` sandbox tool is now registered with the existing `project.app.execute` capability and `ActionExecuteSandboxed` authority/lease path. It is **not** an uncontrolled package installer, a host command or a model-controlled Godot CLI. It accepts only `runtime_id`, `application_id`, `action` (`import` or `run`) and optional bounded `timeout_seconds`; unknown JSON keys and all executable/image/path/environment/mount/network overrides are rejected.

Commands are immutable argument vectors, never a shell:
- `import` → `godot --headless --path /workspace --editor --import`
- `run` → `godot --headless --path /workspace --quit-after 60`

Godot must already exist **inside the explicitly registered immutable OCI image**. The adapter probes rootless OCI, checks the owned runtime + application, requires running/observed `IsolationVerified` with exact single allowed writable Workspace mount and valid spec hash, and bounds execution by caller cancellation or 600 s default / 3600 s maximum. Nonzero scene/import exit status is preserved as an observed **build failure** (not labelled build success), and OCI timeouts/errors fail the Tool invocation. A successful command explicitly reports `artifact_verified=false`: it does **not** publish or externally attest to an artifact. A distinct Task-owned `project.app.files.publish` invocation remains responsible for independently hashed, immutable Library publication and grant controls.

Unit tests cover the exact arguments, forbidden injection parameters/paths, unsupported actions, rootful/extra-bind denial, failed Godot run and deadlines. The opt-in, trusted self-hosted Godot physical acceptance now exercises the **same adapter action path** instead of bypassing it with raw `ExecContainer`, followed by independent host SHA-256 readback of the generated file. Physical CI remains explicitly skipped until a repo administrator pre-pulls and authorises a compatible digest-pinned Godot OCI image and rootless runner; these green source tests cannot claim installed Godot acceptance.

**Still open:** real physical Godot run, package/image preflight, Task Gateway/capability lease live acceptance, publication to immutable Project Library through the real publisher, two-Workspace controlled grants, error/retry/rollback and high-confidence postcondition probe. Do not close #20 or build RC11 yet; user-mandated #86 reviews remain release blockers.

## General-purpose, multi-MiB Task outputs — source and QA scope

The `project.app.files.publish` Tool now supports **opt-in** `publish_large`
for arbitrary generated files from a verified, owned, running rootless OCI
Workspace. This fixes the previous 256 KiB *per-file* limitation without
raising the command-output limit or trusting CLI output truncation. The
**existing `publish` action is unchanged**.

- A fixed OnePane-owned in-container Python reader uses O_NOFOLLOW on every
  directory and the file. It returns a full-file SHA-256 and stable file
  fingerprint, followed by 512 KiB chunks encoded as bounded receipts, then a
  second full-file hash/fingerprint. The host independently verifies every
  chunk and final SHA-256 before publication.
- Maximum file size is **32 MiB** for now, with a **600-second default**
  transfer timeout and a configurable maximum of 1200 seconds, still subject
  to the shorter Task cancellation deadline. Larger
  files, changed sources, malformed/chopped OCI replies, wrong digests,
  unsafe paths, symlinks and extra host mounts fail closed.
- A second authority boundary rechecks the active Task and Attempt, Project,
  canonical Workspace, running on-Node runtime/application and worker before
  the artifact store writes any new bytes. The existing content-addressed
  blob store, immutable Library versions, duplicate suppression, unknown-
  outcome recovery and revoked/cross-Workspace access restrictions remain
  authoritative. No host temp staging or additional network listener is
  introduced.
- Regression suite includes real Python no-follow chunk reads, multi-MiB
  source mutation during transfer, corrupt chunk/hash, same Task ownership,
  missing Attempt, foreign OCI mounts, immutable blob readback, same-hash
  retries, new-version handling and cross-Workspace access denial.

**Still open:** external physical Ubuntu rootless Node smoke must run on an
explicitly trusted host and digest-pinned Python-capable image. 32 MiB memory
assembly is unsuitable for multi-GiB models, large videos/game builds or
low-RAM servers; streaming directly into a bounded, permission-checked blob
sink with durable chunk manifests, disk quota, resumability and recovery is
future work. Do not claim unlimited artifact support or close #20 yet.

## RC11-03 — expiring selective Workspace artifact links (2026-10-10)

Directional artifact links from one active Project Workspace to another can now
carry an operator-defined `expires_at_ms` deadline (Unix milliseconds).
Existing links keep a null, perpetual expiry for backward compatibility.
Create and toggle/update APIs may set a bounded future expiry; renewal or
explicit clearance requires the current revision and authorised Project writer.
An expired link remains in historical metadata as `enabled: true,
expired: true`, never silently turning into a fresh grant.

The same effective expiry check is enforced before new artifact publications,
publication listing, Workspace Library asset enumeration, immutable version
listing/resolution, and verified Workspace output reads. Both endpoints and
the containing Project must still be active. Stale expiry renewals are rejected.
The underlying immutable blob/version and provenance do not get deleted on
expiry; re-enabling/renewing is an explicit operator act.

This is **bounded artifact exchange**, not rootless runtime-to-runtime service
connectivity, desktop preview forwarding, a transitively propagated Library
grant, or a lease to use another Workspace's secrets/tools. RC11-03 #21 remains
open for separately governed service connectivity and installed UI acceptance.

## RC11-03 — scoped service-link authorisation groundwork (2026-10-10)

Service connections are a **separate, deliberately narrower** control-plane
grant from immutable Library artifact links. Schema migration 0046 and the
operator-authenticated Workspace service-link API introduce a revocable,
directional approval pinned to:

- One active Project, exact source and target canonical Workspaces.
- One previously verified **HTTP** endpoint belonging to a running, owned OCI
  application in the source Workspace. No model-selected host/port/URL.
- The application's revision, endpoint's revision, container spec fingerprint
  and verification identifier; rebuilding/re-verifying requires explicit
  operator reapproval.
- One fixed, path-only HTTP resource, e.g. `/health`; no query strings,
  URL-escaped traversal, remote hosts, arbitrary methods, headers or secrets.
- A mandatory expiry at most 30 days ahead, an optimistic revision for every
  approval/revocation, and event provenance. Only active *human* operators can
  approve or renew.

A trusted internal resolver can check whether a particular target Workspace
still has an unexpired grant pointing to the exact currently verified loopback
route. It rejects stale runtime/endpoint state, changed verified identities,
inactive Projects/Workspaces and foreign/transitive consumers. **It does not
send requests or publish an address to the browser or an Agent.**

**Remaining before service connectivity is genuinely usable:** a trusted
Node-local mediation layer must independently re-inspect the actual rootless
container and its approved loopback mapping at request time, strongly bind
authorization to the requesting Task/Workspace, limit method/path/headers/
response size and duration, reject redirects, enforce immediate revocation,
and prove it on the physical Ubuntu Node with two isolated Workspaces.
Do not connect OCI networks, expose an unauthenticated endpoint, or describe
these metadata-only grants as functional service forwarding. The absence of
physical acceptance remains a release blocker under #19 and #21.

## RC11-03 — bounded Node-local health probe (2026-10-10)

`internal/workspacebroker` implements a real, **internal-only** HTTP
readiness probe against an approved directional Workspace service grant. It
uses the existing pinned grant and independently verified loopback route, then
freshly inspects the source's rootless OCI container on the exact designated
local Node. The container must be running, privately networked, mounted to
the single canonical Workspace root, in the expected security profile, with
matching application/spec and matching loopback port. Any mismatch denies
access before opening a socket.

The probe permits only a hardcoded small set of `/health`, `/healthz`,
`/ready`, `/readyz` and `/status` paths, even if the operator-approved
record names something else. It sends only GET, with no caller-controlled
headers/body/URL, no proxy or DNS, no redirects, a six-second deadline,
two-probe maximum concurrency and a 64 KiB maximum JSON response.
The body is reduced to a known ready/unready value; unrelated fields,
untrusted upstream error text, cookies, Authorization headers, raw content,
Node/host addresses and OCI identifiers are never surfaced.

Before returning the normalized result, the broker checks that the exact
source container still exists and has the same observed identity, plus that
the Workspace grant revision, current route and pinned verification have not
changed. Unit fixtures cover live local HTTP, wrong target/Node, extra mount,
public listener, stale verification, changes during the request, redirects,
oversized content and attempted credential exposure.

**Important scope limitation:** The broker is not exposed via public API,
WebUI, Agent Tool or automated Task execution. Its caller still needs a
Gateway-verified target Workspace/Task identity and per-call tool lease.
Rootless physical verification of actual Podman runtime/process ownership and
the route-to-socket binding remains outstanding. This is **not** general
Workspace-to-Workspace HTTP/TCP communication and is not a pass for #21.

## RC11-03 — opt-in physical Workspace health mediation acceptance

A dedicated `TestRealRootlessWorkspaceHealthBroker` now combines real
rootless Podman with the *actual* Workspace service grant resolver and
Node-local health broker. The disposable QA scenario creates two explicitly
named Workspaces (World source and Story client) and an ungranted Research
Workspace; starts a pinned Python HTTP daemon inside an independently
verified, read-only-root OCI container with a single managed Workspace bind;
confirms private networking and a random loopback-only published port;
persists a V2-verified route from the real OCI observation; then grants only
Story access to the fixed `/health` service resource.

When actually run, acceptance requires a normalized `healthy` receipt with
no raw server secrets or cookies, a denial to ungranted Research, and
immediate denial following operator revocation. The approved service is
stopped and only its precisely owned disposable container/network are
removed, without pruning the image or touching existing User Workspaces.

The physical workflow job is gated behind the existing administrator-defined
`ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE` immutable digest and trusted
unprivileged self-hosted Podman user, and is **skipped by default**.
It runs only on a push to the RC11 integration branch, not an untrusted pull
request. CI compilation and synthetic success remain distinct from actual
physical execution. Even physical success does not authorise a general
network proxy or Agent Tool; Task Gateway binding/lease, continuous
revalidation and rigorous UI/installer acceptance are still required.

## RC11-03 — Task Gateway–bound service health probe admission

A narrowly scoped `project.workspace.service.health@1` tool adapter is now
implemented as a **disabled, unregistered integration seam**. Production
bootstrap does not register this tool or enable its broker. A future trusted
Node start-up may do so *only after* physical acceptance and a separate
authorisation review; declaring a service link in the UI cannot turn it on.

The explicit integration code uses the existing Tool Gateway to consume a
per-invocation capability lease for the exact
`project_workspace_service_link:<link_id>` resource, and then verifies the
stored tool invocation's running state, canonical input hash, matching
Tool/Adapter identity, tenant and principal membership, and exact persisted
running Task/Attempt/Agent Worker incarnation. Critically, the running Task
must belong to the **target named Project Workspace** of the active
operator-approved link. A model cannot supply Project ID, Workspace ID,
URL, host, port or HTTP headers through the tool input.

The same durable invocation and Workspace authority constraints are checked
again after the 6-second, declassified Node-local health probe and before
returning any status. A revoked/expired grant, completed/cancelled Task,
revoked principal or changed Attempt/Worker blocks the result. Tests exercise
the *actual* `tool.Gateway` and `authority.Service` lease consumption, not
just an adapter mock, while using a fake probe to avoid claiming physical
verification.

**Still blocked:** actual provisioning of the registry entry, enabling the
adapter in bootstrap, granting agent profiles permission to use it, trusted
Node identity and remote broker routing, and physical Podman multi-Workspace
acceptance. Keep all of these disabled until approval and #19/#21/#86 gates.

## RC11-04 — atomic, human-reviewed Project Task dependency graphs

The Project Orchestrator now supports an explicit operator-approved plan
consisting of 1..32 named Tasks in active, canonical Workspaces of **one**
Project, with up to 8 *hard/all* prerequisite links per Task. Graph validation
rejects unknown nodes, duplicate/self edges, cyclic plans, inactive/foreign
Workspaces, oversized objectives and nonhuman actors.

`POST /v1/projects/{projectID}/orchestrator/task-graphs` requires the
authenticated operator's `task.write` authority, and
`GET /v1/projects/{projectID}/orchestrator/task-graphs/{graphID}`
requires `project.read`. The Project Task graph is neither invented by a
reasoning model nor committed by a read-only Assistant turn. The Project ID
and principal are obtained from the authenticated API session, not copied
from the submitted graph.

Migration 0047 records a Project-scoped idempotency key, canonical SHA-256
plan digest, graph-node mapping and provenance. A **single write
transaction** stores the graph, each Task/Event/Outbox admission message,
and all dependency edges. Repeating the same key+plan returns the original
Task graph; changed content under the same key rejects without creating
more Tasks. Different Workspaces stay filesystem-, secret- and network-
isolated: completion state is the only shared ordering signal.

Existing autonomous Agent Worker admission for newly created Tasks already
waited on hard predecessors. RC11 now also rejects ready-to-start work
when any hard predecessor is not complete or has been archived. That
condition is rechecked inside the Task+Attempt **write transaction**,
regardless of what set the Task to ready. A blocked successor must not
consume a model lease, start an Attempt, or misreport parallel work as
success. Independent DAG branches can continue; failed predecessors
remain visibly blocking instead of silent fallback.

Graph submission itself requires no inference or cloud provider, and
canonical named Workspace Tasks default to disallowing remote model
use unless separately, explicitly approved under existing Workspace
policy. **Not yet delivered:** automatic DAG planning/synthesis from AI,
graph-specific UI, terminal-failure policy/recovery actions and physical
CPU-only completion acceptance. These remain separate governed work, not
inferred from passing source tests.

## RC11-04 — read-only Task graph recovery assessment

The operator's Project Task graph API now exposes a bounded
`GET /v1/projects/{projectID}/orchestrator/task-graphs` inventory and
enhanced scoped per-graph reads. Each graph read evaluates Task states,
archives and every hard prerequisite from one consistent SQLite read
transaction, and returns an advisory, node-by-node readiness classification.

- `admission_eligible`: existing Task Worker may attempt governed admission.
  It does not imply an available CPU model, approved toolchain or proof of
  physical execution.
- `waiting_prerequisites`: one or more hard predecessors still need
  independently verified completion.
- `waiting_resources`: Task is already suspended; inspect the persisted
  Worker continuation (local model/toolchain/approval) rather than silently
  restarting a new Attempt.
- `running` and `verifying`: existing execution/assurance is underway.
- `needs_attention`: failed, cancelled, blocked or archived Task; affected
  downstream Task; unreviewed extra hard dependency; or an execution that
  already started before its predecessor regressed.
- `complete`: only the persisted completed Task state, not a model claim.

Failed/intervened-on prerequisites propagate through the reviewed DAG;
unrelated parallel branches retain admission eligibility. Unreviewed edges
are displayed only as a generic `unscoped_dependency` flag, never revealing
another Task's identifier. The graph's aggregate status and counts remain
informational: the Task Gateway and Worker still revalidate dependencies in
the write transaction before starting any Attempt.

The graph evaluator **never** auto-retries a possibly side-effecting Task,
silently replaces an approved model, skips a prerequisite, or claims a
failed Node has recovered. Human-led reconciliation and safe retry policies
are separate upcoming work, together with Project Orchestrator GUI support.

## RC11-04 — operator Task DAG console (UI)

The Workspace Development surface now includes Project Task graphs, a Project-scoped console showing bounded graph inventory, persisted readiness and next_action per node, exact approved dependency keys, attention and progress counters. Statuses use the canonical backend; no browser-generated state is treated as execution evidence. The panel scales with the existing Workspace Development layout and shares theme tokens.

A human operator can compose up to 32 explicitly named steps across registered, active canonical Project Workspaces, express hard predecessor keys, and supply a Project-scoped idempotency key. The UI requires an explicit approval checkbox and warns that submitting a graph can enqueue runnable Tasks immediately. The backend revalidates the authenticated human, DAG topology and Workspace isolation. Graph submission is not an arbitrary model tool and does not grant remote inference, host files, Vault or network access.

The panel handles list refresh failures without wrongly telling the operator that a successfully committed graph was rejected. Node regression checks verify dependency input, approval messaging, mounting, canonical Workspace IDs and HTML escaping. Future iteration should add direct Task/open-in-Inspector links, richer visual DAG editing, controlled recovery actions and Project-level navigation.

## RC11-04 — CPU-only / CPU-preferred autonomous Task placement

Named Workspace Tasks can now persist a reviewed `onepane_routing.compute_preference`
with one of `auto`, `prefer_cpu`, `cpu_only`, `prefer_gpu` and
`gpu_only`. Invalid or type-confused values are rejected before any Task
is queued. Older named Workspace Tasks with no compute preference default
to `auto`, without enabling remote/cloud inference.

On admission the Agent Worker snapshots the exact Task compute preference
into its durable Worker route policy. Scheduler routing consumes that exact
setting at each step; `cpu_only` excludes candidates not actually declared
CPU and `prefer_cpu` scores eligible CPU candidates more favourably.
An incompatible model pin is **not** replaced to satisfy CPU routing.
No new model, cloud destination or budget authority can arise from a compute
preference. Child delegations inherit the parent's complete canonical
`onepane_routing` policy, including CPU and remote-model constraints.

A missing qualified local CPU model is treated as a persisted, bounded
resource wait on the *same* Task/Attempt, with an explicit reason and retry
deadline. A permanently incompatible or inadequate candidate remains
blocked for operator intervention, not silently substituted. Existing
Research Council pinned-seat/fallback guarantees are unchanged.

Per-step placement is now available in the operator-approved Project Task
graph editor. Omitted values canonicalize to `auto` before graph hashing
and atomic Task creation; changing placement under an existing idempotency
key conflicts instead of rewriting an existing Task. Graph readback
displays the persisted placement without reporting CPU/GPU hardware
as actually running. Physical end-to-end CPU-only inference, performance,
residency and recovery acceptance remain required separately.

## RC11-04 — bounded, version-selected Workspace evidence metadata

The Project Library can now build a **nontransferable metadata manifest** from
1–16 explicitly selected immutable Library versions using
`POST /v1/projects/{projectID}/workspaces/{workspaceID}/evidence-packets`.
This endpoint authenticates the current Project reader and the canonical
Workspace context. Every exact `asset_id + version` is reauthorized in one
consistent SQLite read transaction against the *current* direct read grant
(latest/pinned) or a hash-matching, enabled and unexpired directional
publication with active Project/source/target Workspaces.

The receipt includes only canonical Project/Workspace identity, immutable
version metadata (name, MIME type, recorded content hash and size), a snapshot
timestamp and a deterministic SHA-256 of the canonical selected metadata.
It excludes storage URIs, artifact IDs, raw document bytes, host paths,
download links and credentials; it does not create a read capability. Future
reads still require separate permission and content-integrity verification,
and revocation affects new selections immediately. The packet itself is not
yet a persisted Research Council evidence attachment.

The Workspace Library UI offers checkboxes only for currently accessible
assets and selects each displayed exact version. A bounded "Build scoped
evidence manifest" button renders the returned metadata as plain text,
never HTML. It cannot silently share the packet with another Workspace,
send data to a model, run a background indexer or widen permissions.
Integrations test cross-Project and cross-Workspace denial, link-only
non-access, exact publication, second-asset denial, expiry/revocation,
archive denial, canonical digest ordering, duplicate and oversized
selection denial and absence of raw storage references.

**Still outstanding:** bytes-level content ingestion/indexing and
permission-rechecked evidence retrieval into isolated, explicitly approved
Research Council/Project Orchestrator contexts, byte verification at
attachment time, immutable provenance records and user-approved use of
selected evidence. Source/unit CI alone is not a substitute for installed
Workspace and physical Node acceptance.

## RC11-04 — bounded verified text retrieval (operator review only)

The existing version-selected Workspace evidence packet now supports a separate
`POST /v1/projects/{projectID}/workspaces/{workspaceID}/evidence-packets/verified-text`
request. It is scoped through the same authenticated canonical Workspace
context, **not** a Project-wide or model-initiated file read. An operator
selects 1–8 exact authorised versions (maximum 64 KiB per item, 256 KiB
combined). Only valid UTF-8 files with supported text-like MIME are eligible.
Binary, oversized, malformed, unknown or unavailable items fail closed.

Retrieval obtains the metadata manifest, resolves and reauthorises every
specific version, calls the managed blob store's SHA-256/size verifier,
checks active immutable artifact provenance, reads into a bounded buffer,
and independently hashes the returned bytes. Finally the entire metadata
selection is reauthorised and its canonical digest compared before the
response is emitted. Any denial returns **no partial document content**.
Revocation can still occur after the final read check; the response is never
a persistent lease or a right to retrieve the same content later.

The Workspace Library uses a distinct explicit "Read verified text"
button, and renders every returned untrusted document **only as plain
text**. No text is passed into an Agent, Task, model, prompt template,
Research Council seat or external provider. The response is marked
`untrusted_library_content`; the user must explicitly opt in to a
future governed Research/Orchestrator attachment workflow.

This is real bounded, hash-verified retrieval, not full-text indexing,
OCR, PDF parsing, persistent embeddings or a claim that the attached
data is a trusted instruction. Further work remains for permission-aware
content indexing, artifact-scale streaming and explicitly approved
Research attachments.

## RC11-04 — scoped, on-demand full-text matching of selected evidence

A separate, read-only Workspace API permits case-insensitive text matching
against **up to eight explicitly selected Library versions**:
`POST /v1/projects/{projectID}/workspaces/{workspaceID}/evidence-packets/search-text`.
It takes a 2–64 character query, authorises and verifies the same bounded
source bytes as the separately implemented verified-text retrieval, then
searches them **in memory**. Every request rechecks source Project,
Workspace, version grants and immutable blob integrity; it cannot discover
unselected assets, search other Workspaces, or auto-attach evidence to an
Agent or Council.

Results contain only maximum 12 bounded snippets, at most three per
version, plus an asset/version/hash citation, Unicode-rune offset and
`untrusted_library_content` provenance marker. The operator UI requires
manual selection and search submission, rendering results with
`textContent`; it neither sends bytes to cloud inference nor executes
document instructions. Tests cover Unicode case matching, query control
characters, result attribution, snippet sizes and truncation.

**This is an on-demand memory scan, NOT a persistent FTS or vector index.**
It deliberately avoids an uncontrolled second plaintext copy of private
Library data. A future persistent search service requires an approved
retention policy, quotas, encryption, revocation-aware query filtering,
index invalidation, ingest format selection and explicit Research evidence
attachment; none is inferred from the availability of this endpoint.

## RC11-04 — uniform managed Library artifact provenance

The existing Workspace version **download** had a weaker post-open check
than its preview and verified evidence APIs: it compared immutable hash,
size and Project, but not the blob's active/quarantined state or owning
tenant Workspace. The download, bounded UTF-8 preview and selected evidence
reader now use one shared, fail-closed predicate: active managed artifact,
exact content hash/size, same Project and same Project tenant Workspace.
Downloads also set `Cache-Control: no-store, private` and `nosniff`, and
bound streaming to the exact authorised immutable version length. Unit
tests deny archived, quarantined, corrupted, foreign-Project, foreign-tenant,
wrong hash/size and missing-scope artifacts.

This is an important consistency hardening, **not** permission to access
another Workspace's Library. The existing effective read grants and
directional publications remain authoritative. Stream errors after the
HTTP response begins are still observable client transfer failures rather
than a transactional rollback; stronger descriptor-pinned snapshot reads
and end-to-end installed download tests remain useful future work.

## Research Council — explicit pre-round evidence metadata freeze

An authenticated **human** may select 1–16 exact Workspace-readable
Project Library versions for a Research Council Task that has a canonical
Project Workspace. `POST /v1/team-sessions/{sessionID}/research/evidence-selection`
requires `team.write` and freezes the version/hash metadata selection for
the specific Research session. `GET` on the same path requires `team.read`
and a currently active human principal. Neither action accepts a caller-
supplied Project ID, Workspace ID, model ID or raw file path.

The freeze requires a saved **Research-mode Council session**, its immutable
session-manifest SHA, a created Task with a named Project Workspace,
and **no Council turn initiated** (round zero with no turn requests).
Approval, current exact Library read grants, artifact versions and
Project/Workspace ownership are rechecked inside the write transaction.
Concurrent revocation, archive, foreign selection or a different
human-approval plan fails closed. Identical pre-round retries are idempotent;
the first immutable approval cannot be edited after the Council begins.

Migration 0048 stores the human approver, frozen Council-manifest fingerprint,
source Project Workspace and *metadata-only* Library entry manifest hash.
The event journal captures the approval, not document content. Later reads
recheck the current Library grants; revocation or expiry makes the receipt
unavailable without erasing the historical provenance record.

**Crucial boundary:** freezing metadata does **not** inject content into any
Council seat, inference prompt, WebChat conversation, Task, evidence packet
tool or external provider. First-pass seat isolation and the no-Library-
without-explicit-attachment rule remain enforced. A separately reviewed
byte-verified, sealed Research evidence attachment and seat-specific
consumption policy are still needed. This work does not claim that step is
complete, nor that a physical inference test has run.

## Research evidence — human-only eligible session discovery

The Workspace Library's evidence approval dialog now selects an existing
Research Council Task session from the exact current Project Workspace
instead of requiring manual copying of an opaque session ID.

`GET /v1/projects/{projectID}/workspaces/{workspaceID}/research/evidence-sessions`
requires authenticated Project and Team read authorisation, active tenant
membership and a **human** principal. The backend lists at most 40 live
Research-mode Council sessions with an active Team, a created Task bound to
the requested canonical Project Workspace, zero rounds, no turn requests and
no previously frozen evidence selection. Sibling Workspace and other
Project sessions are never included.

Selecting a session is only UI assistance. The actual freeze endpoint
independently revalidates the human, session manifest, round and current
Library version permissions transactionally. A stale picker result does not
permit a late/unauthorised approval. No document data or evidence content
is passed to a Council model through discovery.

## Research Council — explicit frozen-text integrity preflight (operator only)

A newly frozen Research evidence metadata selection can be separately checked
from the Workspace Library approval dialog by choosing **Verify frozen text
locally**. The approval dialog does not automatically read document bytes.
This additional operator-initiated `POST
/v1/team-sessions/{sessionID}/research/evidence-preflight` requires existing
`team.read` and `project.read`, a currently active human principal with
active tenant membership, and the exact immutable Research evidence selection.
It accepts **no caller-supplied asset list** and does not grant new read access.

The API reuses the established byte-verified Workspace text reader, enforcing
at most eight UTF-8 text versions, <=64 KiB each and <=256 KiB total. Each
read requires the current canonical Workspace grants/publications, matching
Project and tenant-owned active managed artifact, exact selected version hash
and byte length. It fails closed on revoked/expired rights, binary/oversized
files, corruption and changed manifests; it checks the frozen Research
approval again after all reads. A previously frozen metadata selection of
nine to sixteen versions remains valid as **metadata**, but is not eligible
for this bounded text preflight.

The response is **only** a no-store, schema-versioned receipt of the Research
session, manifest hashes, document count, byte total and verification time.
No raw document text, snippets, filenames, blob IDs, storage URLs, capability
leases, prompt content or plaintext caches are returned or persisted by this
preflight. Nothing is attached to a Research seat; no inference, routing,
untrusted document execution or new Council authority is introduced.
Permissions must be checked again at any eventual explicitly consented model
consumption; this receipt is neither an attachment nor proof of perpetual
access. Automated evidence integrity/isolated-scope checks do not substitute
for external physical Node and installed-product QA. The user review
sequence and protected RC10 baseline remain unchanged.

## Research approval history — recheck frozen evidence after leaving the dialog

Workspace Library operators may explicitly select **Show saved approvals** to
list up to 40 formerly approved Research Council Task sessions from the exact
canonical Project Workspace. The new read-only `GET
/v1/projects/{projectID}/workspaces/{workspaceID}/research/frozen-evidence-sessions`
requires Project read and Team read, active Project/Team/tenant ownership and
active human tenant membership. Rows contain only session/Task/Team identity,
Task objective, session state, round and approval timestamp. They include no
Library asset IDs, selected names, content hashes, plaintext, storage paths,
tokens or reusable permissions. Sibling Workspaces and other tenant Projects
never appear.

Each listed session has **Reverify frozen text**, which invokes the separate
CSRF-protected, bounded, human-authorised, no-store preflight implemented
earlier. Loading history never fetches document bytes. Reverification requires
a fresh operator click and rechecks current Library version grants,
content-addressed managed artifact hashes, tenant and Project ownership and
the frozen session manifest; unavailable/revoked records fail closed.
Results render as literal text, not executable HTML. This does **not** attach
documents to Research prompts or bypass Council seat isolation.

The integration tests include pre-approval invisibility, exact approved
Workspace isolation, agent/unknown-human denial, account suspension, Team
archive and restored eligibility. External installed UI and physical Node QA
and the mandatory code-review/vision-review sequence remain unresolved.

## Research Council — immutable local-only seat-sharing consent groundwork

At the initial consent groundwork stage, Research model evidence was **not dispatched**. A separate explicit
per-session operator consent is now available from Workspace Library's
saved Research approvals. The operator first freezes exact metadata, then
selects named eligible **agent** seats from the frozen Council manifest,
reviews the strict local-only limitation and confirms the additional
approval. Manual Web seats, Chair-only seats, unknown seats, human and
disabled seats are ineligible. This initially created a dormant policy record,
not permission to use an inference or external provider.

Migration 0049 stores only exact session/Research/evidence manifest hashes,
canonical immutable seat IDs, human approval provenance and a
`local_only` policy. An approval can be permanently revoked. It cannot be
replaced with a different seat set, restored after revocation, or first
created after a Research turn has been requested. The write transaction
rechecks active human tenant membership, exact Task/Team/Project Workspace,
immutable session manifest, current version grants, and the bounded
1–8 textual-version envelope (64 KiB per version; 256 KiB total).
No raw Library content, authentication secrets or model inputs are stored.

`POST /v1/team-sessions/{sessionID}/research/seat-evidence-approval`
requires authenticated `team.write`, `project.read`, explicit
`confirm_local_only: true` and the selected member IDs.
`GET` on the same path requires `team.read` and `project.read`;
`POST .../seat-evidence-approval/revoke` requires `team.write` and
`project.read`. All service methods separately check active human
membership. Revocation still succeeds when a former Library read grant
is gone; it does not reinstate or dereference old evidence.

**At the initial consent stage**, the Team Worker and model
request assembler did not read this table; the later bounded local delivery
increment below supersedes that implementation limitation. Before actual consumption,
the implementation must independently enforce exact local candidate and
seat identity, route/provider restrictions, byte verification immediately
before inference, untrusted-content context segregation, per-turn
provenance, revocation/retry TOCTOU safety, provider-residency and no-fallback
policies, and negative integration tests. Do not present these records as
successful model delivery. Physical rootless Node acceptance, installed
Windows/Ubuntu QA and both mandatory reviews remain blocked separately.

## Research Council — origin-node evidence destination fence

When a Research seat has an active human-approved local-only evidence intent,
Team Worker now rereads and binds that approval to the immutable Research
session manifest and exact seat before selecting a candidate. It constrains
routing to local origin-node model deployments with no potential monetary
spend and additionally checks the resolved candidate before seat binding,
after any pinned-seat re-resolution, and before budget reservation/inference.
Remote nodes, provider connections, agent runtimes, free cloud offerings and
untrusted destinations fail closed. Provider failure parks the Research round
without substituting a cloud model. Consent is rechecked each turn, and the
Research request manifest records the consent fingerprint with
`research_evidence_content_delivered:false`.

**No evidence bytes were attached in this routing-fence increment**. This is a necessary
policy fence, not a claim that a future local evidence-delivery facility is
complete or that it has passed physical runtime testing.

## Research Council — bounded locally routed evidence context (initial)

A named Research seat with a separate, still-active, human-approved
`local_only` evidence plan may now receive the exact version-frozen Library
text inside its **local-origin model** request. The worker retains isolated
first-pass visibility and never sends this context to manual Web seats, cloud
providers, remote nodes or agent runtimes. The request is rejected if the
runtime model has insufficient verified context capacity; it does not
truncate source files or replace the selected model.

The worker constructs the prompt only from stored approved version IDs,
checks active grants through the canonical Workspace evidence packet both
before and after reads, and independently verifies active Project/tenant
managed artifact provenance, exact SHA-256, exact byte lengths, UTF-8 and
allowed textual MIME types. The operator preflight cap remains 8 documents,
64 KiB each and 256 KiB total; this first local delivery implementation
intentionally applies a **stricter 32 KiB total plaintext limit**. Larger
approved selections pause, without silently dropping documents. Evidence
is carried as non-authoritative `UNVERIFIED_DERIVED` `research_evidence`
context; its text is not executable tooling instructions and is not a
privileged agent directive. SHA-bound Research and Council context
fingerprints accompany the resulting model request.

The worker checks seat consent again immediately before inference. This
reduces but cannot mathematically eliminate the timing interval between the
final SQLite check and an already-issued local model call. Stronger
revocation-while-inflight guarantees and real installed runtime acceptance
remain separate tasks; a verified request is not proof that a model followed
untrusted-content instructions correctly. No RC11 installer is released by
this increment.

## Research Council — derived-content isolation and active consent revocation

Because critique rounds may include prior agent answers, direct prompt
isolation **alone** is insufficient: a local seat could quote a private
Library passage that reaches another model later. Human seat consent is
therefore currently accepted only when **every active Council participant**
is a named eligible agent seat; mixed Web/manual, human, Chair-only, partial
seat lists and manual Chair sessions are rejected. The worker independently
rechecks that immutable full-seat coverage before *all* Research turn
dispatch, including before manual Web handoffs. Once protected evidence
consent is revoked, the session cannot resume as an ordinary unrestricted
Council; the operator must start a new session.

The saved approval metadata contains no evidence bytes. The local inference
request can contain selected, freshly verified text when active approval and
all safety gates succeed. The stored approval API now labels the state as
`verified_local_delivery_enabled` rather than incorrectly claiming local
model dispatch is disabled. This does **not** authorize arbitrary agents,
unapproved Workspaces, cloud APIs, remote nodes or unverified alternative
Library versions. The 32 KiB delivery cap and hardware/context-capacity
constraints still apply. Physical installed inference, crash recovery,
inflight revocation guarantees and disclosure audit remain release-review
items.

## Research Council — per-turn evidence attestation and redacted QA state

A new Research turn-provenance v2 record binds the successful seat response
to an immutable agent-request SHA, resolved local candidate, frozen Council
manifest, selected evidence manifest, consent fingerprint, and exact
verified document count/byte total. The proof independently checks that
each `research_evidence` section is non-authoritative
`UNVERIFIED_DERIVED`, carries `untrusted_library_content`, and its text
still matches its embedded SHA-256. The request metadata cannot falsely
claim evidence attachment with no sections or embed an evidence section
while omitting the approval marker. Invalid metadata and nonlocal candidate
types are rejected **before** model inference.

Only successful Research responses receive per-turn durable provenance.
This is evidence of a completed locally routed response with a bound
request, not proof the model correctly used the evidence nor a hard
guarantee of atomic revocation while an already-sent inference runs.

The restricted QA snapshot surfaces only `seat_consent_state`
(`none`, `local_only_approved` or `revoked`) and
`verified_local_response_recorded`, based on a Worker-authored agent
response whose v2 evidence manifest matches the stored Research approval.
QA does not export documents, model replies, filenames, asset IDs,
selected seat identities, Library contents, provenance hashes or raw
inference prompts. Historic replies may still be recorded after revocation,
while current permission checks stop new protected dispatch.

## Protected Research completion and revocation transaction

For a Research Council turn containing locally delivered frozen Library
evidence, the worker no longer writes the model response and successful
turn in separate transactions. Instead, the final completion transaction
atomically rechecks the active human consent, exact Research and evidence
manifest hashes, selected member and round, active tenant/Team/Project,
the original approving human's active membership, and **all version grants**
including pinned/latest rules and live directional publications. A concurrent
revocation/grant update cannot interleave between this final authorization
check and the durable response. If any predicate fails, the transaction
rolls back without a message or successful turn. The worker discards its
model response and pauses the Council for operator intervention.

This prevents a *post-inference* revocation race in the persistence layer;
it cannot retract plaintext from a model invocation that had already
started before revocation. It does not imply that the local runtime cancels
in-flight generation instantly. Existing non-protected Council/Team paths
retain their own behavior. SQLite regression tests cover success,
idempotency/replay, Library permission withdrawal, and revoked human consent.

## Project Orchestrator — read-only recovery checkpoints

Each operator-approved Task DAG now supports
`GET /v1/projects/{projectID}/orchestrator/task-graphs/{graphID}/recovery-checkpoint`
with authenticated `project.read` permission. Its snapshot is derived from
the canonical graph's consistent SQLite read transaction and binds the
original manifest to a deterministic observation SHA-256 over persisted
Task states, dependencies and recovery classifications.

The UI offers an on-demand **Inspect recovery checkpoint** action for each
graph. It renders the result with `textContent` and never invokes a
task-transition, retry or model fallback. Failed, cancelled, blocked,
scope-drifted or dependency-regressed Tasks demand operator review; a healthy
independent branch remains eligible for the **existing governed Task Worker**.
Running, verifying and resource-waiting attempts remain observations of their
persisted continuation, not instructions to re-run unknown side effects.
All checkpoints explicitly record `automatic_retry_allowed:false`.

These are recovery *observations*, not execution checkpoints from inside
containers. Real crash-safe interrupted attempt reconciliation, lease expiry,
and physical Node acceptance still require separate implementation/QA.

## Research Council — bounded timer retries and permanent consent-fence

The Research retry path now rechecks the original immutable frozen evidence
approval, exact manifest binding, active Team, Project Workspace, tenant,
and approving human membership **inside the same SQLite transaction**
that requeues blocked turns. A previously protected session may never be
restarted as ordinary unrestricted Research after the original consent is
revoked or its authority is lost. The Worker independently performs fresh
Library-version and origin-node model eligibility checks upon each retry.

Unattended provider reset-time retries are capped at **two automatic
attempts** per unresolved seat; every blocked/failed seat must have an elapsed
provider timer and be within this cap before *any* automatically resumed
round. Other errors remain paused for manual human retry. Provider-timer
eligibility is rechecked inside the write transaction to prevent stale
scheduler observations. The original Research seat model pin remains in
force, and the system never silently substitutes another model on retry.
These constraints prevent a rate-limit loop from regenerating a round
indefinitely without the operator.

## Startup — orphan Agent Worker Attempt recovery

The existing atomic admission and `RecoverLostRuns` path already blocks
in-flight worker executions after restart. Legacy or inconsistent records
may nevertheless contain a live Agent Worker-owned Attempt with **no live
Worker row**. Startup recovery now separately detects the exact Agent Worker
protocol marker and principal, and refuses to adopt any other Task executor's
Attempt. The orphan finder includes only created/queued/running/waiting
Attempts and excludes every Attempt with a running or legitimately waiting
Worker. Each candidate is rechecked within a SQLite write transaction
before the canonical Task recovery method atomically interrupts its Attempt,
blocks the Task, records a recovery event and enqueues an operator recovery
outbox job. No model request or Tool mutation is replayed. One inconsistent
Task does not prevent independent orphan inspection. Repeated startup calls
are idempotent; a successful Task still requires independent reconciliation
and explicit admission before future execution.

This adds a narrowly scoped legacy-database/consistency recovery seam,
not a distributed lease or live-heartbeat expiry algorithm. Do not invoke
startup recovery while an existing independent Worker incarnation remains
active. Physical Node/process lifecycle acceptance remains outstanding.

## Orchestrator — Attempt/Worker-backed recovery checkpoint

Project Task Graphs now include an `attempt_health` classification derived
from the **same read-only SQLite snapshot** as the approved DAG, hard edges,
Task state and Workspace identity. A running Task with no active Attempt,
more than one active Attempt, corrupt Attempt metadata, a missing live Agent
Worker for an `agent_worker:v1` Attempt, or a mismatch between Task,
Attempt and Worker statuses is marked `needs_attention`. Legitimately
waiting Agent Workers are reported as waiting, and non-Agent-Worker
execution protocols are not mistaken for this subsystem's ownership.

Recovery checkpoint digests include the derived Attempt health, so an
interrupted/changed Attempt invalidates a previous checkpoint even if the
Task's revision is otherwise unchanged. No raw continuation, prompt,
credential, tool payload or model output is returned by this read path.
Task state remains subject to actual verified evidence; operator inspection
is not a replay permit. No hot-process heartbeat, remote fencing or
physical-Node acceptance is implied.

## Orchestrator — unknown external Tool mutation recovery state

The Project Task graph and recovery checkpoint now inspect only the bounded,
Task/tenant-scoped **Operation state** within the same consistent SQLite
snapshot as Task/Attempt and dependency state. The operator sees
`external_effect_health`, not secret-bearing tool input, receipts, endpoint
URLs or capability leases.

Unknown or blocked-unknown external outcomes, failed compensation, and
inflight mutations that outlived the Task's running state are classified as
`needs_attention`. A legitimately running external Operation remains a
live observation, not an automatic crash verdict. Resolution requires
independent observation, verification or authorised compensation through
the existing OperationCoordinator. No fallback Tool invocation or Task
replay is initiated by a graph read. The external-effect classification
also changes the deterministic recovery checkpoint digest. Automated
tests cover tenant/Task isolation, provider-state precedence, and
unknown-result review instead of automatic retry.

## Local physical Node — fenced sandbox reconciliation

Runtime reconciliation is intentionally local-only for a Workspace
explicitly assigned to this Node. Each Tool/verification sequence now
retains the original approved Runtime and application intent rather than
adopting a newer database revision after a possibly long-running OCI
operation. Immediately before applying runtime or application observations,
and before additional image-pull/container-ensure actions, the reconciler
rechecks immutable Node assignment, Project/Workspace ownership, runtime
desired state, isolation backend, OCI source/image revision, relevant
network/filesystem/resource/environment constraints and exact revision.
Any drift returns `ErrReconciliationDrift` without falsely marking the
new desired state fulfilled or proceeding with further stale Tool actions.

This is a read-then-recheck safeguard; an already dispatched physical
mutation cannot be unsent, and the existing optimistic revision checks
remain the final commit fence. The Operator must reconcile the observed
external state under the newly approved desired revision. This does not
establish successful physical Node smoke acceptance or remote sandbox
execution.
