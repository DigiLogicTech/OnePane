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
