# RC11 — Second code review against confirmed OnePane vision

**Review date:** 2026-10-11. **Candidate:** draft PR #227,
integrated head `77a96162bb2ca3277f3d9737fbd8e9a94b6525bd`.
**Evidence:** [CI #38122612493](https://github.com/DigiLogicTech/OnePane/actions/runs/38122612493)
passed Go, SQLite integration and WebUI checks. One preceding docs-only
run hit a 403 retrieving a Go dependency from the proxy; subsequent
source builds passed, so it is an infrastructure failure, not proof
of a code defect.

This is the requested **post-vision source-level review**. It is not
the final implementation-complete security sign-off or physical QA.
All six decisions are documented in `docs/rc11-vision-decision-register.md`.

## Review findings

| Priority | User-approved outcome | Observed code state | Required remediation |
| --- | --- | --- | --- |
| P0 | Unified Assistant as the single work entry | Existing Project Orchestrator turns and Assistant context have distinct paths; no proven uniform taskless-chat feature-policy chain | Unify user-facing conversation and Project context without granting sandbox privileges |
| P0 | L4 autonomous Projects default, resource constrained completion | Worker/scheduler hot-swap and queue foundations exist, but `featurepolicy.AssistedDefaults()` disables `AutonomousRoutines` when any Project feature policy is configured | Design versioned mode migration, consent and approved sandbox policy so L4 works by default without privilege expansion |
| P0 | First-class L5 continuous, recurring goals | Durable observation/operation/task components exist, but the complete goal lifecycle, recurrence and verified health/response loop has not been demonstrated | Implement OPM-01 through OPM-10 with safe-by-default response playbooks |
| P0 | Five permissions across all tools | `featurepolicy.Policy` currently has a single `Files` state, and explicit file Tool Gateway mappings are enforced; generic shell/Git/OCI/MCP effects lack proven universal classification | Create typed Read/Write/Execute/Network/Secrets enforcement with sandbox hard boundaries and cross-transport bypass tests |
| P0 | Versioned Agent policy and live revocation | Agent profiles have a revision, but that is prompt provenance and is explicitly non-authoritative; Project/Workspace Task policies are partially enforced | Bind immutable Agent policy revisions to live effective grants at every run/tool/transfer boundary; cancel and reconcile |
| P1 | Live plus historical diagnostics by Project or Agent | Existing QA timeline/bundles, Debug Centre captures and policy reviewer form a foundation; no proven general scoped live stream, retention policy or detailed Agent trace pipeline | Bounded per-scope capture and replay with retention, redaction and low-resource tests |
| P0 | Trusted paired Nodes as local-first, distinct from cloud | Node pairing and manifest verification exist; `scheduler.RouteRequest.LocalOnly` currently excludes all remote candidates. Inference scopes cloud/local using whether a provider record exists; endpoint verification/explicit transfer grants incomplete | Implement verified deployment destination types and authenticated Node selection with separate transfer/cloud grants |

## Immediate security correction completed

`internal/scheduler/catalog.go`: a deployment with both a local-node
record and a provider connection was previously eligible as a local,
zero-cost candidate. Deployment-supplied scheduling metadata could
also override a provider cost classification. This could undermine
local-only or monetary-spend restrictions. Both paths are now made
conservative: provider-backed deployments do not become local based
on node status and cannot replace the provider's spend classification
from deployment metadata. Regression is in
`internal/scheduler/integration_sqlite_test.go`, green in CI
#38122612493.

**Conservative limitation:** legitimate locally hosted OpenAI-compatible
deployments using provider records may still be treated as cloud until
their actual endpoint is verified. This is an intentional safe denial
rather than a finished local-compute solution.

## Delivery order and branch manifest

- [x] Vision interview decisions 1–6 recorded, with Level 4 default,
      Level 5 required, safe and advanced approval choices.
- [x] Post-alignment source-path audit and provider/local classification
      security fix validated in hosted CI.
- [ ] **P0-A — Unified authority and policy migration.** Define
      canonical Assistant session->Project/Workspace execution linkage,
      typed action taxonomy, versioned Agent grants and safe L4
      upgrade/migration. Do not implement broad grants by assumption.
- [ ] **P0-B — Side-effect boundaries.** Inventory every shell/Git/OCI/
      MCP/tool/GUI/network/secret path, enforce permission class and
      sandbox isolation, and add adversarial adapter tests.
- [ ] **P0-C — Local-first scheduling.** Verify endpoints, Node trust
      and data transfer; distinguish owned paired Nodes from cloud,
      separate compute routing from paid provider consent, and implement
      queued waiting/Colibri/swap acceptance scenarios.
- [ ] **P0-D — Autonomous L4 and ongoing L5 lifecycle.** Durable
      mode/goal schema, task graph/checkpoints, periodic triggers,
      observations, consented remediation and postcondition verification.
- [ ] **P1-E — Diagnostics and provenance.** Unified live/historical
      privacy-filtered collection and Agent-level scoped tracing.
- [ ] **P1-F — Integrated regressions.** Cross-Workspace isolation,
      revocation during dispatch, protected spend, crash recovery,
      research seat pinning, synthetic security incident playbooks.
- [ ] **GATE — Final code/security audit after implementation**, not
      simply this design-gap review; run hosted cross-platform CI and
      obtain explicit user permission before packaging.
- [ ] **QA — Windows x64 / Ubuntu amd64 upgrade and installed QA.**
      Validate data preservation, actual Node pairing, GPU/CPU model
      residency, themes, log drawer and UI controls on real hardware.

## Non-negotiable release gates

Do not claim the complete L4/L5/permission/Node system is implemented
based on design documents or partial code. Do not merge PR #227 to a
release branch or publish installers until the blocking implementation
items, final review, explicit packaging approval and installed QA.
Preserve current working Project/model data through any migration.

**Continuation:** start with P0-A source inventory and smallest
enforceable policy/migration increments; validate each, commit, update
this manifest, and continue automatically where dependencies permit.

## Incremental remediation after this review

- [x] Trusted-node scheduler candidate proof added: pairing and capability-manifest freshness are checked by the catalogue, and a local-first route may admit such a candidate only with positive `AllowTrustedNodes` and a matching explicit Node allowlist. Unit and node-federation integration regressions added.
- [x] Canonical Task→Project/Workspace persisted trusted Node allowlist now restricts Worker routing and inference preflight/last-transport boundary. New grants require a human principal, and live pairing/manifests are checked. Agent-specific grants and full transport-identity verification remain outstanding.
- [ ] Verify endpoint trust for provider-backed localhost and Node transports before reclassifying them as host-local or trusted rather than cloud.

## Branch execution checkpoint — 2026-10-11, next implementation gates

Completed in this integration branch, with source and regression tests:

- [x] **NODE-03 subset:** project+Workspace double-consent for off-host transfers, strict allowed Node ID intersection, tenant and active-Workspace checks; human approval for expanding transfer authority and live revocation on Node routing/dispatch.
- [x] **NODE-04/05 subset:** Worker supplies persisted allowlist to scheduler each tick; inference gateway revalidates scoped off-host transfer and fresh pairing/manifest before the external transport. Taskless remote transfer remains denied.
- [x] **OPM-01 subset:** an existing Project resolves safely to Level 4; an explicitly selected Level 5 mode is stored/revised with no silent monitor activation or permission expansion. Level 4/5 selector is exposed in Project creation and Inspector.
- [x] **OPM-01/04 subset:** additive `migrations/0054_project_continuous_goals.sql`; canonical Project/Workspace goal records start `configured` and `unobserved`. The approved goal has a typed limited monitoring plan, never an automatic network probe.
- [x] **OPM-07 subset:** authenticated read/configure continuous-goal REST endpoints and a Project Inspector form selecting actual canonical Workspaces. The GUI displays configured/unobserved honestly and does not imply deployed protection.
- [x] **PERM-01/02 subset:** distinct Read, Write, Execute, Network and Secrets effective feature states; monotonic inheritance and legacy Files compatibility; explicit policy expansion requires human approval, while Agents may narrow grants.
- [x] **PERM-03/08 subset:** File Gateway and known shell/OCI/Git/MCP/network/secrets operation effects classified at tool admission and rechecked at the last adapter boundary. Unknown effects fail closed for configured policies. Negative tests include revoked Execute, Write denial, and unclassified mutation.
- [x] **CI and rollback:** initial failing Worker admission regressions were due to a system principal authorising new autonomy under the stronger approval gate. Fixture changed to an explicit human operator; [re-run #38125542945](https://github.com/DigiLogicTech/OnePane/actions/runs/38125542945) passed. Subsequent goal API code [#38125753168](https://github.com/DigiLogicTech/OnePane/actions/runs/38125753168) passed. Latest UI commit CI must be checked before calling this head green.

**Next continuation (priority order):** P0-A unify Assistant and canonical chat/Task authority; P0-B complete process/container/network/secret hard isolation across every adapter and operation path, with attack-path tests; P0-C verify localhost OpenAI-compatible endpoint identities, paired-node transport binding, transfer and cancellation reconciliation; P0-D actual Level 4 planning and Level 5 durable trigger→observe→respond→verify lifecycle; P1 live/historical diagnostics and final Agent policy snapshots. Then run a fresh security/code audit, cross-platform CI, **user-approved packaging** and installed QA. Never infer branch completeness or release approval from green partial CI.


## Unified Assistant implementation checkpoint — 2026-10-12

- [x] **P0-A subset: explicit canonical handoff.** The unified Assistant Submit API now accepts an optional selected Project Workspace. Before writing an Assistant turn or creating a handoff, service validation rejects inactive, unknown, and foreign-Project Workspace IDs; the surviving canonical ID is sent to Project Orchestrator's existing scoped Task path. An explicitly empty per-turn Project selection remains general instead of silently inheriting the thread's prior Project. Regression: `internal/assistant/project_selection_test.go`.
- [x] **P0-A UI subset: one chat.** The separate global Assistant and Project Orchestrator tabs are replaced by a single Assistant control chat with General/Project/Workspace selectors, a visible effective scope and the same authorised Task handoff. General chat's Run button is disabled; actual Project Task execution remains subject to service policy and `task.write` authorisation. General model pinning remains available, without fetching unrelated Assistant model inventories for Project turns. The UI uses canonical Project Workspace API responses, not dashboard-only synthetic layouts.
- [x] **Initial scope-handoff CI:** application code `660a8bf` passed hosted run [#38167207628](https://github.com/DigiLogicTech/OnePane/actions/runs/38167207628). Later unified UI/source validation and new contract-script CI still require green evidence; do not infer they passed from this earlier run.
- [ ] **P0-A remaining:** taskless Assistant and model-inference feature policy, independently versioned Agent grants, human-confirmed creation of a new Project from an unscoped L4 objective, safe L4 planning/execution graph, and no hidden cloud/provider fallback.
- [ ] **All other P0/P1 gates remain open:** L5 trigger→observe→respond→verify implementation, cross-adapter isolation, verified Node transport, live/historical diagnostics, final independent audit and explicit operator authorisation before packaging.

- [x] **Local-first general Assistant default:** unscoped Assistant inference, including an explicitly pinned model, now requires host-local zero-incremental-cost routing. Model choice does not itself issue a cloud-spend or off-host data-transfer grant; unavailable local inference waits/fails visibly rather than silently switching to paid providers. `internal/assistant/model_selection_test.go` covers pinned and automatic requests. Dedicated explicit cloud-grant and taskless policy flows remain a P0 implementation gap.

- [x] **OPM-02/07 safe stop subset:** Level 5 configured goals support authenticated, revision-fenced pause and irreversible human-directed retire via scoped REST endpoints. Both atomically clear `next_due_at` and remove claimed healthy state; transition events are retained. No goal scheduler, network monitoring or remediation is automatically activated. Negative tests cover stale CAS, cross-Project targeting, unapproved activation and terminal retirement.

- [x] **L5 management UI subset:** operator sees configured/paused/retired state and can pause a configured goal or confirm irreversible retirement in the Project Inspector. Calls the scoped CAS endpoints; does not depict paused/retired monitoring as active. CI now includes a lightweight frontend/backend goal-lifecycle contract script. This is a stop/control foundation, not finished recurring Level 5 monitoring.

- [x] **L5 agent-denial regression:** a Workspace-member Agent with `project.write` may not retire a Level 5 goal. Both domain/SQLite integration and authenticated HTTP tests verify that terminal retirement requires an active human principal, not merely write capability.

- [x] **Assistant pin admission:** direct model-selection API rejects remote, paid or unverifiably zero-cost deployments before storing a general Assistant pin. The scheduler still rechecks local-only zero-cost at inference time. Regression protects against bypassing frontend disabled options with an API request.

- [x] **General Assistant model-picker parity:** scheduler candidate API exposes the verified hard-zero-cost indicator and the Assistant selector disables cloud/unknown-cost/unverified candidates, matching server-side pin admission. A static UI/API contract check ensures future revisions cannot silently relabel paid or remote compute as selectable.

- [x] **Unscoped Run API deny:** a direct Assistant API request to create or force Tasks without a canonical selected Project now fails before any user turn or handoff is persisted. Covers stale client replay and the explicit general-chat override, not just the disabled UI Run button.

- [x] **OPM-04 evidence intake subset:** additive migration 0055 and typed Project-goal receipts bind existing immutable Observation IDs, source IDs and verified SHA-256 evidence to canonical L5 Projects. The attach transaction checks active tenant/Workspace, human operator, explicit monitoring-plan source allowlist, freshness, CAS revision, duplicate prevention and source-tenant isolation. A metadata-only authenticated API lists and attaches evidence. It **does not** activate sensors, establish baseline compliance, change health to healthy or schedule the next check. Adversarial API and domain tests must pass before calling this step verified.

- [x] **OPM-04 manual evidence UX:** Level 5 goal setup can specify approved observation source IDs. Project Inspector offers operator-mediated attachment of preexisting observation IDs and on-demand metadata-only receipt listing. Source selection does not start sensors; successful attachment remains explicitly unobserved, with no monitoring/response claim. Regression extends the L5 frontend contract.

- [x] **P0-A taskless Project inference check:** Project Orchestrator resolves canonical Project + active Workspace feature-policy gates before persisting a turn, before model routing, and again immediately before inference dispatch. Reassignment to a sibling Workspace rechecks its effective policy; denied, malformed, cross-tenant or inactive scopes do not reach routing. The inference service's own final transport check and Agent-level policy snapshots remain separate P0 tasks.

- [x] **Revocation-in-flight guard:** Project Orchestrator reasoning rechecks the chosen canonical Workspace after model routing, and again before model-derived or ForceTask admission. A revocation during scheduler selection blocks inference before transport, while ordinary model unavailability may still queue explicitly permitted work. An integration-independent regression simulates policy revocation inside scheduler routing.

- [x] **Run means Run:** OnePane's unified Assistant now passes both task-creation consent and `force_task:true` for the explicit Project Run action. It can create a governed queued Task even when an eligible local reasoning model is temporarily unavailable, instead of treating Run as a read-only Ask when model JSON omits `action=task`. General chat cannot Run, and backend API continues to require `task.write`.

- [x] **No arbitrary Workspace assignment:** explicit Run with an unavailable local planner may enqueue work when the operator selected a Workspace (or when exactly one is active), but will not silently assign a Task to a most-recent sibling when several isolated Workspaces exist and no model chose an active destination. The user sees a waiting/selection explanation, and no Task is written in the ambiguous case.

- [x] **Run UI scope clarity:** Run is disabled when a Project has no active canonical Workspace, and the unified chat warns that multiple isolated Workspaces require a successful model plan or an explicit Workspace choice. Users can still Ask without Task authority. The Project Orchestrator's SQLite admission enforces the same no-guess policy.

- [x] **L5 read-only evidence coverage:** lightweight metadata-only assessment now distinguishes missing, stale and recently attached approved observation sources, without exposing sensor values, claiming verified health, changing goal state, invoking an LLM, or activating probes. Scoped GET API and Project Inspector view added. Coverage does not imply device-security posture or complete operational monitoring.

- [x] **P0 Agent prompt-revision fence (partial):** pinned custom Agent profiles require an active, exact-Workspace persisted revision; missing/archived/foreign profiles never silently default to `agent.md`. Worker binds the revision before context compilation and rechecks after routing and immediately before inference dispatch. Revision changes block the run without granting any Agent authority; actual independent versioned *Agent capability policy*, atomic transport cancellation and final tool revocation remain open P0 work.

- [x] **P0 non-spending final candidate admission:** unified general Assistant and Project Orchestrator now independently reject a scheduler result unless it is a verified host-local, hard-zero-incremental-cost model. This is enforced after routing and before fetching the global snapshot or sending Project context for inference. Regression injects cloud, paid, unverified and wrong-runtime scheduler results; approval for paired-node transfer remains a separate boundary.

- [x] **P0 endpoint URL parsing/SSRF protection:** local OpenAI-compatible transport now validates parsed numeric IPv4/IPv6 loopback host, numeric port, and an origin-only HTTP URL, rejecting deceptive `127.0.0.1:port@remote` user-info, paths, query strings, fragments, non-loopback aliases and invalid ports before any model prompt leaves the process. The default HTTP client no longer uses environment proxies for host-local inference. Dedicated SSRF regressions added; this does not yet attest arbitrary provider-backed OpenAI-compatible endpoints.

- [x] **P0 inference budget bypass closed:** final `inference.Execute` budget admission no longer accepts deployment JSON overriding the registered provider's spending classification. Every provider-backed route requires an approved reservation unless the provider itself is configured as hard-zero-cost `free`; a provider claiming `local` is conservatively reserved until origin attestation exists. Non-provider local/paired-node routes keep their separate trust and transfer gates. Negative tests prevent paid/unknown provider budgets from being bypassed by a forged deployment `cost_class`.

- [x] **P0 taskless final-transport locality gate:** `inference.Execute` now independently requires the canonical deployment to remain on the configured local Node, with no provider connection, for `onepane-assistant` and `project-orchestrator` requests. It enforces this before runtime acquisition and re-reads deployment revision and identity directly before the transport invocation, preventing a scheduler-to-transport switch to cloud, paired Node or different deployment. No Task/Node transfer privilege is silently granted; tests inject mid-queue reassignment and provider association.

- [x] **P0 Agent policy revision foundation:** immutable, CAS-controlled Agent policy revisions live in migration 0056, independent of editable profile instructions. Explicit custom Agent Task scopes now load the current revision at every stored-scope resolution, applying it monotonically between canonical Workspace and Task restrictions; missing/archived/foreign profiles fail closed. Agents can only add restrictions; removing a prior denial requires an active human member, and no revision can assert new enabled capabilities. Scoped GET/PUT API plus integration tests cover live restriction, CAS, cross-tenant, immutable history and archived-profile revocation. This is not yet runtime cancellation of in-flight external side effects.

- [x] **Agent restriction UX:** custom Agent profile cards now expose a revision-aware Restrictions editor for Read, Write, Execute, Network and Secrets. Checked rights are explicitly denied; unchecked rights inherit only canonical Project/Workspace grants. Other specialist denials are preserved on save. A stale edit or non-human removal is rejected at the backend; core/builtin profiles remain read-only.

- [x] **L4 new-Project autonomy preset (UI only):** human-created Projects default the visible safe-local autonomous Task option to enabled, persisting `feature_policy.AutonomousRoutines=enabled` without enabling shell, network, secrets, cloud or provider spending. The backend requires an active human to expand a new Project policy. Operators can opt out, and existing Projects/API callers are not silently migrated. Automatic Task planning and setup of new Workspaces remain separate P0 work.

- [x] **L4 atomic starter Workspace:** the new-Project UI explicitly requests one canonical unprivileged Development Workspace. The domain creates it in the same SQLite transaction as the Project, with separate scoped creation audit events. A human member is mandatory; it creates no runtime, host mount, Agent or network permission. API callers that omit `create_starter_workspace` preserve previous semantics. This removes the new-Project/no-Workspace barrier to safe queued Tasks, while actual autonomous planning remains outstanding.

- [x] **L4 user-authored initial objective handoff:** New Project dialog now accepts an optional initial objective and offers a default-on, explicitly labelled queue action. It first commits the human-owned Project+canonical starter Workspace, then submits a `force_task` handoff to the existing authorised Project Orchestrator in that exact Workspace; no new model-generated privilege grant or guessed Workspace is introduced. Failures retain the Project and explain that Task creation must be retried; queued does not mean executed.

- [ ] **Legacy read-side materialisation follow-up:** `WorkspaceViews` still uses the preexisting `ensureWorkspaceViews` compatibility mechanism, which persists a fallback Main Workspace on the first read for Projects without canonical workspaces. This has not been changed by the explicit starter option. It should be relocated to an explicit, auditable compatibility migration before final security sign-off; no claim of pure read-only semantics is made for this legacy endpoint.

- [x] **Legacy Workspace migration concurrency guard:** read-triggered legacy `WorkspaceViews` materialisation now rechecks Project Workspace rows inside its SQLite transaction and refuses archived Projects, preventing duplicate Main objects from concurrent initial reads or resurrection by a read after Project archive. It remains a mutating legacy GET path pending migration into an explicit reconciliation phase.

- [x] **Scoped Agent policy history for debug/QA:** GET `/v1/agent-profiles/{id}/policy/history` returns at most 50 immutable policy revisions within the authenticated Workspace. The custom Agent restriction modal can display recent revision number, issuer and date without model prompts or secrets. Tests cover ordered revisions and cross-tenant denials; this is not a general live Agent trace collector.

- [x] **P0 queued inference Agent policy-head fence:** in addition to checking the pinned custom Agent instruction revision, final inference transport now captures the independently versioned `agent_policy_heads` revision. A newly added, changed, missing, foreign or archived Agent policy invalidates the queued request before prompt export, even when the new policy still happens to allow the immediate action. Negative tests include inactive and wrong-tenant heads and an Agent policy revision change during the model queue.

- [x] **P0 full inference destination revalidation:** task-bound and taskless inference now re-fetches and compares canonical model, deployment and provider records directly before the transport call, after runtime and budget waits. It detects model trust/weights changes, Node relocation, provider credential or endpoint replacement, deployment configuration/fingerprint changes, and revocation of paired-Node trust. No scheduler-returned label or old catalog snapshot can authorise a substituted transport. This is a final local admission fence, not atomic remote cancellation after dispatch; physical paired-Node and provider QA remain open.

- [ ] **RC11 final hardware/atomic revocation QA:** physically verify CPU/GPU cold-load/hot-swap, queue waiting and paired-Node inference under the identity fence. Hosted regression now allows monotone residency/context-capacity observations while refusing model/endpoint/Node/provider changes and catalog rollback. Actual model runtime transitions and last-boundary concurrency still require installed QA; arbitrary remote requests cannot be cancelled once sent.

- [x] **P0-A consented Project creation from General Assistant:** General chat exposes `Start Project…`, bounded to the user's own 4,096-character objective draft and prefilled into the existing explicit human Project form. The preview is escaped. Creating the Project and optional first Task still follows existing canonical Workspace and human confirmation; the general Assistant receives no Task, cloud, shell or network grant.

- [x] **P0-A Task objective admission:** the Project Orchestrator no longer substitutes untrusted model-generated `objective` into a runnable Task. Its canonical Task retains the operator's exact approved objective, even after model-directed Workspace rebinding. Model suggestions can remain in the answer, but richer autonomous subtask plans require a separate bounded standing grant and graph admission. Existing cross-Workspace handoff regression now checks objective integrity.

## RC11 implementation continuation checkpoint — 2026-10-12 (later batch)

- [x] Task inference captures both the custom Agent prompt revision and independently versioned effective Agent policy head before a potentially long model queue, revalidating them immediately before dispatch. A changed or wrong-tenant head denies stale inference instead of silently adopting another Agent policy.
- [x] All inference routes now re-read canonical deployment, model, provider and paired-Node trust before transport, even when the request was a legitimate Task. Hosted regressions cover malicious provider, endpoint, weights, fingerprint, Node and trust substitutions.
- [x] The final route check explicitly tolerates safe context-capacity and residency-only changes during cold-load, protecting OnePane's limited-hardware/hot-swap intent. Revision rollback and changes to transport/security identity still fail closed.
- [x] In General Assistant mode the user can choose **Start Project**, which opens the human-confirmed Project form with their original, safely escaped and bounded draft objective. General chat still cannot Run Tasks by itself.
- [x] A Project Orchestrator model cannot replace the user-authorised objective of a queued Task, even when a different Workspace was selected and context safely rebound.
- [ ] **Next P0-D delivery:** structured bounded local planning with no silent objective expansion, task-graph admission under separately persisted Level 4 standing grants, crash-safe queue resumption and proper Provenance/Task placement. Do not mark L4 autonomous execution complete based on a single queued Task.
- [ ] **Next P0-D L5 delivery:** explicit, revocable probe execution grants and idempotent cadence scheduler; safe read-only observations; evidence-based health verification; independently approved remediation and postcondition checks. L5 configuration/evidence intake is not yet active monitoring or protection.
- [ ] **Remaining security / QA:** consistent hard host-level sandbox isolation and permissions for every adapter and helper, real authenticated paired-Node transport, final end-to-end revocation/spend QA, scoped live/historical diagnostics, full code audit, and explicit user approval before release packaging.

- [x] **P0-D bounded local plan draft subset:** a local Project Orchestrator model can optionally return a versioned 1–8-step DAG draft. The parser binds every step to the already selected canonical Workspace, rejects unknown fields, credentials/tool-permission declarations, out-of-Workspace placement, cycles, missing/duplicate edges, and oversized objectives. Only validated drafts enter the original scoped Turn's provenance and answer; they do **not** create a Task graph, mint grants or run tools. SQLite handoff regression proves validated model drafts stay within the rebound destination Workspace and remain nonexecuting. Actual autonomous graph admission/standing grants are still the next P0-D blocker.


## RC11 model-authored Task graph JSON ambiguity gate — 2026-10-12

- [x] The Level 4 local model plan has progressed beyond a nonexecuting
  preview: the existing branch now admits bounded Task DAGs only under
  persisted human-granted and revocable Project/Workspace autonomy.
  Durable Assistant provenance tracks approved graph identity and rejects
  arbitrary graph IDs without SQLite-backed ownership. These are *not*
  unrestricted Agent or tool grants.
- [x] **P0-D hardening:** reject duplicate JSON member names (including
  escaped equivalents) at the initial Project Orchestrator response
  parsing boundary, before reading a model-authored action, Workspace
  suggestion or graph. Invalid directives cannot trigger a model-directed
  action; the plain answer remains available.
- [x] The bounded DAG parser independently performs the same ambiguity
  check for direct callers, in addition to its preexisting schema,
  objective, Workspace and dependency validation. Unit regressions
  cover repeated action, destination, plan and step fields.
- [x] Hosted Go/SQLite/WebUI regression and cross-platform compilation
  validated the strict planner after the escaped Go string was corrected:
  [run #38184374215](https://github.com/DigiLogicTech/OnePane/actions/runs/38184374215)
  passed. Later integration source head is also verified below.
- [ ] P0-D remains incomplete: durable L4 task execution/recovery across
  actual runtimes and CPU/GPU/model swaps, and fully approved L5 recurring
  observe->respond->verify loops must be tested on installed hardware.
  This parser fix is not end-to-end autonomous-operation sign-off.

- [x] **P0-D L5 monitoring-plan admission:** configured source allowlists
  now reject duplicate keys, escaped-key duplicates, trailing JSON,
  unexpected authority fields and oversized payloads *before* creating
  any durable goal. Added focused unit tests; an empty plan still grants
  no probe/network execution. This is source-list input validation, not
  an active Level 5 monitor or automatic remediation capability.
- [x] Combined model-plan and configured L5 source-list changes passed
  hosted Go/SQLite/WebUI validation at
  [run #38184550038](https://github.com/DigiLogicTech/OnePane/actions/runs/38184550038);
  the full unmerged review gate remains mandatory.


## RC11 P0-D Level 5 evidence isolation checkpoint — 2026-10-12

- [x] The configured source-list format is bounded and rejects duplicate,
  encoded-duplicate and malformed JSON fields rather than accepting the
  last occurrence. Unit and Go/SQLite/Windows hosted validation passed at
  `be6d5f6`, [run #38184680298](https://github.com/DigiLogicTech/OnePane/actions/runs/38184680298).
- [x] Stored source policy is re-canonicalised at both metadata-only
  coverage reads and the final SQLite observation attachment transaction.
  Conflicting/unknown source grants fail closed rather than producing
  apparently valid coverage or attaching an unapproved observation.
  Integration regression mutates a stored source allowlist and asserts
  that both paths refuse it.
- [x] A **tenant, active Project/Workspace and human-member preflight**
  now precedes full observation integrity verification and payload lookup.
  A foreign/unknown Observation ID or Agent principal cannot cause this
  API to process cross-tenant sensor values before scope admission.
  Source identity, Project scope, human authority and freshness continue
  to be rechecked in the transaction; the preflight is not treated as
  irrevocable authority. Regression asserts generic early denial with
  no raw observation subject/value disclosure.
- [x] **Final application-code CI gate for this batch:** corrected non-human
  attestation to retain HTTP 403 (rather than 400), with an explicit
  privacy-scoped domain regression. The final application code head
  `72c50b00` passed hosted Go/SQLite, cross-platform and UI checks at
  [run #38184872468](https://github.com/DigiLogicTech/OnePane/actions/runs/38184872468).
  Earlier source runs failed only during the now-corrected response contract.
- [ ] Verify CI on the final documentation-only checkpoint; this evidence
  is not a final RC11 release/security sign-off.
- [ ] **Next implementation:** Level 5 independently consented source/probe
  registration and revocable execution leases; crash-safe cadence claims,
  evidence-baseline comparison, graded defensive playbooks and postcondition
  verification. Do not activate monitors, call a Project healthy, package
  an installer or merge a release merely because intake tests pass.


## RC11 Level 5 evidence-review authority and cadence-ledger checkpoint — 2026-10-12

- [x] **OPM-03 permit subset:** additive migration `0057_goal_evidence_review_permits.sql`
  records a human-granted, source-specific, expiring receipt-review permit.
  An exact canonical Project, Workspace, Goal, allowlisted source, plan hash,
  effect class and expiry bind each grant. No old goal gains a grant during
  migration. Permit expansion requires a fresh human Project member; Agent
  self-approval fails. One active grant per source prevents silent reuse,
  even if an older grant has expired. Reapproval follows explicit revocation.
- [x] Permits are CAS/revision fenced; revocation atomically advances goal
  revision and retains a nondeletable audit row. Expired, revoked, paused,
  foreign, non-L5, tampered-plan and archived-Workspace scopes deny a future
  receipt review. The only effect class is `existing_receipt_metadata`:
  **no host/network probe, raw observation read, LLM, tool or remediation**.
- [x] **OPM-04/05 metadata assessment subset:** additive migration
  `0058_goal_evidence_review_runs.sql` stores immutable, bounded
  per-permit/cadence-window evaluations of already-attached observation
  timestamps. A duplicate request in the same cadence window returns the
  original record; no second Task or new sensor invocation is created.
  Source freshness is `missing`, `stale_attached` or `recent_attached`,
  never inferred device health/compliance. Historical review results contain
  only source, timestamp, scope and provenance metadata.
- [x] API and Project Inspector permit grant/revoke, manual metadata
  review and history controls are available with explicit "not monitoring"
  language; backend independently enforces scope. UI review never changes
  `status`, `health`, `next_due_at`, `last_checked_at` or
  `last_verified_at`.
- [ ] Verify final combined Go/SQLite/WebUI/cross-platform CI. Latest
  application and contract-script validation for this batch is
  [workflow #38191508741](https://github.com/DigiLogicTech/OnePane/actions/runs/38191508741);
  if failure occurs inspect and remediate instead of treating a prior green
  run as latest. A test-only expiry-fixture bug was corrected in
  `7d71406c`; it did not relax runtime constraints.
- [ ] **Still required for actual continuous monitoring:** separately
  consented probe identities/targets/transports and live network/host/secret
  gates; a durable restart-safe active schedule/claim loop; baseline
  definition, trusted sensor integrity, truthful health verification;
  bounded remediation approval/rollback; installed hardware QA and final
  independent security audit. Current evidence reviews are manually
  triggered, not autonomous recurring sensor monitoring.


## RC11 Level 5 passive recurring evidence reviews — 2026-10-12

- [x] **OPM-05 narrow automatic execution subset:** `harnessd` now invokes
  `TickDueGoalEvidenceReviews(ctx,8)` on startup and once per minute. This
  is a bounded CPU/SQLite-only loop that reviews **preexisting, previously
  attested receipt timestamps** when a human-approved source permit has not
  expired. It does not load any model or invoke probes, network, shell,
  secrets, tools or device remediation.
- [x] The durable reviewer identity is
  `system:goal-evidence-reviewer`, independently bootstrapped as a named
  audited system principal. It cannot mint or revoke human permits. Every
  review rechecks Project/Workspace status, L5 mode, permit revision, grant
  expiry, exact source and plan hash inside the SQLite transaction.
- [x] Cadence windows are deduplicated by the immutable
  `(permit_id,cadence_window)` ledger. Startup and repeated ticks do not
  replay completed windows, and service restarts do not backfill offline
  intervals. Revoked permits and paused or retired goals block subsequent
  work. The candidate batch and per-tick results are bounded, allowing
  resource-constrained local hardware to remain responsive.
- [x] Project Inspector labels grant consent as **recurring receipt
  review** and explicitly states that device/security monitoring and
  remediation are *not* activated. Separate manual review and immutable
  historical timeline remain available.
- [ ] Validate final integrated CI and automatic worker integration
  regression after the UI contract wording correction in `5b7b8bb`.
  [Run #38191942666](https://github.com/DigiLogicTech/OnePane/actions/runs/38191942666)
  was running at checkpoint time. Earlier UI runs failed only an obsolete
  expected message string and must not be treated as a code-security
  failure or as proof the new source head is green.
- [ ] **Next P0-D:** add separately human-approved **real probe**
  identities/targets, durable external-effect admission and cancellation,
  bounded/read-only adapter execution and observation intake; establish
  verified baselines, truthful health, privacy-filtered incident history,
  signed evidence and approved remediation/postcondition checks.
- [x] **Crash-safe review starvation remediation:** migration 0059 adds a
  non-authoritative persisted worker cursor. Each tick examines at most 64
  candidates, visits up to 8 successful reviews by default, persists the
  last attempted permit even on denial, and wraps from the final source
  on later ticks. A healthy source beyond a malformed 64-row prefix is
  no longer permanently hidden by that prefix after restart. Integration
  regression includes 65 corrupt permits plus a healthy source.
- [ ] **Operational performance follow-up:** full retention/compaction
  policy for historical review ledgers, resource-pressure prioritisation,
  fairness under concurrent host instances and installed Windows/Ubuntu
  CPU-only and paired-Node QA. These remain outside the currently tested
  read-only receipt reviewer.


## RC11 receipt-review admission race closure — 2026-10-12

- [x] The last controllable metadata-review side-effect boundary now reads
  the current clock **inside** the SQLite writer transaction. A permit that
  expired while the request was waiting for the transaction lock cannot
  be authorised by its earlier pre-transaction timestamp.
- [x] An injected advancing-clock regression exercises the before-expiry
  caller preflight versus after-expiry transaction admission, expecting
  `ErrInvalidTransition` and no immutable review run.
- [ ] Confirm the complete hosted regression on application/test head
  `3afeb5c1` at
  [run #38192373387](https://github.com/DigiLogicTech/OnePane/actions/runs/38192373387).
- [ ] No release approval is implied. Active sensor permits, safe external
  probe dispatch, tenant data-transfer enforcement, verified baseline
  evaluation and Level 5 postcondition/remediation remain separate P0 gates.


## RC11 Level 5 explicit network observation — 2026-10-12

- [x] **Separate network authority:** additive migration
  `0060_goal_tcp_probe_permits.sql` stores human-granted, CAS-fenced,
  auditable permits to test ONE exact RFC1918 IPv4 literal and TCP port per
  approved Goal source. The exact canonical Project/Workspace, original
  monitoring-source plan hash, port, timeout (100–1500ms), approved human and
  ≤24h expiry are bound to each permit. No existing Project, Goal or source
  is activated by migration. Receipt-review permission is not network
  permission.
- [x] Source-registration API and Project Inspector offer explicit two-step,
  exact-target confirmation, list and revocation. The confirmation binds
  source, target, port, timeout, goal revision and expiry; a changed
  selection requires a fresh second click. DNS, CIDR, public, loopback,
  link-local, IPv6, metadata and unspecified targets are refused by backend
  `netip.ParseAddr` checks; no DNS resolution is possible.
- [x] **Manual real read-only probe subset:** migration
  `0061_goal_tcp_probe_attempts.sql` and `RunManualGoalTCPProbe`
  implement a human-initiated, one-shot outbound `tcp4` handshake.
  The service first records an immutable `claimed` attempt under a live
  tenant/Workspace/Goal/permit/membership check inside SQLite, then
  revalidates that permit immediately before dialing; the connection is
  bounded at 1500 ms and closed immediately without transmitting a request
  payload. The recorded outcome is `connected`, `not_connected`,
  `timed_out` or `cancelled`, explicitly NOT security posture, TLS trust,
  endpoint health, compliance or verified service operation. An interrupted
  claim is shown as unknown; it cannot silently auto-retry the same window.
- [x] **Abuse/replay fencing:** migration `0062_goal_tcp_probe_source_rate_fence.sql`
  prevents duplicate socket claims for the same `(goal_id, source_id,
  cadence_window)`, even if a permit is revoked and replaced. Database
  uniqueness and admission checks guard restarts and competing clients.
  The UI requires another confirmation before an actual connect; no
  daemon scheduling of TCP tests has been introduced.
- [x] Domain/API tests use a fake TCP dialer to verify exact target,
  fixed `tcp4` protocol, human-only authority, revoked/paused denial,
  idempotence after restarts, no second dial on replay, and zero effect
  on Task, raw Observation or verified Goal health. API tests never
  contact a real network target.
- [ ] **Verify final network increment CI** at
  [run #38194015501](https://github.com/DigiLogicTech/OnePane/actions/runs/38194015501)
  after the final source-level rate and exact UI-confirmation changes.
  Earlier partial-source Go builds failed on a corrected duplicate `err:=`
  binding; later pre-rate-check implementation checks passed at
  [#38193873006](https://github.com/DigiLogicTech/OnePane/actions/runs/38193873006).
- [ ] **Remaining P0-D:** authorise and implement recurring *real* probes
  with durable leasing, in-flight stop/cancel semantics, explicit
  target/network/Node scope and secure observation intake; add baseline
  comparison, trusted-sensor quality, graded health alerts, approved
  remediation/postcondition checks. A TCP handshake by itself is a very
  limited signal and is not protection of the user's home network.
- [ ] **Release gates:** independent code/security review, sandbox and
  paired-Node end-to-end enforcement, resource pressure and offline QA,
  installed Windows/Ubuntu testing, then explicit packaging decision.
  This work remains on the unmerged RC11 draft integration branch.


## RC11 TCP evidence baseline and drift comparison — 2026-10-12

- [x] **P0-D partial trusted-baseline scope:** migration
  `0063_goal_tcp_reachability_baselines.sql` stores immutable
  human-authored revisions of an expected `connected` or
  `not_connected` **TCP handshake** for a source and exact approved
  network permit. Each update uses CAS on both the Goal and prior
  baseline revision and advances an auditable baseline head. An Agent
  cannot define its own desired baseline, nor widen TCP target scope.
- [x] The new read-only comparison projects `missing_evidence`,
  `stale_test`, `inconclusive`, `matches_last_tcp_test`,
  `unexpected_tcp_reachability` and `permission_inactive`.
  It rechecks canonical Project, active Workspace, approved source,
  original plan hash and permit/approver validity. If the source is
  unconfigured, revoked or stale, it cannot produce a positive match.
  An interrupted/cancelled/timeout attempt is not a successful sample.
- [x] New API endpoints and Project Inspector controls let an authorised
  operator set/revise a baseline, browse immutable baseline history and
  compare the last TCP sample. The result always reports
  `device_health=unverified` and `monitoring_active=false`, never
  mutating Goal health, scheduling more probes or executing remediation.
  **Reachability equivalence is NOT proof of device security.**
- [x] Go/SQLite and API regression tests cover human-only baselines,
  stale revision denial, missing/changed expectation assessment,
  revoked consent, immutable history and no false health state.
  All runtime TCP tests in CI use a fake dialer (no external LAN access).
- [ ] Combined baseline Go/SQLite/WebUI/cross-platform validation is
  running at
  [#38194555709](https://github.com/DigiLogicTech/OnePane/actions/runs/38194555709)
  on code head `cac12b5a`. Earlier runs failed on a corrected Go
  `err:=` redeclaration; inspect any new failures and remediate.
- [ ] **Next tasks:** human-enabled recurring *real* reachability
  dispatch with renewable, precisely scoped leases and live pause/revoke;
  trustworthy source provenance and structured Observation admission;
  multi-signal baseline confidence, alert severity, diagnostics and
  bounded signed remediation/postcondition verification. Default
  must remain no autonomous network probing or fixing without
  independent consent. Cross-platform installed QA and release review
  remain mandatory.


## RC11 network permit integrity and evidence-class separation — 2026-10-12

- [x] **Security audit remediation:** migration
  `0064_goal_tcp_probe_permit_immutability.sql` rejects changing an
  approved permit's exact target IP, port, timeout, source, Plan hash,
  Project/Workspace/Goal, expiry or approver. Only a revisioned transition
  from approved to revoked is permitted. Any wider/new target requires
  a new human-approved permit. Regression explicitly attempts mutable
  retargets, port swap, source replacement and expiry extension.
- [x] **Authority liveness:** a TCP probe claim and its final pre-dial
  admission independently require the approving human to retain active
  tenant membership, in addition to a live operator, unchanged Goal,
  strict allowlist and currently approved exact target. Removal of
  human membership blocks a still-unexpired permit. Domain regression
  exercises revocation between claim and the socket boundary.
- [x] **Evidence visibility without evidence conflation:**
  `GoalEvidenceCoverage` projects most recent TCP handshake metadata
  separately from attached, integrity-verified Observation receipts.
  The presence of `tcp_reachability=connected` does not turn
  `evidence_state=missing` into `recent_attached`, and does not
  change `monitoring_active` or verified Goal health. Inspector labels
  this distinction explicitly. Source test fixture verifies separation.
- [ ] Verify the combined coverage source and script contract in
  [run #38194815212](https://github.com/DigiLogicTech/OnePane/actions/runs/38194815212).
  Earlier grant immutability and baseline increments passed hosted CI at
  [#38194688599](https://github.com/DigiLogicTech/OnePane/actions/runs/38194688599)
  and [#38194555709](https://github.com/DigiLogicTech/OnePane/actions/runs/38194555709).
- [ ] **Not yet delivered:** recurring real network probes (distinct
  opt-in consent and target-bound live leases), hardened external Node
  network boundaries, trusted Observation intake, security-health
  baselines beyond TCP reachability, autonomous incident policy and
  verified remediation. None of these may be inferred from a successful
  TCP connect. Pre-release second independent code/security audit,
  Windows/Ubuntu installed QA and packaging approval remain open.


## RC11 local-resource guard on manual network probes — 2026-10-12

- [x] A process-wide two-slot TCP-handshake admission limit now bounds
  concurrently open connections from human-triggered reachability tests.
  Waiting requests must respect cancellation and do not create a
  persisted claimed attempt, consume a source cadence window or open
  sockets until admitted. The subsequent network connect remains
  limited to 100–1500 ms and one approved private literal IP/port.
- [x] Tests verify cancellation under saturated slots; static UI/source
  contracts prevent wiring this manual network action into the daemon's
  existing unattended receipt-review scheduler.
- [x] The integrated goal coverage view shows TCP test status beside,
  but not instead of, attested immutable observations; final relevant
  combined source/contract head passed full CI in
  [#38194815212](https://github.com/DigiLogicTech/OnePane/actions/runs/38194815212).
- [ ] Final resource-pressure validation is pending at
  [#38194932570](https://github.com/DigiLogicTech/OnePane/actions/runs/38194932570)
  on test head `a0a68a4d`, then the eventual final manifest head.
- [ ] The distinction between manually initiated TCP tests and
  continuous device protection remains explicit. P0 future:
  separately consented recurring probes with durable cancellation
  leases, source integrity and health evaluation, precisely bounded
  remediation/rollback. No release merge/build is authorised.


## RC11 UI/UX convergence — mandatory pre-release review (2026-10-12)

**New release gate:** [RC11 UI/UX alignment plan](rc11-product-ux-alignment.md), requested before the next packaged QA build. A source/CI pass alone does not approve the interface. The design and application behaviour must express the present OnePane vision, not the original Alpha menu layout.

- [x] **UX-01 design brief and actionable inventory recorded:** canonical Project/Workspace hierarchy and isolated tool environments; local-first models and queues; Colibri / OmniRoute; unified assistant and task orchestration; Level 4 bounded automation by default versus separately consented Level 5 observation and remediation; honest evidence and access scope; themes, responsiveness, Inspector/Logs drawer, keyboard controls, Diagnostics and release-review sequence.
- [x] **UX-02 first shared-component increment committed:** `internal/webui/static/ux-alignment.css`, loaded after specialist page styles, aligns page headings, toolbar/button placement, theme-token surfaces and focus rings, responsive wrapping, reduced motion and independent Level 5 authority groups. It deliberately does not override shell height, widget drag/resize geometry or custom modal controls.
- [x] **Automated regression gate added:** `scripts/validate_product_ux_alignment.js` verifies token reuse, CSS precedence, responsive/reduced-motion styles, nav/source scope, L5 separation and the complete UX task manifest; it runs under the Project/Workspace development CI and is included in workflow path triggers.
- [ ] **UX-03–09:** inspect and fix each major page/route, one canonical nav state, controls/IA, model/offload lifecycle and authority state, theme/readability, drawer-aware responsiveness, keyboard/screen-reader semantics and connected visual QA. Screenshots and actual UI interactions are still required.
- [ ] **UX-10:** complete user-led code/vision realignment and second code review, fully green CI, then Windows/Ubuntu installed QA prior to any release approval. Keep PR draft until all gates are complete.
- [ ] **Latest CI:** validate combined UX source and contract at the branch head after workflow commit `19e317b9`. Prior migration/probe/test work is separate and should not be interpreted as visual QA.


## RC11 execution order clarified — 2026-10-12 (user decision)

**Order is mandatory:** (1) finish remaining OnePane feature and integration work
including actual L5 monitoring gates, autonomous project/runtime recovery,
isolated Workspaces, model/Node lifecycle and diagnostics; (2) complete
functional regression and first implementation/security review; **(3) as the
LAST product development pass before the next version/build**, conduct the
comprehensive user-centred UI/UX review, design realignment, appearance/
customisation implementation and responsive visual acceptance; (4) repeat
code/security review and full CI; (5) only then prepare an explicit Windows/
Ubuntu QA build for operator installation. No build or PR merge is authorised.

Earlier UX design notes and shared-style work are preserved as draft input;
**UX-03–UX-10 are deliberately deferred**, not considered complete. The
experimental presentation-preferences script was removed before wiring it
into Settings, to avoid shipping unfinished controls. There is no continued
active UI work until preceding functional/implementation gates are met.

**Resuming now:** Level 5 externally-consented observation pipeline and
trustworthy evidence handling. The existing manually approved bounded TCP
reachability check and passive receipt-review worker are different rights;
never treat either one as blanket recurring network consent.


## RC11 Level 5 recurring network observation — consolidation checkpoint (2026-10-12)

- [x] User-mandated development order retained: functional implementation and security review, **full UI/UX review last before next QA build**, post-UX code/CI verification, then Windows/Ubuntu packaging. The detailed UX plan is retained but no comprehensive UI review is being performed during the Level 5 implementation phase.
- [x] **Distinct recurring network consent:** a human explicitly approves a new, immutable, source- and exact-target-bound `TCPRecurringGrant` (schema `0065_goal_tcp_recurring_grants.sql`) in addition to the underlying manual permit. Revocation, short expiry, plan hash, original permit revision, operator membership and L5 mode independently constrain unattended use. Neither initial L5 goal registration nor a successful manual TCP test grants recurring access.
- [x] **Integration cleanup:** independently developed overlapping `0065_goal_tcp_recurring_consents.sql` and duplicate `tcp-recurring-consents` handlers were consolidated; the canonical `project_goal_tcp_recurring_grants` permission model is the sole recurring authorization path. Red CI from transient duplicate source references is not treated as current.
- [x] **Bounded implementation, pending final CI:** source `continuous_goal_tcp_recurring_worker.go` includes a dedicated system principal, SQLite cursor `0066_goal_tcp_recurring_worker_cursor.sql`, exactly bound `tcp4` target, ≤1500ms deadline, per-Goal/source/cadence idempotence, restart-safe claims and permit revocation checks at atomic claim and immediately before dialing. Integration tests use a fake dialer, not real network targets.
- [ ] **Activation gate:** until runtime bootstrap actually registers the dedicated system identity and `harnessd` schedules the worker *after all tests pass*, this capability is source-level only. No actual unattended device probe can be inferred from the mere presence of an approved grant record or source code.
- [ ] **Latest tests:** integrated recurring worker test head `bedb824800f` under [CI run #38196405337](https://github.com/DigiLogicTech/OnePane/actions/runs/38196405337) was running at checkpoint; a syntax-only test fixture error in older runs was corrected in `20248ddf`. Independently confirm latest success and remediate any new failures before enabling the worker.
- [ ] **Still required:** sensor provenance and trusted structured Observation admission, bounded baseline/alert handling with no false security-health claims, scoped remediation and postconditions, unattended resource-pressure QA, independent code review and final UI/UX review gate.


## RC11 L5 recurring TCP execution activated behind separate consent — 2026-10-12

- [x] The `system:tcp-recurring-observer` identity is now bootstrapped
  on daemon startup. A separate once-per-minute background loop calls
  `TickDueTCPRecurringProbes(ctx,1)`. It is **inert without an explicitly
  human-approved, currently valid `recurring_tcp_connect` grant** linked
  to an unchanged manual exact-IPv4/port permit. Manual permission alone
  never enables unattended connections.
- [x] An admitted check uses a bounded global two-slot TCP semaphore,
  at most one source per minute tick, a 100–1500ms TCP-only deadline,
  a durable source/cadence rate fence, persisted cursor and immutable
  claimed/final outcome. It revalidates canonical Project/Workspace,
  both active human approvals, system identity, plan hash, grant,
  precise target and revocation at the transaction and pre-dial boundary.
  A probe from a revoked/paused goal cannot be admitted; in-flight
  handshakes are bounded and cannot be retroactively unsent.
- [x] Fake-dialer integration verifies zero sockets before separate
  consent, exact target, repeated tick/restart deduplication, cadence
  continuation and revocation suppression. Go + WebUI + cross-platform
  source CI passed at
  [run #38196551825](https://github.com/DigiLogicTech/OnePane/actions/runs/38196551825);
  follow-up static safety contract passed
  [#38196619211](https://github.com/DigiLogicTech/OnePane/actions/runs/38196619211).
- [x] Coverage now separately exposes `tcp_recurring_authorized` and
  `recurring_tcp_authorized_sources`; it does **not** transform TCP
  handshakes into trusted Observation receipts or verified device health.
  A corrected shutdown/cancel classification is committed under
  `9d2e8d8e` and awaits final integration CI at
  [#38196741560](https://github.com/DigiLogicTech/OnePane/actions/runs/38196741560).
- [ ] Next functional gates: trustworthy sensor/Node provenance and
  authenticated structured observation intake; explicit baseline
  evaluation and risk/alert handling; multi-signal evidence verification;
  safe remediation/rollback and response approval; in-flight cancellation
  and resource-pressure QA.
- [ ] **Product review order remains unchanged:** full development and
  security review first; broad, human-focused UI/UX/customisation review
  **last before the next version/build**; then final CI and QA packaging
  after explicit operator approval. No release has been merged or built.


## RC11 complete TCP provenance and truthful reachability assessment checkpoint — 2026-10-12

- [x] Added schema `0067_goal_tcp_attempt_recurring_provenance.sql`: every automatic TCP attempt must reference its exact, human-authorised recurring grant. A manual TCP attempt cannot be retroactively promoted to an automatic one. Database triggers bind provenance immutably and deny inserting a system-origin attempt without its recurring consent.
- [x] TCP attempt records, API history and admission events now retain the originating recurring grant ID. The final pre-dial check requires the exact grant ID to match the durable claim; tests verify manual null provenance, claimed scheduled grant identity and rejection of post-hoc rewriting.
- [x] `GoalEvidenceCoverage` separately reports how many TCP sources are **currently authorized for recurring checks**, after checking current Project/Workspace status, grant, underlying exact target and active human approvers. This does **not** increase attested Observation coverage or mark Goal health verified.
- [x] Subsequent TCP baseline admission/assessment changes preserve the exact approved baseline revision that existed when the probe was claimed, so a baseline revised later cannot retroactively create a “matched” verdict. Regression records that distinction without network access.
- [x] Green hosted CI for recurring grant, fake-socket worker and daemon integration at [#38196551825](https://github.com/DigiLogicTech/OnePane/actions/runs/38196551825), authorization coverage at [#38196672387](https://github.com/DigiLogicTech/OnePane/actions/runs/38196672387), and immutable recurring attempt provenance at [#38196892436](https://github.com/DigiLogicTech/OnePane/actions/runs/38196892436). 
- [ ] Latest baseline-snapshot/drift-regression head `7f9ffa9d9ef` is under [#38197011353](https://github.com/DigiLogicTech/OnePane/actions/runs/38197011353) and requires final CI result before a completion claim. Any failure must be investigated and remediated.
- [ ] Next functional work: signed source integrity and structured observation lineage, defensible multi-signal incident criteria and alerting, bounded remediation authority/rollback/postconditions, Node/sandbox isolation and real hardware QA. TCP reachability is **not** proof of security or complete home-network protection.
- [ ] **Sequencing remains mandatory:** complete these functional/integration tasks and code/vision review first; perform the comprehensive UI/UX review and alignment as the **last pre-build product workstream**; rerun code/security CI; only then prepare the next Windows/Ubuntu QA version. No release merge or installer approval at this checkpoint.


## RC11 trustworthy TCP evidence chronology and drift notices — 2026-10-12

- [x] **Baseline cannot be applied retrospectively:** migration
  `0068_goal_tcp_attempt_baseline_snapshots.sql` records the exact
  human-approved baseline ID/revision **at the atomic TCP claim**. Old
  attempts retain revision 0; no forged backfill is performed. An
  operator changing a baseline after a handshake gets
  `baseline_updated_since_test` or `no_baseline_at_test`, not a new
  drift/health conclusion. Immutable snapshot trigger and first-test/
  revised-baseline/fresh-test integration regressions added.
- [x] **Consent provenance on the same attempt:** migration 0067 and
  its tests distinguish manual test receipts from unattended tests,
  which carry the exact independent recurring human grant ID.
  Tests cannot silently rebind this permission after a claim.
- [x] **Audited notice only:** migration
  `0069_goal_tcp_reachability_alerts.sql` records an immutable,
  deduplicated notice only when the completed TCP handshake disagrees
  with the **same baseline revision captured at admission**.
  It classifies only `unexpected_reachable` or
  `unexpected_unreachable`; it does not claim device compromise,
  verified health, or compliance, and never invokes remediation.
- [x] Human Project members can acknowledge notices via an
  independently authorised, CAS-fenced API; Agents cannot self-ack
  and acknowledgements do not resolve the underlying availability
  or authorise another network effect. Domain tests confirm no
  Tasks, no health state transition and an immutable notice history.
- [x] Baseline chronology + domain alert integration passed hosted CI
  at [#38197167929](https://github.com/DigiLogicTech/OnePane/actions/runs/38197167929)
  and [#38197425333](https://github.com/DigiLogicTech/OnePane/actions/runs/38197425333).
- [ ] Last combined API/contract checks are still running at
  [#38197483351](https://github.com/DigiLogicTech/OnePane/actions/runs/38197483351)
  and [#38197529356](https://github.com/DigiLogicTech/OnePane/actions/runs/38197529356).
  Do not claim branch complete until the final head passes.
- [ ] **Remaining functional P0:** trusted multi-signal Observation
  identity and integrity, in-flight revocation/Node-scope enforcement,
  alert confidence/severity/retention, human-authorised reversible
  remediation with postcondition checks, isolated Workspace sandbox
  and agent/model/Node runtime hardening. UI/UX review remains **last**
  before the next build, followed by full CI, installer QA and
  explicit release approval. No build or merge is approved.


## RC11 P0-B Tool effect-classification and Task scope corrections — 2026-10-12

- [x] **OCI image-store writes are Write effects:** the `project.app.pull`
  action uses the rootless container engine's `PullImage`, which mutates
  its local OCI image store. It now requires `files.write` in addition
  to `process.execute` and `network.connect`; explicitly read-only
  Workspaces reject the action even with valid Network/Execute grants.
  A gateway denial regression verifies neither authority consumption
  nor the adapter is reached. CI green:
  [#38197473239](https://github.com/DigiLogicTech/OnePane/actions/runs/38197473239).
- [x] **Capability-label impersonation denied:** a mutating or
  external-sending adapter may not pass using read-only
  `project.*.inspect` IDs or `filesystem.read` capability metadata.
  All known Project OCI/builder/file/Git Tool IDs are now bound to their
  exact registered authority modes; fake registrations with altered
  modes are unclassified and denied before consuming a lease.
- [x] **Unknown adapters are classified by mode before claimed capability:**
  a generic sandbox-executing `filesystem.read` adapter needs
  Read+Write+Execute, not merely Read; an external sender declaring
  `vault.read` requires Network+Secrets. Explicit audited built-in
  operations retain their narrower, fixed-command capability mappings.
  Gateway regression denies adapter invocation when either independent
  permission is absent.
- [x] **Taskless Project effects are denied:** `project.app.*`,
  `project.runtime.*` and `project.image.*` Tool IDs require an
  attached canonical Task. A tenant lease without a Task lacks a
  provable Project/Workspace/Agent permission chain. A present Task
  also requires an available persisted policy store; silent bypass
  when the scope database is nil has been closed. These are
  conservative denials, not broad implicit grants to Assistant chat.
- [ ] Final combined policy test head `52baadf50b30` is validating at
  [#38197865758](https://github.com/DigiLogicTech/OnePane/actions/runs/38197865758).
  Remediate any failure before considering this increment green.
- [ ] **Still P0-B:** inventory every other shell, MCP, Git, OCI, tool and
  direct GUI call site not using the Tool Gateway; verify attached Task
  permission chains at each external transport and effect boundary,
  safe standalone Assistant approval-to-Task conversion, and
  enforcement against sandbox/Node bypasses.
- [ ] **User-designated order unchanged:** functional development and
  security/vision review first; the comprehensive human-focused
  UI/UX and customisation pass LAST before building the next QA
  version; only then final CI, explicit packaging decision and
  Windows/Ubuntu installation/upgrade QA. No release approved.


## RC11 P0-B final Project Tool identity and Tool JSON admission checks — 2026-10-12

- [x] **Fixed built-in Tool mode integrity:** the allowlist binds all
  rootless OCI, Git, file, runtime and inspection Tool IDs to their
  actual registered action modes. An adapter may not advertise
  `project.runtime.inspect` with mutating or sandboxed execution,
  or `project.app.files.inspect` with unsandboxed read-only execution
  and bypass its actual adapter security boundary.
- [x] **Reserved Project adapter namespaces:** newly registered
  `project.app.*`, `project.runtime.*` and `project.image.*` IDs
  are denied until explicitly classified. This applies to legacy
  Tasks with no feature-policy migration, not just modern configured
  Tasks. The legacy exception does NOT allow Tool ID/mode spoofing.
  Tests cover real versus forged registration modes, legacy Tasks
  and unscoped attempts. CI
  [#38198111927](https://github.com/DigiLogicTech/OnePane/actions/runs/38198111927)
  passed the namespace and legacy-policy regression suite.
- [x] **Single-pass strict JSON Tool admission:** Tool inputs and
  adapter results now reject duplicate object keys at any depth
  (including Unicode-escaped equivalent names), multiple roots and
  excessive nesting before canonical hashing, lease consumption or
  adapter dispatch. A streaming-token single decode avoids a second
  full-sized payload decode, retaining support for legitimate
  large base64 file content without a 64KiB limit.
- [x] **Combined application/test CI passed** at
  [#38198334048](https://github.com/DigiLogicTech/OnePane/actions/runs/38198334048)
  on `42cc4e298206` (Go/SQLite integration, WebUI contracts and
  cross-platform checks). The consolidated manifest checkpoint also
  passed at
  [#38198372329](https://github.com/DigiLogicTech/OnePane/actions/runs/38198372329)
  on `f19d444620ea`. No installer, release merge or device QA is
  implied by these source-level results.
- [ ] These changes are **not** evidence that every direct shell,
  MCP, Node, adapter and GUI side effect has universal independent
  Write/Execute/Network/Secrets enforcement. Complete adapter
  inventory, caller/Principal/Task-scoped transport verification,
  recovery and hardware tests remain mandatory.
- [ ] User-ordered product gate remains: finish all functional work
  and first code/security review, then perform the comprehensive
  human-focused UI/UX and customisation pass LAST before the next
  release build, then rerun code/CI and seek explicit packaging/QA
  approval. PR #227 remains draft; no installer produced.


## RC11 P0-C truthful TCP dial outcomes and Taskless generic effects — 2026-10-12

- [x] **Network outcome classification corrected:** one shared helper is used by both human-requested TCP tests and independently approved recurring observations. A cancelled caller/adapter records `cancelled`, a deadline records `timed_out`, an ordinary connection refusal remains `not_connected`, and a buggy adapter yielding `(nil,nil)` can no longer forge `connected`. No endpoint or security-health claim is inferred from cancellation. No real network activity occurs in the tests.
- [x] **Regression coverage:** `continuous_goal_tcp_dial_outcome_test.go` tests valid in-process connection, missing connection, refusal, cancellation after a successful socket, cancellation from both contexts and deadline paths. The TCP source/test head `f63983b98caf` passed hosted CI [#38202192562](https://github.com/DigiLogicTech/OnePane/actions/runs/38202192562).
- [x] **Generic Tool bypass narrowed:** a production Tool Gateway with a persisted feature-policy store now rejects Taskless `ActionExecuteSandboxed`, `ActionMutate` and `ActionExternalSend` even when the adapter is not in a reserved `project.*` namespace. Side effects require a canonical Project/Workspace/Agent/Task chain rather than tenant-lease-only authority. Ordinary read-only activity retains existing lease and policy gates. The in-memory legacy test harness is unchanged.
- [x] **Additional policy regressions:** `taskless_generic_effect_denial_test.go` checks all three effect modes are denied before lease consumption/adapter invocation, while ordinary read remains possible. These denials deliberately do not create an implicit Assistant tool grant: future standalone Assistant actions must first create an approved, canonical Task.
- [x] **Combined Tool validation passed:** hosted CI [#38202404072](https://github.com/DigiLogicTech/OnePane/actions/runs/38202404072) completed successfully on Tool admission source/test head `6c1f808d2f71`; manifest checkpoint also passed [#38202442553](https://github.com/DigiLogicTech/OnePane/actions/runs/38202442553). This does not certify all adapters or runtime isolation.
- [ ] **P0 remains:** complete direct side-effect and GUI/MCP transport inventory, versioned Agent/Node revocation and trust, multi-signal signed Observation intake, incident criteria, bounded response permissions with rollback/postconditions, isolated sandbox execution and low-resource recovery testing. No claim of whole-home protection, complete autonomy or physical-Node validation.
- [ ] **Mandatory user development order:** functional/security implementation and review first; comprehensive user-centred, customisable UI/UX convergence LAST before next QA build; post-UX audit/CI and only then explicit operator-approved Windows/Ubuntu packaging/installed QA. No merge or installer authorised.


## RC11 P0-C TCP audit event tenant scoping — 2026-10-12

- [x] **Tenant-scoped security audit trail:** a claimed TCP test already recorded its canonical tenant Workspace ID. The finished attempt and its derived reachability-drift notice previously omitted that ID, preventing ordinary Workspace-scoped readers from seeing the final result while leaving metadata unscoped. The finalisation transaction now resolves the Project's persisted tenant Workspace and stamps both events consistently.
- [x] **No widened disclosure or privilege:** this adds tenant scope, not probe permissions, network effects, validated health, or automatic remediation. The prior exact target/source grants, revision and observation-class restrictions remain unchanged.
- [x] **Direct integration regressions** assert two matching claim+finish events with correct Workspace scope and zero unscoped copies, and that a single derived reachability notice likewise has its canonical tenant. Tests exercise SQLite through the fake TCP dialer without touching real network targets. Source/test head `52739825ded1` is validating under [#38202635593](https://github.com/DigiLogicTech/OnePane/actions/runs/38202635593); **do not treat it as green until independently confirmed**.
- [ ] After source CI, complete source-signed Observation admission/anti-replay, coherent multi-signal evidence rules, bounded incident response and postcondition verification, universal Workspace/Node side-effect enforcement, then the separate code/security review. Full UI/UX and customisation is STILL the final development pass before user-approved QA packaging; no release merge or installer is authorised.


## RC11 P0-A signed-source integrity foundation — 2026-10-12

- [x] **Key/ledger migration `0070`:** no retroactive trust upgrades. Human-approved Ed25519 public key is scoped to exact L5 Project, canonical Workspace, Goal, source ID and enrolled service/Agent principal, approved source plan hash, operator, expiry and live revocation. SQLite CAS/immutable history and one-active-key-per-source constraints are enforced; private keys are never persisted in OnePane.
- [x] **Atomic signature admission:** a separately authenticated source proves content ownership with versioned, domain-separated Ed25519 signature over exact Project/Goal/signer/Observation/integrity hash/monotonic sequence. The serialized writer transaction rechecks live Project/Workspace, both principals and role types, source plan, chronology, key expiry/revocation and source identity. Proof plus sequence advancement commit together. Foreign tenant data, forged/duplicate/replayed/cross-goal signatures, stale keys and revoked memberships are denied.
- [x] **Truthful evidence projection:** manual attachments and cryptographically source-authenticated proofs are separate; revoked keys lose CURRENT source-authenticated coverage without erasing immutable historical proof. No device-health, protection, automatic monitoring or remediation claim. Signed proof history has a read-only Project-authorised metadata-only API; no write/submit privilege is exposed by that route.
- [x] **Validation:** base source service/schema and regression [#38203538876](https://github.com/DigiLogicTech/OnePane/actions/runs/38203538876), coverage [#38203684151](https://github.com/DigiLogicTech/OnePane/actions/runs/38203684151), and live principal-role revocation regression [#38203802519](https://github.com/DigiLogicTech/OnePane/actions/runs/38203802519) passed. Read-only API smoke run [#38203944469](https://github.com/DigiLogicTech/OnePane/actions/runs/38203944469) found the legacy generic Goal transition route intercepting an invalid POST request with HTTP 400 rather than read-only HTTP 405. A specific deny-write route was committed and **full combined CI passed** at [#38204111470](https://github.com/DigiLogicTech/OnePane/actions/runs/38204111470). This is source-level assurance, not approved packaging or installed/physical hardware QA.
- [ ] **Not yet operator-active:** narrowly authorised HTTP key enrolment/sensor submission, cryptographically authenticated paired Node transport and device measurement identity, resource bounds for long proof histories, incident confidence/baselines, safe scoped remediation/rollback and actual Windows/Ubuntu/Node hardware QA. No general Project write permission may silently substitute for a source-only submit capability. Design evidence: [signed source provenance contract](rc11-signed-source-observation-provenance.md).
- [ ] **Order preserved:** complete functional/security work and first code review, then comprehensive human-focused UI/UX and customisation review LAST before next QA build, then final code/security/CI and operator-approved packaging. PR #227 remains draft; do not merge or release.


## RC11 signed-source key rotation and strict JSON ambiguity checks — 2026-10-12

- [x] **Revoked-key rotation tested:** revoke a source's Ed25519 identity, preserve prior immutable proofs and sequence watermark, explicitly approve a distinct new public key for that Goal/source, then accept a fresh proof from sequence 1 of the new key only. The old signer remains revoked. Full application/SQLite/cross-platform CI [#38204321403](https://github.com/DigiLogicTech/OnePane/actions/runs/38204321403) passed.
- [x] **Ambiguous Observation JSON is fail-closed:** a streaming, depth-bounded token validator now rejects duplicate JSON object fields (including Unicode-escaped aliases), multiple roots and excessive nesting before Observation recording. The same check runs when an existing Observation is integrity-verified, so a legacy hash with ambiguous JSON cannot be treated as authenticated sensor data. Errors do not reveal private field names.
- [x] **Negative tests:** valid nested arrays and ordinary values still pass; duplicate root/nested/Unicode aliases, secret-bearing error messages, invalid roots and deep arrays are rejected. A SQLite regression constructs a legacy immutable Observation with duplicate fields and a correctly matching SHA-256 and confirms that integrity verification still fails.
- [x] **Validated:** strict-JSON source/test head `6a1feb751e8f` passed full hosted CI [#38204547439](https://github.com/DigiLogicTech/OnePane/actions/runs/38204547439), including evidence admission/legacy regression, Go/SQLite integration, full Go tests and cross-platform compilation. This does not certify installed systems or physical Nodes.
- [ ] **Next:** independently authenticated, capability-scoped service submission and key enrolment UI/API; trusted paired-Node measurement/identity; multi-signal incident assessment and safe postcondition-checked remediation; low-RAM long-history validation. No autonomous protection or installed QA claim. Preserve the full user-centred UI/UX review as the final pre-build development gate.


## RC11 P0-B source-only HTTP submission — 2026-10-12

- [x] **Production bearer path:** machine service/Agent source submission demands a real API bearer identity with non-empty credential ID and the separate `goal.source.submit` capability for the canonical Workspace. `project.read`/`project.write` alone, web-session cookies, arbitrary `actor_principal_id`, unregistered source identities, foreign Workspace scopes and cross-Goal signatures cannot replace this check. The human-only signer approval/revocation APIs retain `project.write` plus domain human/Workspace verification.
- [x] **Operator visibility:** bounded human-only public signer-key inventory and live revocation/audit status; machine submit principals cannot list keys or change them. Proof history remains separate read-only metadata. New mutation bodies are limited to 8 KiB and reject ambiguous/duplicate JSON aliases, unknown fields and surplus roots.
- [x] **Proven by actual bearer credential tests:** tests seed SHA-256-hashed API credentials with exact Workspace and capability scope and exercise full HTTP route admission, real SQLite source-key approval, proof submission, replay, forged caller/sequence denial, live signer revocation, metadata-only secret redaction, and invariant `monitoring_active=false`. The first source-submit HTTP integration after import fix passed full Go/SQLite/cross-platform CI [#38205260233](https://github.com/DigiLogicTech/OnePane/actions/runs/38205260233). Key inventory and operator audit tests passed [#38205335116](https://github.com/DigiLogicTech/OnePane/actions/runs/38205335116).
- [x] **Durable ledger throughput constraint, source under test:** schema 0071 adds indexed per-signer recent proof lookup; the serialized SQLite proof admission transaction checks a maximum of 30 admitted signatures in the preceding 60 seconds per signer. A rejected attempt must not advance the signer sequence or persist a signed proof.
- [x] **Quota CI passed:** source/test head `2e7dcf79` passed full hosted [CI #38205561832](https://github.com/DigiLogicTech/OnePane/actions/runs/38205561832), including source quota/no-sequence-consumption negative tests, Go/SQLite integration, full Go regression and Windows/Ubuntu compilation. The API now returns HTTP 429 and `Retry-After: 60` on this domain quota. HTTP credential revocation and 429 status tests were subsequently added; newest combined code head CI still requires independent verification.
- [ ] **Not complete:** this endpoint signs an existing *locally recorded* Observation, rather than accepting/attesting a raw remote Node sample; needs a separate data-ingress admission, credential-provisioning UX and issuance with least privilege, anti-abuse limits on rejected attempts, authenticated paired-Node measurement provenance, device/incident multi-signal confidence, postcondition-tested bounded recovery and installed QA. Technical contract: [signed source submission API](rc11-signed-source-submission-api.md).
- [ ] **Build sequencing binding:** finish functional/security and first code review, perform comprehensive human-focused UI/UX/customisation pass **LAST** before QA build; final code/CI verification and operator-approved Windows/Ubuntu installers afterwards. No merge/release/installed QA yet.


## RC11 P0-C bounded two-step source reading admission — 2026-10-12

- [x] **Staging read-in (not source trust):** new `POST /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations/record` requires the same source-only machine bearer credential with `goal.source.submit` as proof submission. It binds the source/actor to the real bearer principal and the human-enrolled exact Goal/signer ID, permitted SourceID, canonical tenant Workspace, still-active operator and signing key/plan. No browser session or generic `project.write` substitute.
- [x] **Zero-trust data labeling:** request only supplies `signer_id`, `observation_type`, bounded strict JSON `value`. The application assigns server time, opaque `signed-source://<signerID>` subject, `INTERNAL` confidentiality, `TRUSTED_NODES` residency, `UNVERIFIED_DERIVED` trust and no Node origin. Raw bytes go into the immutable Observation store; response is only ID/hash/source/time plus `unverified_awaiting_source_signature`. Stage does **not** produce a cryptographic proof, schedule work, claim physical Node identity/health, or permit remediation. The signed proof must be separately confirmed by the private key.
- [x] **Conservative local resource budget:** migration `0072_goal_source_record_window.sql` persists a per-signer serialized 30-per-minute reservation counter before Observation writes; budget is not refunded if append later fails. Counter exhaustion gets HTTP `429 Retry-After: 60`. Signature proof admission keeps its separate rolling-minute indexed 30-per-minute bound; neither exhausted path advances accepted signed proofs.
- [x] **Negative end-to-end test added:** verifies real bcrypt-free hashed API bearer credential scope (SHA-256 token hash stored, never exposed), machine-only read staging, unknown Node/provenance field rejection, raw secret redaction, exact unverified label/trusted-node residency, oversized payload rejection, 30/31 reading rate fence, subsequent signed proof success, revoked signer/credential denial and no activated Goal. A separate test ensures session identities and bearer identities missing credential ID cannot submit.
- [x] **Prior full successful checkpoints:** signed-proof rate-window source head [#38205561832](https://github.com/DigiLogicTech/OnePane/actions/runs/38205561832), bearer credential revocation path [#38205702681](https://github.com/DigiLogicTech/OnePane/actions/runs/38205702681), and cross-tenant signed-proof rejection [#38205865079](https://github.com/DigiLogicTech/OnePane/actions/runs/38205865079).
- [x] **CI verified green:** complete reading-ingress source+test/document contract [#38206259494](https://github.com/DigiLogicTech/OnePane/actions/runs/38206259494) **passed** (Go/SQLite integration, API, full Go regression, cross-platform compilation, source contracts and MSI log reader). Earlier head [#38206085045](https://github.com/DigiLogicTech/OnePane/actions/runs/38206085045) had a transient unrelated `TestVersionedAgentPolicyAPIEnforcesScopeAndHumanDenialRemoval` SQLite ping 15s timeout under CI load; it did not recur in this verified latest application checkpoint.
- [x] **Paired Node is not origin proof:** new negative Go/SQLite regression records a source-signed Observation labelled with an existing `harness_nodes.trust_state='paired'` remote Node ID and verifies signed admission still fails; claimed Node ID + signature is not a measurement transport attestation. Full hosted source/API/Go/SQLite/cross-platform CI [#38206467252](https://github.com/DigiLogicTech/OnePane/actions/runs/38206467252) **passed** for the paired-Node origin-denial regression.
- [ ] **Unfinished:** first-class operator provisioning and revocation UX for machine API credentials; source-data/Node identity assurance over authenticated paired-Node transport; rejected-attempt and concurrent multi-principal anti-abuse admission control; exact postcondition-checked L5 recovery playbooks; high-volume/low-RAM sustained load; installer+hardware QA. Keep broad UI/UX review **last** before packaging and do not release/merge early.


## RC11 P0-D provisioned machine signer credentials — 2026-10-12

- [x] **Credential issuance foundation:** schema `0073_goal_source_api_credentials.sql` stores an immutable signer/Goal/Project Workspace/tenant source-credential binding and human approver, exact expiry and revocation. API credential token material is generated using 32 CSPRNG bytes, returned **once** over operator-authorised issuance HTTP, and stored only as SHA-256 in the existing `api_credentials` table. Event/audit metadata, credential listing, revocation and historical proof records exclude the secret token.
- [x] **Human-only operator control:** `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/credentials` requires authenticated human, `project.write`, live Workspace/Goal/signing-key authority and source membership. Issue only `goal.source.submit` for exactly the Project's tenant Workspace, a finite 5-minute-to-7-day lifetime no longer than the approved signer key, and at most two simultaneously active unrevoked key credentials. Signed-source credentials do not grant Project write or Node operations.
- [x] **Operator inventory/revoke:** human `project.write` controls matching `GET .../credentials` and `POST .../credentials/{credentialID}/revoke`, with bounded metadata-only list and immediate transactional revocation of both grant and existing bearer-token credential. Credentials separately revoked via the generic backing table are reflected as revoked in the operator inventory.
- [x] **Final writer enforcement:** both source-record reservation and signed proof acceptance recheck the bearer credential's exact signer/Goal/tenant/source principal binding, finite expiry, live signer status, revocation and exact non-wildcard workspace/capability scope **inside** the SQLite writer transaction, following the HTTP authentication gate. A generic `project.write`, wild-card scoped token, wrong signer or old unlimited-expiry legacy token no longer substitutes for an approved source credential. Existing internal non-HTTP service calls remain a separate trusted interface without a bearer credential; do not expose them as HTTP shortcuts.
- [x] **Expanded HTTP regression:** real CSPRNG-minted token stages a source reading *before* operator revocation; minted token then loses admission immediately, cannot list/manage credentials, never appears in audit events, listing or revoke results. Legacy seeded source fixture now has a finite expiry and explicit binder row. Existing source quotas, source-signing sequence and source-key revocation assertions remain in place.
- [x] **Full credential/HTTP integration passed:** [#38207832662](https://github.com/DigiLogicTech/OnePane/actions/runs/38207832662) validated real minted bearer use, signer-bound read staging, revocation, privacy and unchanged 30/min quotas, plus full Go/SQLite and cross-platform compile. Earlier run [#38207645343](https://github.com/DigiLogicTech/OnePane/actions/runs/38207645343) correctly rejected an older unbounded-expiry test token; the fixture was updated, not the production rule.
- [ ] **Still necessary:** secure initial service principal enrollment, credential-copy/privacy UX, credential rotation expiry warnings, reliable paired-Node mutually authenticated data and device measurement provenance, aggregate rejected-attempt caps, multivariate detection, bound/reversible L5 playbooks with verified postconditions, sustained low-resource load and physical Windows/Ubuntu QA. Comprehensive user-centred UI/UX review remains the **last pre-build functional workstream**; PR remains draft and unmerged.

- [x] **Human-readable live token states and preflight:** signed-proof admission now checks exact bearer/signer binding before reading Observation content and again at the durable writer transaction. Operator credential listing distinguishes `active`, `expired`, `revoked`, `signer_inactive`, `scope_inactive` and reflects externally revoked API credentials. New negative regressions verify two-active-credential limit, replacement after revocation, signer revocation state, strict metadata redaction and immediate external revocation projection.
- [x] **Live credential status and final HTTP regression passed:** code/test head `b074a339a76c96f3c107c14c64136d90a6ef43bb` full hosted CI [#38208329043](https://github.com/DigiLogicTech/OnePane/actions/runs/38208329043) **passed**, including source-key rotation/revocation states, operator inventory, SQLite/API integration, full Go regression and cross-platform compilation. Subsequent documentation-only checkpoint commits do not represent a separate installed QA or release.


## RC11 P0-E rejected source request protection — 2026-10-12

- [x] **Shared source HTTP attempt guard:** migration `0074_goal_source_request_attempt_budget.sql` adds a persisted credential-wide counter. `ReserveGoalSourceRequest` serializes a maximum of 60 requests over one 60-second fixed window for each active human-enrolled, exact Goal/signer-bound credential; both source `record` and `submit` route through it before strict JSON decoding, signature verification or Observation lookup. Invalid/failed submissions count against the budget and cannot be refunded or reset by clock rollback; the 61st is HTTP `429 Retry-After: 60`. Existing 30/min staged reading and 30/min successful signed proof caps remain independent.
- [x] **Security scope:** bearer authentication, canonical tenant authorisation, live signer/key/credential/single-capability identity, Project/Goal configured status and active service principal membership are required even to reserve; none of this elevates Node trust, reads raw Observation values or triggers monitoring/remediation. This is an exact credential request budget, not a general network/per-Project DoS guarantee.
- [x] **Regression added:** the newly issued and still active second machine bearer sends 60 deliberately malformed proof JSON bodies; the next malformed source reading across the **other** endpoint is denied with 429 and Retry-After. First machine source credential continues with its independent staged reading/proof/immutable receipt checks. Source/test head `c719cd3f693df30b169042f5c80487c24c3c4be7`, CI [#38208721460](https://github.com/DigiLogicTech/OnePane/actions/runs/38208721460).
- [x] **Full CI green:** source request attempt budget + malformed cross-endpoint regression code/test head `c719cd3f693df30b169042f5c80487c24c3c4be7` passed [#38208721460](https://github.com/DigiLogicTech/OnePane/actions/runs/38208721460), including Go/SQLite and HTTP integration, source contracts, full Go tests and cross-platform compilation. Documentation-only commits after this source head are tracked independently.
- [ ] **Next P0:** authenticated paired-Node telemetry transport with separately approved Node-origin attestation and immutable replay protection; conservative incident signal fusion/baselines and reversible human-approved L5 playbooks/postcondition checks; operator service identity bootstrap, rejected traffic shared per-Project quotas and runtime low-memory endurance. Preserve first code/security review, final human-focused UX/customisation workstream LAST before next QA build, and no merge/release until approved.


## RC11 P0-F authenticated paired-Node telemetry transport — 2026-10-12

- [x] **Separate human-reviewed mTLS source:** migration `0075_goal_node_measurement_transport.sql` adds immutable, exact Project/Goal/project-Workspace/source ID/paired Node/fingerprint-scoped human grants and append-only metadata-only connectivity receipts. Goal operator `project.write` approves/revokes; human `project.read` lists at most 100 grant/receipt metadata records. The approved source must belong to the configured Goal's allowlist. The grant must be active and bound to the Node's current independently confirmed pairing and pinned TLS client fingerprint, and the original human approver must still be active and a member.
- [x] **Federation-only ingress:** `POST /federation/v1/measurements` is handled by the existing pinned TLS federation service, NOT by Project HTTP bearer routes. The handler requires a current client certificate tied to the paired peer, refuses anonymous/mismatched or stale certs, strictly decodes at most 2048 unambiguous JSON bytes and rejects unknown or duplicate fields. The remote peer may submit only `peer_connectivity` and `reachable`/`unreachable`/`unknown`, server-bounded five-minute freshness and positive strictly monotonic `sequence`; no arbitrary targets, Node ID, hostnames, raw probes, scan requests, health claims or actions.
- [x] **Transactional durability and quota:** each admitted sample rechecks current Project/Goal/Workspace, cert, pairing, active human and grant in its SQLite writer transaction. It advances source/Node/Goal-specific monotonic sequence, enforces 30 receipt admissions per 60-second indexed sliding window, and appends an immutable tenant-audited metadata receipt. Replay returns 409, exhausted quota 429 with Retry-After 60. No existing local signer / Observation proof is upgraded by transport authentication and no L5 Goal health/monitor is activated.
- [x] **Operator and transport tests:** actual SQLite/HTTP tests cover human-only exact source grant, optimistic revision and revoke, spoofed Node/health fields, duplicate JSON keys, replay, 30/min budget, immutable receipt history, live human-approver suspension, withdrawn Node trust and no automatic monitoring. A live local TLS client-certificate handshake exercises anonymous rejection and independently paired sender acceptance. Full hosted source/API/Go/cross-platform CI [#38210006950](https://github.com/DigiLogicTech/OnePane/actions/runs/38210006950), [#38210229521](https://github.com/DigiLogicTech/OnePane/actions/runs/38210229521) and [#38210409483](https://github.com/DigiLogicTech/OnePane/actions/runs/38210409483) all **passed**. The first new test failed only due to an incorrect event-column reference, fixed without relaxing transport checks.
- [x] **Explicit Node sender implementation:** migration `0076_node_outbound_measurement_sequences.sql` reserves monotonic per-local Node/recipient/explicit grant `sequence` in SQLite. `nodefederation.Service.SendPeerNodeMeasurement` sends a single caller-supplied self-connectivity state over existing pinned client-certificate TLS, with no automatic scheduling or network probe. Sequences persist after restart, failed send or timeout, with at most 32 tracked grants per destination on a low-resource Node. The sender never auto-discovers other Goal grants.
- [x] **Sender CI verified:** real two-Node pinned mTLS/independent SQLite restart and revocation e2e code/test head `d70858322a7127ab96f273d7cb894e1bc4fd2c3b`, full hosted [CI #38210692162](https://github.com/DigiLogicTech/OnePane/actions/runs/38210692162), **passed** with Go/SQLite, full regression and Windows/Ubuntu cross-compilation. No installed/real physical remote Node QA has been performed.
- [ ] **Still P0, not claimed:** self-reported mTLS peer identity is NOT physical device integrity or external network-device measurement provenance. No automatic background sender, worker scheduling, live monitoring, network scan, incident-confidence evaluation or remediation has been activated. Next work: human-provisioned Node grant handoff and controlled sender invocation, real measurement tool identity, source-to-Node attestations, cross-signal incident criteria, reversible approved L5 actions and postcondition verification, admission-rate soak/retention testing, full code/security review. Local-first human-centric UI/UX alignment remains LAST before the next QA build, then final CI and Windows/Ubuntu installer/hardware QA. See [transport design](rc11-paired-node-measurement-transport.md).

## RC11 P0-G paired-Node request budget and ingress backpressure — 2026-10-12

- [x] **Low-resource bound:** migration `0077_node_measurement_request_budgets.sql` persists a per-Node authenticated **request** budget across all Goal grants. The mTLS receiver authenticates the paired client fingerprint, then atomically reserves one of 60 attempts per fixed 60-second window **before strict JSON decoding**. Malformed and failed requests spend budget. A 61st attempt returns HTTP `429 Retry-After: 60`, separate from the 30-per-minute successfully admitted receipt quota per exact human Goal grant. The SQLite budget cannot be deleted or decremented within the same window; server clock rollback is not a quota reset.
- [x] **Revocation precedence:** paired Node trust, dual confirmation and certificate fingerprint are checked before budget reservation. A withdrawn peer gets an authorisation denial even when its earlier attempt budget was exhausted. Existing grant admission independently rechecks human reviewer and Node trust under writer transaction; no side effects, Task or Goal-health promotion.
- [x] **Adversarial test committed:** integration case submits 60 malformed JSON envelopes with the same valid paired TLS identity and checks the 61st gets 429 with no signed Observation, measurement receipt, or unbounded audit response. Also checks Node trust withdrawal. Source/test head `13786a974a411121902d8d80a3abd81528499209` full hosted [CI #38211019093](https://github.com/DigiLogicTech/OnePane/actions/runs/38211019093) **passed**.
- [x] **Migration-trigger source head green:** `0c02ca24b05e9e2287d10a647aa65f02badfb91d` full hosted [CI #38211068502](https://github.com/DigiLogicTech/OnePane/actions/runs/38211068502) **passed**, including SQLite migration triggers, real Node federation integration, full Go regressions, frontend contracts and Windows/Ubuntu cross-compilation. This is source/CI evidence only, not installed physical Node QA.
- [ ] **Next P0:** real external measurement identity / source binding beyond the now-verified paired Node *transport*, per-Project aggregate bandwidth/connection caps, manually consented bounded probe execution, corroborated incident classification and reversible pre-approved recovery with postcondition proof, high-throughput/endurance on low RAM. Then initial code/security review and last pre-build human-facing UI/UX/control-customisation pass; do not merge/release or claim a working L5 home-network defender.


## RC11 P0-H current Node telemetry grant status — 2026-10-12

- [x] **Truthful operator backend state:** `ListGoalNodeMeasurementGrants` now separates persisted human review `status` from `effective_transport_state`. The latter is computed against current Goal/Project/Workspace activity, the human approver's active membership, certificate fingerprint and two-sided live paired Node state. Values are `active`, `expired`, `scope_inactive`, `approver_inactive`, `peer_unavailable` or `revoked`; never reinterpret this authority state as device health or active protection.
- [x] **API regression added:** an approved Goal Node grant is initially effective-active; when a paired peer becomes unavailable its metadata changes to `peer_unavailable`, without rewriting immutable approval. After human revocation it reads `revoked`. Code/test head `8d684f9d2a16d47b88d4ccac44f4b886e404f90d`, [#38211477418](https://github.com/DigiLogicTech/OnePane/actions/runs/38211477418).
- [x] **Status projection verified green:** full hosted source/API/SQLite/integration/Go/cross-platform CI [#38211477418](https://github.com/DigiLogicTech/OnePane/actions/runs/38211477418) **passed** for code/test head `8d684f9d2a16d47b88d4ccac44f4b886e404f90d`. Latest manifest-only commit CI must be checked separately.
- [ ] **Remaining goals unchanged:** independently verifiable real device/source measurement (beyond merely authenticated peer transport), secure operator grant handoff and consented bounded monitoring, evidence-fusion incident classification, reversible user-approved remediation with postconditions, low-end stress tests, code/security review, and human-centred UI/UX/control redesign LAST before next QA build. No merge/installers/releases yet.


## RC11 P0-I local connectivity evidence triage — 2026-10-12

- [x] **Human-only read-only signal comparison:** `internal/projectworkspace/continuous_goal_signal_triage.go` and `GET /v1/projects/{projectID}/continuous-goals/{goalID}/signal-triage` expose *metadata only* from current human-approved bounded TCP handshake attempts and pinned-mTLS peer connectivity receipts. Authenticated human `project.read` required; remote Node/Agent tokens, cross-tenant and POST attempts cannot gain action authority. Sources derive from exact configured Goal allowlist (max 32); at most one qualified current receipt per class/source, only the previous 15 minutes. No raw target IP, port, payload, password or network effect.
- [x] **No false incident/device trust:** triage state explicitly remains nonverified, with `monitoring_active=false`, `device_identity_verified=false`, `remediation_authorized=false`. `multiple_adverse_indicators_investigate` requires at least two *different source IDs* and actual adverse states in **both** TCP-connect and Node self-report classes. Two bad Node claims with a healthy TCP sample cannot masquerade as independent cross-class adverse evidence. Such indicators are NOT measurements of the same physical device and not a confirmed attack or security incident; human investigation only.
- [x] **Live provenance and revocation:** TCP proof must match the current Goal plan hash, exact human-approved IPv4/port/timeout, permit revision, approval chronology, live approver membership and configured Goal. Node receipt must match live approved Goal grant, exact paired Node/certificate, source and post-human-grant timestamp. Paused/retired Goals return `goal_not_configured`; revoked/suspended evidence remains in immutable histories but cannot count toward current triage.
- [x] **Domain and HTTP tests:** real SQLite/Go regression seeds *test-only* metadata, checks pre-approval exclusion, one-class/cross-class state, safe source receipt IDs, no target/secrets, reviewer/Node downgrades, foreign tenant, preserved `unobserved` Goal health, and paused-Goal handling. HTTP test denies anonymous, foreign human, Agent callers and POST. Pure table-driven classifier test rejects overcount from duplicate classes or healthy TCP. Design: [read-only triage contract](rc11-level5-signal-triage.md).
- [x] **Green source checkpoint:** after respecting existing one-TCP-attempt-per-cadence uniqueness, full Go/SQLite/HTTP, frontend source validators and Windows/Ubuntu compilation CI [#38216358887](https://github.com/DigiLogicTech/OnePane/actions/runs/38216358887) **passed** for source/test head `07ffd26dabbe09df1f28a9acab62dd1cba679257`. The initial test heads failed on an intentionally preapproval Node timestamp and then a duplicate synthetic TCP cadence slot; both tests were fixed without weakening production guards.
- [x] **Provenance and paused-Goal source CI:** first [#38216544748](https://github.com/DigiLogicTech/OnePane/actions/runs/38216544748) failed because a test searched for digits `443` anywhere in generated IDs/timestamps; the privacy test was corrected to assert absence of the actual `target_ipv4`, `target_port`, timeout and credential fields instead. Corrected full Go/SQLite/API/source/cross-platform [CI #38216748106](https://github.com/DigiLogicTech/OnePane/actions/runs/38216748106) **passed**, code/test head `f5642ba5c2d5258f099c48305c1bdb4a86ebd105`. This includes strict permit plan/target/revision and postapproval evidence chronology, paused Goal classification, human-only read access and cross-tenant isolation.
- [ ] **P0 next:** establish independently attested external device/instrument identity and explicit operator-approved shared-target correlation; bounded consented real probing, reliable multi-vantage independence, calibrated incident confidence, operator-approved reversible response with rollback/postcondition evidence, high-volume low-resource soak, full code/security review. Last functional workstream before next QA installer remains comprehensive human-focused UI/UX and customisation review; no premature merge/release or claimed operational L5 home-network defender.

## RC11 P0-J interrupted TCP triage and live Node grant allowlist — 2026-10-12

- [x] **Live Node grant reporting honesty:** the `effective_transport_state` projection now additionally requires the approved source still appear in the current Goal plan and requires the same canonical Project Workspace. Otherwise immutable approval remains but live state is `scope_inactive`. Human operator API regression changes the stored plan source and checks effective state without rewriting grant history. Source/test commit `bbfb5359e25639d3a01e87f454e2ed305d7fe1ad`; earlier CI on this head was affected by unrelated unfinished triage fixture tests, not a confirmed grant-query defect.
- [x] **No resurrected TCP signal:** Goal triage now reads the newest **claimed** approved TCP attempt, including the in-progress/cancelled state. A newer unfinished or cancelled attempt is `unknown_after_interruption` and **masks older** completed results, rather than falsely counting stale `not_connected` as current negative evidence. Current exact source, grant plan, timestamp and tenant checks remain.
- [x] **Adversarial source/API tests:** standalone classifier table tests ensure positive TCP cannot corroborate two adverse Node sources; Goal integration test verifies a newer claimed TCP attempt changes multi-source investigation to a single adverse peer indicator while preserving the historical TCP receipt, and paused Goal returns explicit `goal_not_configured`. No sockets created by triage.
- [x] **Interrupted-attempt CI verified:** initial [#38216957135](https://github.com/DigiLogicTech/OnePane/actions/runs/38216957135) correctly failed a new regression because the SQL query excluded `claimed` receipts before selecting the latest attempt; source fix `051f8cc4a703478238aba0a9a98b76f4119ffd52` moves completion chronology validation AFTER newest-claim selection. Full hosted [CI #38217153843](https://github.com/DigiLogicTech/OnePane/actions/runs/38217153843) **passed**, including Go/SQLite integration, full regression, source contracts, Windows/Ubuntu compilation and existing MSI log reader. Older completed TCP probes no longer resurrect stale negative evidence behind a newer interrupted claim.
- [ ] **Still P0:** human-designated canonical device/instrument identity and authentic physical measurement (paired Node identity alone insufficient), independent same-device target corroboration, bounded operator-consented verification and response, calibrated incident confidence, reversible Level 5 playbooks with postcondition validation, low-hardware endurance, code/security audit and full human-focused UI/UX alignment **LAST before QA build**. PR stays draft/unmerged; no installer or live home-network defence claim.

## RC11 P0-K operator-declared exact TCP target provenance — 2026-10-12

- [x] Human-assigned endpoint identity: migration 0078_goal_tcp_declared_target_bindings.sql adds immutable, revocable, time-limited (5min–24h) operator_declared_tcp_endpoint identity for one exact pre-approved TCP permit, Goal, source, private IPv4, port and plan hash. This is NOT physical device attestation. Declaration never opens a socket or starts monitoring.
- [x] Human-only API: POST/GET /v1/projects/{projectID}/continuous-goals/{goalID}/tcp-declared-targets and POST /{targetID}/revoke; project.read/write + live human reviewer required. Status and effective_identity_state are separate (active, expired, approver_inactive, permit_inactive, revoked). Target IP/port are hidden from this bounded inventory and triage; immutable historical records retained.
- [x] Exact postapproval triage attribution: TCPDeclaredTargetID/IdentityClass appear only for an already-consented exact target/permit, active reviewer, unchanged Goal plan, and TCP claim STRICTLY LATER than human target approval. Same-millisecond claims fail closed; target revocation removes current label but preserves underlying immutable TCP test. Paired-Node source remains explicitly paired_node_transport_self_report; cross_class_target_correlation is always not_established.
- [x] Negative real SQLite/HTTP tests: no Agent/anonymous/foreign operator access, no duplicate JSON key, unsafe labels or stale revision, no retroactive label, exact source/plan revocation, no raw target IP/port, append-only scope and no Goal health/Task/remediation side effects.
- [x] CI verification: core target API full CI #38219850132 succeeded; effective identity and changed Goal plan CI #38220468886 succeeded. A later test caught ambiguous equal-millisecond preapproval receipt wrongly receiving an identity, production changed to strict later-than comparison. Fixed code/test head f4a419bbf95df6244d55c64cf06853fb93a346ad full CI #38220709022 succeeded (Go/SQLite/API/source contracts, Windows/Ubuntu compilation).
- [ ] Latest documentation-only head 32fffc91cc4fe4f8cdb11cf25569cd85f19e3e24 CI #38220749219 was pending at checkpoint. Verify current head independently.
- [ ] Next: independently pinned endpoint public-key proof or authenticated device instrument, user-approved SAME-target correlation, calibrated incident criteria, reversible low-risk bounded response with rollback/postconditions, stress, code/security review, and comprehensive human-focused UI/UX/control/customisation LAST before next QA build. Do not merge PR, package release or imply active L5 network protection yet.

Detailed design: docs/rc11-operator-declared-target-provenance.md

## RC11 P0-L independently pinned TLS certificate possession — 2026-10-12

- [x] Human-approved, out-of-band certificate pin: migration 0079_goal_tls_endpoint_pins.sql binds one immutable SHA-256 leaf-certificate fingerprint to a declared, exact Goal TCP endpoint/permit/monitoring plan. 5min–24h expiry never outlasts permit or declared identity, one current pin per target, historical audit immutable; human Project.write approval and revoke, Project.read bounded metadata only.
- [x] Explicit bounded TLS verification: POST /v1/projects/{projectID}/continuous-goals/{goalID}/tls-pins/{pinID}/verify is human-only and rechecks active Goal, exact TCP target, private literal IPv4, allowed source, permit, all original human reviewers, current pin and plan. There is no supplied URL/host/IP/port, DNS, redirects, request body to the remote, auto-scanning or scheduled monitor. TLS1.2+, exact constant-time human-approved SHA-256 DER pin, cert validity; no unpinned fallback. Network connect + handshake honor the original 100–1500ms timeout.
- [x] Durable low-RAM attempt control: only 2 concurrent explicit TLS verifications per process, one transactionally reserved check per pin/five-minute fixed window (including failure); replay cannot dispatch a second socket. Claim and final completion are audited; results are immutable claimed/matched/mismatched/tls_failed/unavailable/cancelled with no target/certificate PEM/credentials in new event payload. Revoke or live approver loss pre-dispatch prevents network access, and a mid-flight change suppresses matched result before commit.
- [x] Historical vs live operator authority: pins show immutable status separately from effective_pin_state (active/expired/approver_inactive/target_inactive/revoked), including current Goal plan and exact target/permit. Listing uses bounded queries closed before further SQLite reads, suitable for single-connection low-RAM deployment.
- [x] Honest read-only triage: new tls_check_state, tls_check_id, tls_pin_id, tls_identity_class=operator_pinned_tls_certificate and tls_certificate_possession_matched show only recent, live-approved certificate claims. Pending check masks older matched results; revoked pin disappears from current view but remains in immutable history. TLS certificate possession DOES NOT prove physical device identity, share a target with the paired Node, count as incident corroboration, mark security health or authorise remediation. cross_class_target_correlation remains not_established.
- [x] Real TLS and negative tests: local TLS server exact certificate match/mismatch, invalid fingerprint, RFC1918 literal-only, bounded timeout and no unchecked fallback; HTTP human-only pin/verify/revoke, no arbitrary address injection, expired/revoked/changed-plan state, 5-minute replay protection, metadata audit privacy, SQL immutable receipt and no Goal health promotion; Go/SQLite end-to-end handshake and triage from two different receipt types.
- [x] Verified CI checkpoints: first source/API TLS verification #38224388764 passed. Full real TLS/Go/SQLite integration initial #38224789794 failed a privacy assertion that conflated older human TCP-permit target-bearing events with the NEW TLS event family; test was scoped to TLS event types rather than weakening historic audit. Corrected full CI #38225008266 passed. Distinct live-authority pin and triage tests #38225330516 passed full Go/SQLite/source contracts and Windows/Ubuntu cross-compilation.
- [x] Final operator inventory source head 7ca028ea78fef431b2bd74da2c0f90694f1a0260 full hosted CI #38225602255 PASSED, including changed-plan/revoked live pin status, real Go/SQLite TLS triage integration, human-only API, frontend source validations, full Go regression and Windows/Ubuntu cross-compilation. Later documentation HEAD is independently validated before a release claim.
- [ ] Still P0: independent EXTERNAL device/instrument challenge/signature or trusted physical-device identity, approved same-target binding between sources, calibrated incident alerts, reversible human-authorised Level 5 remediation with postcondition verification, aggregate bandwidth and low-RAM soak, full code/security audit. Human-focused UI/UX/customisation remains LAST pre-next-build development workstream; no release merge, installer, installed hardware QA or operational autonomous home-network defender claim.

Design: docs/rc11-tls-certificate-possession.md


## RC11 P0-M independently enrolled instrument key challenge/response — 2026-10-12

- [x] **Human-reviewed out-of-band source key:** migration `0080_goal_instrument_challenges.sql`, Go `internal/projectworkspace/continuous_goal_instrument_challenges.go` and API `internal/api/project_goal_instrument_challenges.go` introduce an independently enrolled Ed25519 **public** instrument key bound to exact active Goal/Project Workspace/current canonical monitoring plan, a human-declared private TCP target and its currently approved TLS pin. Key expiry is at most 24h and never later than pin/target/permit. One live instrument key per TLS pin. The application never generates or sees the instrument's private key and never assumes a physical device actually has it.
- [x] **Human-only signed challenge:** `POST /instrument-keys/{keyID}/challenge` emits crypto/rand 32-byte nonce and exact domain-separated `OnePane/instrument-challenge/ed25519/v1` signing bytes binding Project, Goal, key, challenge, declared target, TLS pin and fingerprint, monitoring plan, issue time and 2-minute expiry. Human operator imports a detached Ed25519 signature through `POST /instrument-keys/{keyID}/prove` (Project.write required). Server revalidates **all** current human reviewers and scope under SQLite transaction, verifies exact signature, consumes challenge once and atomically persists immutable metadata proof/event. Replay, forged key, signature for another nonce, stale/expired/revoked source, cross-Project and cross-tenant scope rejected. No public Agent or source-submission endpoint and no unattended polling.
- [x] **Bounded append-only resource usage:** four challenges per key per rolling 5min and **128 lifetime challenges per key**. Key, nonce ledger and proof history cannot be deleted or scope-mutated. Bounded 100-entry human-readable key/proof listings. After key, pin or target revocation, historical cryptographic proofs remain but new challenges and CURRENT triage attribution are suppressed. No raw target IP/port, private key, sensor reading or certificate PEM in proof metadata/audit payloads.
- [x] **Independent forensic verification:** accepted immutable ledger now retains detached 64-byte signature (public material) and digest; human-only `GET /instrument-proofs/{proofID}/verification-bundle` reconstructs and rechecks the original signed bytes from immutable enrollment, TLS pin and nonce. Returns public key, exact signing payload, detached signature and immutable proof so any independent Ed25519 verifier can reproduce the result **after revocation**. Never claims physical-device/instrument-location integrity, real measurement accuracy or safe network conditions.
- [x] **Explicitly non-voting triage:** `GoalSignalTriage` exposes `recent_signed_challenge` plus key/proof IDs and `human_bound_instrument_key_possession` only if all current human key, TLS pin and permit/Goal reviewers remain active with exact monitoring plan; source contributes **zero** incident votes. `cross_class_target_correlation=not_established`, `device_identity_verified=false`, `monitoring_active=false` and `remediation_authorized=false` always. Independent instrument key possession is NOT evidence it sampled that target or a particular physical device.
- [x] **First verified CI:** core Go/SQLite signature, adversarial replay/forgery/foreign Project, nonce budget and revocation [#38231282520](https://github.com/DigiLogicTech/OnePane/actions/runs/38231282520) **passed**; end-to-end human-only API signature import [#38231463440](https://github.com/DigiLogicTech/OnePane/actions/runs/38231463440) **passed**; live pin/plan inventory and independent non-voting triage [#38231776814](https://github.com/DigiLogicTech/OnePane/actions/runs/38231776814) and [#38231962470](https://github.com/DigiLogicTech/OnePane/actions/runs/38231962470) **passed** with Go/SQLite/full Go/frontend contracts/cross-platform compile.
- [x] **Offline proof export source green:** app/test head `589eb8da02c83a0c42e476f0cef86bf8ef1985f5` full hosted [CI #38232599028](https://github.com/DigiLogicTech/OnePane/actions/runs/38232599028) **passed**, including Go/SQLite/API signing and post-revocation export, frontend contracts, full regressions and Windows/Ubuntu compile. An additional audit-hardening pass persists the **exact original signing_payload bytes** (immutable in challenge ledger), verifies the original signed envelope against immutable Project/Goal/key/challenge/target/TLS-pin/plan/nonce/time fields and stores a detached 64-byte signature for offline verification without reserialising future JSON versions; source/test [CI #38233198737](https://github.com/DigiLogicTech/OnePane/actions/runs/38233198737) **passed**. New schema constraints on plan and signature digest prefixes and expiry chronology at `63741a32ba0d285294024df4201ee0704725975c` [#38233452886](https://github.com/DigiLogicTech/OnePane/actions/runs/38233452886) are still validating at this checkpoint; check and remediate any failure before considering the schema gate green.
- [ ] **Remaining P0:** a real approved physical device/instrument with independently verified association to the target, authenticated signed *measurement* including specific observed asset semantics, trustworthy source independence and calibration, safe operator-consented low-impact checks, incident-confidence model, reversible separately-approved Level 5 actions with verified rollback/postconditions, low-hardware endurance and full code/security audit. Human-focused control/customisation/UI/UX review remains **LAST before next QA build**, followed by final CI/packaging and Windows/Ubuntu installed physical QA. No release merge or claim of autonomous home-network protection.

Design: [independent instrument challenge](rc11-independent-instrument-challenge.md).


## RC11 P0-M audit immutability final verification — 2026-10-12

- [x] **Version-stable signed evidence:** `0080_goal_instrument_challenges.sql` persists the **exact** original issued `signing_payload` bytes as an immutable 128–2048-byte BLOB, in addition to nonce, key, expiry and the accepted detached Ed25519 signature. A later OnePane JSON encoder change cannot silently destroy the audit. Admission checks that the live two-minute payload exactly matches the v1 canonical envelope; historical export validates the stored signed envelope's domain, Project/Goal/key/challenge/declared target/TLS pin/certificate/plan/nonce/issue/expiry claims against immutable DB rows, then independently verifies detached Ed25519 and signature SHA-256 digest.
- [x] **Schema integrity:** explicit `sha256:` digest-prefix/length checks and accepted_at strictly inside the issued challenge's expiry window, non-deletable key/challenge/proof rows, challenge status only `issued→accepted`, no mutable signature or signed bytes. Human-readable signed proof history remains bounded and metadata only; the separate human-only bundle provides public verification material without private instrument key or private endpoint IP.
- [x] **Proof-bundle version-stable CI:** code/test head `973dc164d00bb7390ef425fdb4b5f89e047da468` full [#38233198737](https://github.com/DigiLogicTech/OnePane/actions/runs/38233198737) **passed** with immutable original bytes, offline Ed25519 verification across revocation, no foreign read/Agent access, tenant Go/SQLite and Windows/Ubuntu cross-compilation. Schema canonical digest/expiry hardening head `63741a32ba0d285294024df4201ee0704725975c` [#38233452886](https://github.com/DigiLogicTech/OnePane/actions/runs/38233452886) **passed**.
- [x] **Final audit event test green:** initial [#38233776037](https://github.com/DigiLogicTech/OnePane/actions/runs/38233776037) correctly caught a regression TEST that expected seven events before the human revocation call, when six were correct. Production audit semantics were unchanged. Corrected test head `7e68a66dae5a67cd566b851cecfb94cd753427eb` asserts six events pre-revocation and seven post-revocation, plus no private target in newly added instrument audit events. Full hosted Go/SQLite/API/source contracts, cross-platform compile and opt-in MSI log-reader [CI #38234012973](https://github.com/DigiLogicTech/OnePane/actions/runs/38234012973) **PASSED**. Latest documentation-only branch HEAD must be checked separately.
- [ ] **Remaining security features are NOT implied by cryptography alone:** this proves ownership of a human-enrolled public-key pairing, not authenticated location of the instrument, physical device measurement, security health or same-asset/source corroboration. Next implement a narrowly scoped out-of-band *instrument-signed measurement* with target binding, provenance/trust-zone checking and independent verification before any incident confidence or human-approved reversible defensive operations. UI/UX review remains LAST before next QA installer, then installed hardware QA.


## RC11 P0-N human-imported signed instrument outcome claims — 2026-10-13

- [x] **Exactly one scoped signed self-report per nonce:** immutable SQLite migration `0081_goal_instrument_signed_measurements.sql` and `internal/projectworkspace/continuous_goal_instrument_measurements.go` add a deliberately narrow `endpoint_tcp_connect_claim` with four exact outcomes (`connected/not_connected/timed_out/unknown`). Existing 32-byte random Ed25519 challenge and 2-minute expiry + 4 per 5min / 128 lifetime per key budgets are reused; the exact challenge is atomically consumed across BOTH key-possession and measurement admissions. No arbitrary source-provided target IP, network command, shell, host, URL, tool effect or measured payload.
- [x] **Verifiable and fully consented:** `PrepareGoalInstrumentMeasurement` computes exact server-owned v1 domain-separated signing bytes including the original immutable challenge, Project/Goal/key/challenge/target/TLS-pin/source/measurement type/outcome/observed time. Human writer imports an external Ed25519 signature via `AcceptGoalInstrumentMeasurement`, which rechecks current canonical Goal plan, exact human-approved target/permit/TLS pin, original/current human reviewers, nonce status and chronology, then transactionally verifies signature, consumes nonce and stores immutable proof + tenant event. Pre-challenge/future time, forged signature, changed outcome, other Project, cross-type replay and post-revocation use denied.
- [x] **Operator API with isolated permission:** POST `.../instrument-keys/{keyID}/measurement/prepare` and `.../measurement/accept` require authenticated **human** `project.write`. GET `.../instrument-measurements` and GET `.../instrument-measurements/{claimID}/verification-bundle` require human `project.read`. Query bounds LIMIT 100; JSON strict-decoded. Only public key, signed outcome, source and provenance IDs, timestamps and detached signature are exported; no private key, raw IP/port or network response bytes. Bundle re-verifies Ed25519 signature, digest, stored original challenge, approved TLS fingerprint and exact signed source/target semantics, even after historical human revocation.
- [x] **Strict no false device/security claims:** `proof_class=instrument_self_report_signed_not_attested`; all receipts deny observer-vantage attestation, device identity, same-physical-target correlation and remediation authority. Goal human-only triage can show `recent_instrument_self_report_unverified` only when every original key, TLS pin, target, permit, Project/Goal plan and reviewer remains active. Signed `not_connected` contributes ZERO negative incident votes; old proof disappears from CURRENT triage after human key revoke while signed history remains immutable/readable.
- [x] **Go+SQLite+HTTP adversarial tests:** real Ed25519 independent key, wrong-signer, altered `not_connected` to `connected`, prechallenge timestamp, cross-tenant Project, use-once challenge across key-possession and measurement proof types, no duplicate admission, immutable metadata history, independent offline verification post-revoke, human/Agent/foreign API reads/writes, no Goal health or incident changes. Code/test source head `7693518ef9b4a4c5b004cdd33f93784d3392e49e` full hosted [CI #38299392436](https://github.com/DigiLogicTech/OnePane/actions/runs/38299392436) **PASSED**, including real Go/SQLite signature/nonce/triage and human API tests, frontend contracts, full regression, Windows/Ubuntu cross-compilation and opt-in MSI log reader. Initial [CI #38299014674](https://github.com/DigiLogicTech/OnePane/actions/runs/38299014674) failed only a synthetic instrument event-count expectation, fixed at `9eda1bb4cc542c0bc5bf47500ca77409dcd02772`: replacing one unused challenge kept the total challenge events constant, with one new accepted measurement event (7 pre-revocation, 8 afterward). Documentation-only branch-head CI must be independently checked before release readiness.
- [x] **Historical proof bundle scope hardening verified:** source commit `9138f302479b97fc67ff40434ba74b4a291c79f3` enforces original Project/Goal challenge context, enrolled TLS-pin-to-target identity and human source ID/plan join while exporting immutable signed measurement bundles—even after later key revocation. Full [CI #38299937390](https://github.com/DigiLogicTech/OnePane/actions/runs/38299937390) **PASSED** with Go/SQLite/HTTP and signed self-report/nonvoting-triage integration, frontend contracts, full regression, Windows/Ubuntu compiles and MSI log reader.
- [ ] **Next P0 not delivered:** no trusted actual measurement runtime or hardware instrument attestation; human-declared target association is **not** independently authenticated physical-device identity or proven vantage. Need real device/instrument proof, approved same-device target correlation, calibrated incident confidence, verified human-approved reversible Level 5 actions/rollback, low-RAM endurance and security/code review. Human-focused UI/UX/customisation review stays LAST before next Windows/Ubuntu QA installer, followed by final CI and physical installed QA. Draft PR remains unmerged and NO autonomous home-network protection is claimed.

Detailed technical contract: [instrument signed measurement claims](rc11-instrument-signed-measurement-claims.md).


## RC11 P0-O opt-in one-shot local instrument runner — 2026-10-13

- [x] **Local-first actual TCP-connect execution path:** `internal/instrumentprobe/probe.go` and `cmd/onepane-instrument-probe/main.go` add a strictly opt-in, single-host CLI with separate `keygen`, `probe`, and `sign` modes. It is NOT a service, an L5 background Task, autonomous scanning, remediation, automatic telemetry producer, physical-host attestation or installed release. `probe` opens exactly ONE short TCP4 connection to one explicitly entered canonical private IPv4 literal and port (original permit 100–1500ms), no DNS/public/loopback/link-local/CGNAT, range enumeration, HTTP, application writes or retries. It refuses an expired/consumed/ambiguous/noncanonical challenge and requires an operator-provided confirmation of the human-declared target ID before opening any socket.
- [x] **Local instrument key custody and signed receipt:** new `keygen` creates a never-overwritten local 64-byte Ed25519 private key file with owner-only `0600` on POSIX, prints ONLY public key + SHA256 fingerprint for independently approved OnePane enrollment. CLI refuses non-regular/malformed/overly accessible POSIX private-key files. On Windows, NTFS ACL restriction is an explicit operator responsibility. The local probe immediately signs its canonical report with outcome/observed time and operator-selected literal IP/port; these private address fields stay in the local `0600` receipt and are never sent to the OnePane signed-measurement API.
- [x] **Human review, no automatic submission or false vantage:** operator must still call authenticated OnePane `.../measurement/prepare` with the *actual local probe* outcome/time and then run CLI `sign`. `sign` verifies the original local report's signature, original human challenge and its exact Project/Goal/key/target/TLS-pin, then checks canonical server-prepared v1 signing bytes for exact source/outcome/timestamp; refuses wrong key, expired nonce, altered outcome, forged vantage flag, mismatched TLS pin and noncanonical payload. The resulting JSON detached signature request must be submitted by the human through existing `.../measurement/accept` which independently verifies all live scope. OnePane does NOT know that operator-entered IPv4 matches original permit or that the CLI genuinely executed on a particular physical host: the signed result remains `instrument_self_report_signed_not_attested` and gets ZERO incident votes or Level 5 remediation authority.
- [x] **No extra runtime dependencies:** uses Go stdlib, already existing human-only instrument challenge/measurement APIs, no Node/VPN/hardware dependency added, tiny/no background worker memory use. Hermetic `internal/instrumentprobe/probe_test.go` uses dialer injection to check exactly one TCP socket, RFC1918-only/TTL/target ID restrictions, connected/refused outcome, signed local receipt, wrong key, changed outcome, altered response, changed pinned TLS identity and nonce expiry. CLI `main_test.go` tests keygen file handling, owner-only POSIX mode, no overwrite, invalid key denial and exact serialization compatibility with actual `projectworkspace.GoalInstrumentChallenge` and `InstrumentMeasurementSigningPayload` types.
- [x] **All application source CI green:** one-shot runner/one-socket/rejection suite `e3bbde084b1f1e03631d3db625fe2c0a207e14ad` [#38301190849](https://github.com/DigiLogicTech/OnePane/actions/runs/38301190849) **PASSED**; local keygen/0600/no overwrite `3d46bcd4f6d8f0cc7c7dbff10dc845a4f6e6c5b3` [#38301356671](https://github.com/DigiLogicTech/OnePane/actions/runs/38301356671) **PASSED**; exact real OnePane API challenge/prepare JSON serialization and original TLS pin binding `4fc5153e3327fd0fa7b8379ac1c9dd88f6246a70` [#38301682479](https://github.com/DigiLogicTech/OnePane/actions/runs/38301682479) **PASSED**. Each includes Go/SQLite source integration and full regression plus Windows/Ubuntu cross-platform compile. Documentation-only head requires independent final CI.
- [ ] **Next P0 actual platform assurance:** OnePane can now execute a human-initiated one-shot TCP dial in source code, but it cannot independently authenticate its physical host/observation vantage or prove the operator-entered literal target is the same physical asset as a paired Node. These require explicit approved Node/instrument pairing and transport-enforced execution witness/certificate binding, target provenance and physically installed QA, followed by calibrated independent source correlation, low-noise alerts and separately human-authorised reversible Level 5 actions with rollback/postconditions. Low-resource soak and full code/security audit remain open. Complete human-centric controls/customisation UI/UX pass stays LAST before next Windows/Ubuntu QA build; no premature merge/installer.

Operator procedure: [one-shot instrument runner](rc11-one-shot-instrument-runner.md).


## RC11 P0-P live human-approved endpoint plans and challenge v2 — 2026-10-13

- [x] **One-shot human-reviewed target plan:** `internal/projectworkspace/continuous_goal_instrument_probe_plan.go`, `internal/api/project_goal_instrument_measurements.go`, route `POST /v1/projects/{projectID}/continuous-goals/{goalID}/instrument-keys/{keyID}/probe-plan` (human Project.read, strictly decoded `challenge_id` + `expected_goal_revision` only). In one SQLite-scoped read transaction, recheck authenticated active human and original human approvers, configured Project/Goal/Workspace, exact source/plan, human TCP permit, declared target, TLS fingerprint/pin, independently enrolled instrument key, unused/short-lived challenge. Derive literal RFC1918 IPv4, exact port, 100–1500ms timeout and immutable challenge digest from ORIGINAL grant, never user-supplied IP/URL/hostname. This export does NOT create a socket, Task or new nonce, and private address is never exposed in machine-readable generic triage, model context or signed audit history.
- [x] **Plan-based runner is default:** `cmd/onepane-instrument-probe` accepts human-authenticated `--plan plan.json` plus original challenge, refuses any mixed manual address/port/timeout or manual downgrade flag, and validates Project/Goal/key/nonce/target/TLS pin/source identity and precise expiry before ANY network access. Missing `--plan` fails closed. Legacy manually typed private-address workflow requires explicit `--allow-manual-target` for operator QA only. Plan itself is NOT OnePane server-signed; trusted receipt over authenticated channel remains essential.
- [x] **V2 endpoint fingerprint in immutable challenge:** `internal/projectworkspace/continuous_goal_instrument_challenges.go` now issues domain `OnePane/instrument-challenge/ed25519/v2` with SHA256 of `OnePane/target-endpoint/v1|<canonical_ipv4>:<port>` from human-approved permit, not target chosen by local CLI. `internal/instrumentprobe/probe.go` independently computes hash from plan and verifies it equals the original signed challenge's endpoint fingerprint before opening the socket. Altering only plan's RFC1918 address or port must trigger zero socket calls, even when other IDs stay valid. Legacy v1 proof/measurement bundles remain independently verifiable, but legacy v1 challenges do not satisfy stronger plan-based v2 execution; reissue on upgrade rather than downgrade authority.
- [x] **Private low-resource CLI file handling:** private signing key and private probe-plan/local receipt inputs require non-symlink regular files with owner-only mode on POSIX, bounded input bytes (key 256B, JSON 8KiB), and Lstat+opened-file Stat/`os.SameFile` checks before limited read to prevent symlink-swap TOCTOU. Windows requires manually restricted NTFS ACL and still lacks hardware/instrument host identity attestation. Generated private files are exclusively created with owner-only mode, never overwritten. No persistent service, background memory or model-mediated network calls.
- [x] **Adversarial tests:** human/anonymous/Agent/foreign-tenant probe-plan API, no caller target override, wrong/stale Goal revisions, foreign challenge and consumed nonce; client tampered Project/key/TLS pin/nonce/expiry/plan digest, changed RFC1918 socket IP and port, public/loopback/metadata target and timeout; real local TCP4 listener reached via test-only exact-target dialer (not internet), exactly one connection and authentic Ed25519 local receipt. Tests also cover deliberate manual fallback selection, strict API Go JSON compatibility, private file/symlink/shared-mode denial, bounded input sizes, historic legacy-v1 plan rejection and v2 scope.
- [x] **All current app/source CI green:** full earlier source checks [#38305010394](https://github.com/DigiLogicTech/OnePane/actions/runs/38305010394), [#38305153375](https://github.com/DigiLogicTech/OnePane/actions/runs/38305153375), [#38305397256](https://github.com/DigiLogicTech/OnePane/actions/runs/38305397256), [#38305499189](https://github.com/DigiLogicTech/OnePane/actions/runs/38305499189), [#38305720670](https://github.com/DigiLogicTech/OnePane/actions/runs/38305720670), and [#38305770470](https://github.com/DigiLogicTech/OnePane/actions/runs/38305770470) **PASSED**. Latest versioned v2 endpoint-hash/legacy-v1 distinction source commit `7ad44da0dfa66a4656433e195f99c73885ddd0cd` passed full hosted [CI #38306272999](https://github.com/DigiLogicTech/OnePane/actions/runs/38306272999), including SQLite/Go integration and self-report history, hermetic and real-socket instrument tests, signed payload/versioned domain checks, API human/Agent permissions, source validators, full Go regression and Windows/Ubuntu cross-compilation. The latest documentation-only branch-head run is checked independently before claiming full branch-head success.
- [ ] **Remaining physical assurance:** the client authenticates only human permission and the original endpoint identity fields, **NOT** the physical Node running the probe, original trusted transfer of both challenge and plan, the network vantage or physical device. Attackers who can edit BOTH transferred documents can still induce an off-target local socket before admission fails; no hardware attestation root/server signing certificate is pinned by the CLI. Next P0: independently authenticated paired-node execution witness and target/vantage provenance, calibrated cross-vantage incident evidence (avoid false corroboration), human-approved reversible L5 response with postcondition rollback, low-RAM endurance, code/security review. Human-focused UI/UX remains LAST before QA installer, no draft merge or release.

More detail: `docs/rc11-one-shot-instrument-runner.md` and `docs/rc11-independent-instrument-challenge.md`.


## RC11 P0-P immutable private target-plan access — 2026-10-13

- [x] Migration `0082_goal_instrument_probe_plan_access.sql` records **one immutable topology-export receipt per human Principal per challenge**, containing only Project/Goal/instrument/challenge, actor and challenge-SHA256 metadata. The service uses one serialized SQLite transaction to verify ALL live human/Project/Goal/key/permit/TLS pin/target/plan/challenge grants, derive the private endpoint, insert the unique audit row and append the first-export event. Subsequent reads of the same unused nonce by the same human return the same current approved plan without duplicate events or unbounded log growth. Neither table nor event stores raw internal IPv4/port.
- [x] Strict authenticated human Project.read endpoint with unknown-field/duplicate JSON denial; tests require blocked Agent, anonymous and foreign-tenant reads, stale Goal revision, foreign/consumed challenge, no client-selected IP, correct scope binding, idempotent audit, immutable export ledger and redacted events. No automatic poller, route scanner or host effect.
- [x] Human-transfer source integrity strengthened: `OnePane/instrument-challenge/ed25519/v2` seals a SHA256 digest of the ORIGINAL human-approved canonical IPv4+port. `--plan` client checks the endpoint digest and all original IDs before a socket; tests reject a *different but also RFC1918-valid* endpoint and altered port, as well as v1 legacy probe plans. Retain historical v1 detached proof and measurement bundle verification, but do not grant old v1 nonce new v2 plan authority.
- [x] Local instrument file reads use one open file descriptor, stat/inode identity checks and strict 8KiB plan/receipt and 256B key limits to prevent symlink-swap TOCTOU and oversized JSON/key allocation; owner-only POSIX mode required for sensitive files. No claim of Windows ACL enforcement or hardware attestation.
- [x] **Verified v2 and export audit source CI**: versioned endpoint challenge source `7ad44da0dfa66a4656433e195f99c73885ddd0cd` [#38306272999](https://github.com/DigiLogicTech/OnePane/actions/runs/38306272999) **PASSED**, including historical v1/v2 proof bundles, real-socket TCP and altered-plan rejection. Metadata-only private-target export audit source `e1fb1536009b79526c909597c31238d2f7427523` full hosted [CI #38306757760](https://github.com/DigiLogicTech/OnePane/actions/runs/38306757760) **PASSED**, including one-entry-per-human/nonce immutable ledger, repeat-read idempotency, redacted audit event, human-only restrictions, complete Go/SQLite and source/HTTP regression, Windows/Ubuntu cross-compilation and opt-in MSI reader. Latest documentation-only branch-head CI must be verified separately.
- [ ] Next P0 explicitly remains independent host/Node execution attestation, signed raw instrument ownership/vantage and trusted transfer of the original human plan/challenge. This increment does **not** prove physical device health, network attack, incident/source independence or authorise Level 5 remediation. Before QA build: correlation and calibrated alerts, reversible human-approved L5 playbooks, small-hardware soak and full source/security review; human-focused comprehensive UI/UX/customisation review LAST. No merge/release/installer.



## RC11 P0-R Project-wide instrument nonce budgets and true rolling TLS cadence — 2026-10-13

- [x] **Project aggregate low-resource challenge issuance budget:** migration `0083_goal_instrument_project_challenge_budget.sql` indexes `project_id,issued_at DESC`; issuance now enforces **16 challenges in a rolling five-minute period** and **256 in a rolling 24-hour period** across all current/revoked/rotated instrument keys under the same Project, on top of existing four/5min and 128 lifetime per key. The checks and nonce creation share the existing serialized SQLite transaction, preventing concurrent Project issuers bypassing the cap. No new background workers, scanners, trust elevations or permanent Project disablement. Excess requests return human-only HTTP 429 with explicit 300/86400-second `Retry-After`.
- [x] **One Project cannot exhaust another:** integration test seeds 16 and 256 immutable synthetic challenge receipts across the same Project and verifies exact separate five-minute/daily error classifications, unaffected other Project, and automatic eligibility restoration after the 24-hour window; HTTP unit test verifies 429 mapping/retry headers. These limits protect immutable nonce ledger/audit storage against rapid key rotation but do NOT cap long-term historical accumulation/archival.
- [x] **Clock-boundary TLS cadence corrected:** previously one pinned TLS connection was allowed per *fixed* five-minute wall-clock bucket, permitting two real network effects seconds apart across a boundary. A new rolling query on the prior immutable `claimed_at` timestamp is enforced **before socket dispatch**, while retaining the original bucket uniqueness constraint for concurrent claims. Migration `0084_goal_tls_check_rolling_cadence.sql` indexes `(pin_id,claimed_at DESC)`; deterministic Go/SQLite regression checks adjacent-bucket denial and expiry after a full five minutes. This was discovered when existing human API regression failed on an actual fixed-bucket boundary in CI #38321497422; production logic was fixed, not the test weakened.
- [x] **Verified hosted CI:** Project aggregate enforcement source `db191d014f3e5c3d060d8b77de5a4edff7b6dd0f` [#38321362428](https://github.com/DigiLogicTech/OnePane/actions/runs/38321362428) **PASSED**. Combined challenge/TLS source at `9cbefbff798d7d2da4151e247ad4077d0a2f459b`, and TLS documentation head `1a8abb03afa07549c0fb299a052ca7a1505bd8fc`, completed [#38321740648](https://github.com/DigiLogicTech/OnePane/actions/runs/38321740648) and [#38321788691](https://github.com/DigiLogicTech/OnePane/actions/runs/38321788691) **PASSED**, including original Go/SQLite/API instrument and pinned TLS tests, source contracts, full Go regressions, Windows/Ubuntu cross-compile and MSI log reader. Current manifest-only head should be separately checked.
- [ ] **Still not complete:** physically authenticated paired-Node/instrument execution/vantage, trusted original challenge/plan transfer, approved same-target physical asset correlation and evidence independence, calibrated false-positive incident confidence, reversible human-approved Level 5 recovery with rollback/postconditions, low-RAM endurance/long-term bounded audit archival and full security/code audit. Human-focused UI/UX/customisation pass remains LAST before QA installer and installed Windows/Ubuntu hardware QA. Draft PR stays unmerged; no active autonomous home-network defence claim.

Contract: `docs/rc11-instrument-project-challenge-budget.md`, `docs/rc11-tls-certificate-possession.md`.


## RC11 P0-S v2 manual endpoint parity and per-host nonce anti-rerun — 2026-10-13

- [x] Full hosted CI [#38322732883](https://github.com/DigiLogicTech/OnePane/actions/runs/38322732883) PASSED on source/test/doc head `52da423c4ae685589cc0f95db45151015ee0546e`. One atomic Git tree commit replaced the prior high-latency one-file/one-CI pattern.
- [x] v2 challenge endpoint SHA-256 digest now enforced before manual TCP socket AND before post-hoc signed admission; a different RFC1918 IPv4 or port cannot inherit an exact approved v2 target ID. Legitimate historic v1 proof bundles retain historical readability, not present-day attested endpoint or trusted-sensor authority.
- [x] Follow-on local per-key anti-rerun CLI nonce ledger, private key self-check and invalid-directory handling passed full hosted CI #38323225758 at `d68cc4aeccb240e819233017877ed971145d7671`. The ledger is only accidental-retry protection on the chosen host and is not tamper-resistant, global, or independent hardware attestation.
- [ ] Remaining P0: trusted paired-Node source execution witness + real instrument/target provenance, consented and calibrated cross-source incident decisions, reversible human-approved Level 5 actions with rollback/postconditions, bounded small-hardware soak, initial code/security audit, and LAST comprehensive human-centred UI/UX/customisation pass before Windows/Ubuntu QA installers. Draft unmerged, no autonomous protection claim.


## RC11 P0-T — one-shot local IO durability hardening (2026-10-13)

- [x] Canonicalise local instrument key parent directory before deciding which per-key replay ledger holds a nonce. Regression denies absolute/relative/symlinked-parent aliases, but deliberately makes no tamper-resistant or across-host claim.
- [x] fsync POSIX parent and ledger directory metadata around first nonce reservation; decline probe when local crash-durable claim cannot be confirmed.
- [x] Exclusively reserve a private receipt output **before** consuming any nonce or opening the single socket. Existing or missing destinations fail closed and cannot silently overwrite evidence; partial failed outputs are not signed receipts.
- [x] The P0-T one-shot local IO durability commit `4170b7b8a3f69847465f905f35f1ab339aa1bf43` passed full hosted CI [#38333680445](https://github.com/DigiLogicTech/OnePane/actions/runs/38333680445), including full Go regression and Windows/Ubuntu cross-compilation. No installed QA, device attestation, automatic monitoring, remediation authority or release approval is implied.
- [ ] Next P0: independently approved paired-Node execution/vantage binding and trustworthy same-target correlation, then calibrated incident logic, bounded consented reversible playbooks with verification/rollback and low-resource endurance/security review. UI/UX is the final development review before human-approved packaging.


## RC11 P0-U — human-independent offline local receipt signature review (2026-10-13)

- [x] Add an offline-only `verify-receipt` command requiring a separately known, canonical Ed25519 enrollment public key and private local receipt file. No private key, network connection, retry, automatic work or target export is involved.
- [x] Verify original canonical challenge, chronologically valid signed observation and approved v2 endpoint fingerprint, target and Ed25519 signature; historical verification must not refresh an expired challenge or be interpreted as an authenticated device observation.
- [x] Negative tests for wrong public key, changed outcome, observation time, target/port/target ID, nonce and signature.
- [x] Subsequent full hosted evidence-approval/regression CI [#38342053187](https://github.com/DigiLogicTech/OnePane/actions/runs/38342053187) PASSED. Physical Node execution/vantage remains unverified.

## RC11 P0-V–AC — plan-bound Node evidence custody and explicit one-shot execution (2026-10-13)

- [x] **P0-V/W original human approval and pinned transport integrity:** receipt verification requires the separately trusted original challenge, human-approved private probe plan and independently enrolled Ed25519 public key; paired Node sender refuses a contradictory acknowledgement. CI [#38341408595](https://github.com/DigiLogicTech/OnePane/actions/runs/38341408595), [#38341587633](https://github.com/DigiLogicTech/OnePane/actions/runs/38341587633) PASSED.
- [x] **P0-X original challenge file private:** offline review and local probe refuse shared POSIX original challenge file; fixture fix and exact-head hosted CI [#38342053187](https://github.com/DigiLogicTech/OnePane/actions/runs/38342053187) PASSED.
- [x] **P0-Y immutable original Goal plan generation:** migration 0085 pins Node telemetry approval to the canonical plan hash AND monotonically increasing generation. Editing then restoring old plan never resurrects former consent; legacy granted scopes intentionally fail closed until human revoke/reapprove. Full CI [#38343484223](https://github.com/DigiLogicTech/OnePane/actions/runs/38343484223) PASSED.
- [x] **P0-Z paired Node TLS certificate checks:** pinned leaf fingerprint alone insufficient; direct TLS check now requires current validity, explicit server-auth EKU, digital-signature key use and single cert. Full CI [#38343815884](https://github.com/DigiLogicTech/OnePane/actions/runs/38343815884) PASSED. Initial Node enrollment expires_at is NOT the lifespan of an active pairing.
- [x] **P0-AA software-key countersigned instrument custody:** explicit Node Ed25519 countersignature binds original independently authenticated human challenge/plan, full private local one-shot report hash, enrolled instrument key, exact target/TLS pin/source/Goal/outcome and timestamp. Offline verifier requires independently supplied Node certificate and enrolled pin. Full CI [#38345684683](https://github.com/DigiLogicTech/OnePane/actions/runs/38345684683) PASSED.
- [x] **P0-AB manual sign/verify CLI:** existing Node identity only, 0600 private JSON, bounded input and independent pinned Node certificate/key. Test fixture JSON tag syntax failure remediated at 101d6f5; final full CI [#38346193719](https://github.com/DigiLogicTech/OnePane/actions/runs/38346193719) PASSED.
- [x] **P0-AC explicit Node-local one-shot execution:** `onepane-node-custody probe-and-witness` loads existing keys and independently supplied original approved v2 endpoint, reserves exclusive private report/witness files, spends the same crash-persistent per-instrument-key nonce as the old instrument CLI, performs at most one bounded TCP4 connect, signs outcome and Node custody. Replay, failed dial, wrong target/key, no Node and false hardware-vantage deny regression. Shared nonce extraction [#38348651351](https://github.com/DigiLogicTech/OnePane/actions/runs/38348651351), Node executable [#38348735103](https://github.com/DigiLogicTech/OnePane/actions/runs/38348735103), combined exact-head [#38348903908](https://github.com/DigiLogicTech/OnePane/actions/runs/38348903908) **PASSED**. No real network scan in CI.
- [ ] **NEXT P0**: Independently enrolled and physically proven Node/instrument identity and execution vantage, authenticated server-issued original challenge/plan delivery, human-reviewed same-target asset ownership/cross-source independence; calibrated incident confidence (not self-report promoted to independent votes); scoped time-bounded human-approved reversible Level 5 playbooks with postcondition verification and rollback. Software-key custody is NOT TPM/TEE attestation, trusted host identity, accurate external sensor, incident vote or active defence.
- [ ] **RELEASE gates**: Windows NTFS ACL and physical Node test, Windows/Ubuntu installed integration, local low-resource soak and bounded long-term evidence retention, complete security/code audit. User-directed human-focused UI/UX and customisation review remains **LAST** before explicit approval for QA installer/package. Draft PR #227 unmerged; Project/model data preserved.

Current source head (before this manifest update): f4a560fef3b82aa45edb9836c5a7b4913358984c, hosted CI run 38348903908 successful. PR discussion checkpoint #6138574967 contains this batch's execution/audit rationale.



## RC11 P0-AD–AF — human-scoped Node probe execution and conservative verification (2026-10-13)

- [x] **P0-AD separate execution authority:** migration 0086 and human-only Project API grant a single short-lived paired-Node execution permit for exactly one original issued instrument challenge. The scope binds active L5 Project, Goal, Workspace, human reviewer, live instrument/key, approved RFC1918 target, source/TLS pin, paired Node fingerprint and original challenge digest. Grant and revocation are independently audited, immutable and unique for the challenge lifetime; existing peer-mTLS telemetry grant cannot grant a network action. Source commits e25a114e, c88f759b; integration test cb1d30b8, API test 470d6f87. Full hosted CI [#38352320641](https://github.com/DigiLogicTech/OnePane/actions/runs/38352320641) **PASSED**.
- [x] **Operator control and effective state:** authorising API denies anonymous/Agent/foreign Workspace and strict duplicate fields. Inventory distinguishes historical approved status from active, revoked, expired, unpaired Node, missing reviewer, consumed challenge and inactive scope. Local execution verifies enrolled Node certificate date/client-purpose and matching Ed25519 private signer BEFORE socket; operator-origin permit metadata checked before nonce consumption. Commits 90cb2b31, 4b6b4dee, ed8ea6cb. [CI #38352619504](https://github.com/DigiLogicTech/OnePane/actions/runs/38352619504) **PASSED**.
- [x] **Three-key historical software-custody chain:** the human explicitly signs the OnePane-generated permit through a separate domain-separated Ed25519 operator key using the new sign-execution-permit CLI; the Node probe-and-witness command rejects unsigned/grant-mismatched copies and reused Node/instrument signer as human approval. The verify-execution CLI checks the original independent human-signed permit alongside Node and instrument signed original receipts/challenge/plan as of historical witness time. Neither output permits a new socket/automatic incident. Commits 6630e7a3, 4d8707af, be66e11e, 9d642e1c, b8e16d13. Hosted [CI #38352938558](https://github.com/DigiLogicTech/OnePane/actions/runs/38352938558) and [#38353306684](https://github.com/DigiLogicTech/OnePane/actions/runs/38353306684) **PASSED**.
- [x] **Witness privacy test robustness:** old test incorrectly searched for port digits within random SHA256 fingerprints and timestamps and flaked at CI #38353232195. Changed to assert no raw socket fields in exact JSON witness schema and no literal private IPv4, without changing the private cryptographic report check. Commit eee0ac42, full hosted [#38353449565](https://github.com/DigiLogicTech/OnePane/actions/runs/38353449565) **PASSED**.
- [x] **P0-AF Goal plan generation invalidation:** migration 0087 binds each Node execution grant to immutable approved monitoring-plan hash plus a strictly increasing plan generation. Editing/restoring the prior plan **never** revives the old permission or inventory's active state; legacy zero-generation grants fail closed. Original v2 challenge plan SHA256 and positive generation are checked by offline Node, and an operator cannot sign an already inactive scope. Commits 3dbd3750, 3b71f51f, 65356833, final exact-source [CI #38353877299](https://github.com/DigiLogicTech/OnePane/actions/runs/38353877299) **PASSED**; full Go/SQLite/source contracts, Windows/Ubuntu cross-platform compile, opt-in MSI log reader. Physical Node CI acceptance SKIPPED.
- [ ] **P0-AG next: trust origin and enrollment, NOT cryptographic overclaim:** separate human Ed25519 signer must be enrolled to an active OnePane Workspace principal through independently authenticated proof. A portable permit from API is NOT OnePane-server-signed and its signed copy alone does not prove it came from the current server; local offline Node cannot observe human revocation after issuance until short original challenge expiry. Require authenticated local-first grant handoff and live revocation checks before treating portable files as end-to-end server-verified network permissions.
- [ ] **P0-AH then: physical and incident proof:** attest real paired Node execution/vantage, independently verify actual asset/source relation, same-target/same-device correlation and independent adverse evidence. Never count software key signatures as distinct truthful physical sensors. Calibrate incident classification and safe reversible, time-bound HUMAN-approved L5 playbooks with postconditions and rollback; preserve L4 default and opt-in L5.
- [ ] **Before QA release:** bounded low-RAM disk/SQLite soak and retention, full source/security and code review; human-centred controllable/customisable UI/UX alignment must be LAST before separately authorised Windows/Ubuntu installer packaging and real installed physical Node acceptance. No merge/release; installed Projects/models preserved.

Progress/comment checkpoint in draft [PR #227](https://github.com/DigiLogicTech/OnePane/pull/227#issuecomment-6139269870). Source head 6535683373ce88075996f2d08792c5b78bd31c65 (prior to this manifest-only commit).

## RC11 P0-AG–AI — original server + human + paired Node + instrument execution custody (2026-10-13)

- [x] P0-AG authenticated human operator key: migration 0088 pins one immutable human Ed25519 operator fingerprint to the exact original one-shot approval. Explicit human API proof-of-possession binds Node, Goal, challenge, active human actor and optimistic revision. Missing/changed or instrument-equivalent operator key fails closed; existing empty-key grants stay inactive. Source CI #38354323222 PASSED.
- [x] P0-AH original OnePane server signing: existing local federation Ed25519 identity (never silently generated here) signs a CURRENT active human-authenticated permit via the human-only POST /node-probe-execution-permits/{permitID}/seal endpoint. Offline verifiers require the independently pinned original OnePane certificate DER, fingerprint and Node ID; the portable file's own signer claim is not trusted. Cross-source issuer/scope impersonation denied. Pinned server sign, source and API tests passed hosted CI #38355704881.
- [x] P0-AI no unsigned fallback: sign-execution-permit first independently authenticates the original OnePane server seal and current-at-signing effective state, then adds the separately approved human Ed25519 signature. Explicit Node probe-and-witness refuses to open its one private TCP4 socket unless exact OnePane server, human, paired Node and instrument signing keys match the original private approved challenge, TCP scope, target, source, TLS pin, public keys and bounded nonce. Offline verify-execution separately checks the original server seal plus human and Node/instrument historical signatures; all are software-key and approval-custody evidence, not physical attestation. CI #38355453914 and full #38355704881 PASSED. Real physical Node acceptance CI remains SKIPPED.
- [ ] P0-AJ trust/vantage: instrument/Node certificate possession does not establish hardware boot integrity, that actual physical Node executed the probe, actual owned device identity, independent observer vantage, or independent incident voting. Test same-target and distinct physical sensor evidence before calibrated human-reviewed incident assessments. Detached signed permits cannot communicate post-export revocation; server admission must independently recheck live authority; grant signing may race changes. Original challenge scope expires within two minutes, no automatic polling or retry.
- [ ] P0-AK reversible human-approved L5 actions: explicit bounded asset scope, dry-run plan, manual stop and audit, postcondition and rollback for every action; Level 4 remains default. No security remediation can be launched from a signed observation or heuristic triage alone.
- [ ] Release gates: low-resource endurance, bounded SQLite/retention, comprehensive security/source review, Windows NTFS ACL and physical paired Node installed testing. The user-directed human-focused UI/UX controls and customisation review must remain the LAST development workstream before separately approved Windows/Ubuntu QA packaging. PR #227 remains unmerged; installed Projects/models and settings preserved.


## RC11 P0-AJ — live upstream TCP consent and transactional issuer seal (2026-10-13)

- [x] **Dependency-chain denial:** original Node probe approval inventory now verifies the current exact human TCP4 permit and human-declared target; the original TCP grant must still match source, Project/Goal/Workspace, plan, revision, private IPv4, port, bounded timeout, effect class and BOTH current human reviewers. It also checks linked TLS pin, instrument and original issued challenge. Withdrawing original TCP consent returns tcp_scope_unavailable, even when the historical Node execution grant is still approved. It cannot receive a fresh original OnePane server seal.
- [x] **Accurate operator reasons:** human reviewer membership withdrawal reports approver_inactive ahead of downstream TCP-scope denial, while all states deny execution. A real SQLite integration test revokes original approved TCP consent and checks effective Node denial; existing plan edit/restore, Node unpairing and membership tests remain.
- [x] **Atomic issuer signing and audit:** the human-only OnePane original seal API now invokes Project Service within a SQLite write transaction, acquires writer lock before inspecting the SAME live effective-state projection used by human inventory, seals with the already enrolled OnePane Ed25519 issuer while holding the lock and commits a digest-only seal-audit event before releasing the signed bytes. No raw endpoint in the audit. Revoked grants do not invoke the signer or create seal audit entries; a competing revocation is serialised before or after a successful issuance. A previously exported offline permit can still outlive an immediate revocation until original short challenge expiry, which requires separate hardware/live-admission testing.
- [x] **CI exact-source success:** commits 5482bb4, 14390a7, 38fc714, 237296c, 5272d57, 5592017, d8aa0f0, 7409fe3 and 32dc229 passed exact-head hosted CI #38367484298 (Go/SQLite integrations, contracts, Windows/Ubuntu compilation, MSI log reader). Earlier intermediate CI failures (misleading denial reason and outdated nontransactional API mock) were corrected. Physical Node acceptance job SKIPPED.
- [ ] **Remaining P0:** independently establish owned target and physical Node/vantage provenance; same-target correlation and truly independent observation; calibrated human-reviewed incident classification, reversible human-approved and audited Level 5 playbooks with preconditions/postconditions/rollback. Do not promote software signature possession to device proof or automatically remediate from self-reported evidence.
- [ ] **Remaining QA/security:** test live offline-revocation boundary and Windows NTFS ACLs on installed physical Node; low-resource memory/disk/SQLite soak and retention; full source/security review, comprehensive human-oriented control/customisation UI/UX review LAST before separately authorised Windows/Ubuntu QA build. Preserve existing models, Projects and settings. Draft PR #227 unmerged.

Verified source head before this documentation checkpoint: 32dc229a1add595c6f62de6b42df13955cdb1c52.


## RC11 P0-AL — seal-export budget and hosted one-shot race/security gates (2026-10-13)

- [x] **Bound signed approval export writes:** transactionally limit human original OnePane server seal exports to at most three per immutable one-shot permit; lost handoff can be re-exported twice, but the fourth attempt must fail before signing or appending an event. Test proves exactly three successful audit records, denial of fourth, and no new signing/audit after underlying TCP revocation. Event counting is indexed by existing event aggregate index; no raw target IP/port is logged. Commits 2aa769b and b1f2885. Full hosted source CI #38368497015 PASSED.
- [x] **Hosted race and platform compilation coverage:** development-contract CI now executes Go -race regressions over instrument probes, federation Node custody and both manual probe CLIs; separately executes the actual one-shot human Node permit SQLite integration test with -race. Full regression also explicitly cross-compiles both new CLI executables for Windows amd64 without packaging installers. The first workflow-edit source #38368950019 failed YAML parsing due a broken inline test pattern and was restored without weakening the existing full Go stage. Final exact-source CI #38369112806 PASSED (including race stage, Go regression, Linux and Windows cross-compilation, Windows MSI log reader); physical Node acceptance CI job SKIPPED. Workflow source head before docs: 32174de95b7168b8dfb04c70e89735ee7c6880ac.
- [ ] **Remaining P0 physical proof:** real Node/device identity and trustworthy independent network vantage; same-target and genuinely independent source corroboration; calibrated human-reviewed incident decision. Software issuer/operator/Node/instrument signatures remain custodial data and do not prove physical authenticity, device ownership or sensor independence.
- [ ] **Remaining P0 reversible L5:** explicit per-asset human-approved action plans with preconditions, dry-run, manual stop, bounded effects, independently observed postconditions and rollback. Signed one-shot probes cannot grant host remediation. Level 4 remains default; Level 5 opt-in.
- [ ] **Before next QA installer:** bounded long-running retention/security and low-resource hardware soak; real physical Node mTLS/NTFS ACL acceptance; comprehensive end-to-end source review; human-focused UI/UX design control/customisation review LAST; installer/build only after explicit approval. Draft PR #227 remains unmerged. Preserve installed models, Projects, secrets and settings.


## RC11 P0-AM — original plan-generation fence for ALL signal classes (2026-10-13)

- [x] Paired-Node receipt triage now requires original approved plan SHA256 and monotonic generation, and an old Node self-report cannot count after plan edit/restore. Source commits 40bbeb4, 79d0239; CI #38389060520 PASSED.
- [x] Migration 0089 adds immutable original plan_generation to exact human-approved TCP4 permits. Historical grants default to generation zero and deliberately fail closed rather than silently reactivating; original audit/Project records are retained. TCP execution admission, target/TLS/instrument enrollment and all current evidence paths require the matching CURRENT Goal generation. Commits 7b1bdbf, 827af7d, 49e2516, 1088274.
- [x] Human TCP approval inventory now displays historical approved status separately from effective_scope_state; plan edited then restored yields goal_scope_inactive, never active. Target/TLS/instrument inventories propagate the original human grant invalidity; claimGoalTCPAttempt refuses a fresh socket. Commits b164e3f, 89d22cd, 527f993, 85a1255, 1b62012.
- [x] Read-only TLS check, instrument challenge and signed instrument self-report triage now fail closed on stale original TCP grant generation. Synthetic SQLite fixtures test relational evidence only, NOT physical sensor integrity. Tests fc0efcc, 34e5c1b, 675030d validate no current signal from an old generation; the immutable SQL trigger cannot be bypassed by updating the generation. Older intermediate CIs failed on outdated fixtures or an overstrict one-source assumption; both were corrected, with no production safety relaxation. Final exact-source CI #38391066762 PASSED (Go/SQLite, race tests, Windows/Ubuntu compilation, MSI reader). Physical Node test job SKIPPED.
- [ ] A human must revoke/reissue any stale original TCP, declared target, TLS/instrument and paired-Node scopes under a truly new plan generation if reauthorising network work; never infer automatic renewed approval from restored JSON.
- [ ] Next: independent physical Node and actual owned device target/vantage association, calibrated human-reviewed same-asset incident assessment, reversible human-approved L5 playbooks with postcondition and rollback, low-RAM/retention/security audit. Full human-centred UI/UX review remains LAST before authorised QA packaging. PR #227 stays draft; no release/merge.

Source head before this documentation checkpoint: 675030d8c44bab9ed60313e42b98a7d0c32907b7.


## RC11 P0-AN — current signal authority across plan rollback (2026-10-14)

- [x] TCP baseline consent, read-only reachability, final network dispatch, drift notices and Goal evidence coverage require the original positive current monitoring-plan generation. Restoring byte-identical JSON never regrants old TCP authority. Commits e4d539d, 2129893, ed5b512; exact-source hosted CIs #38464619001, #38464826395 and #38464979338 passed. Only synthetic/fake sockets; physical Node job skipped.
- [x] Migration 0090 adds immutable original generation to human-enrolled signed source grants. Existing historic signers start at generation zero and require renewed human enrollment; signed proof admission, credential minting, bearer preflight, credential inventory and current signature coverage deny an edited/restored plan without new permission. Goal pause suppresses current signed proof projection. Commits 4e06903, 2fb760d. CI #38465590757 and #38465803783 pending at this checkpoint.
- [ ] P0: independently verify physical paired Node execution/vantage and owned target identity, independent same-target evidence, calibrated human-reviewed incidents, bounded explicit reversible L5 response permissions with preconditions/postconditions and rollback. A cryptographic signature does not prove physical attestation.
- [ ] Before installer: low-RAM and long-term SQLite retention, post-export revocation, Windows NTFS ACL and physical-node installed testing, complete independent code/security audit, final human-centred customisable UI/UX review LAST, and explicit user approval for QA packaging. Draft PR #227 remains unmerged; preserve existing Projects, models, credentials and settings.


## RC11 P0-AO — origin HTTP Agent Runtime dialer isolation (2026-10-15)

- [x] Security review identified that merely clearing `http.Transport.Proxy` retains caller-provided `DialContext`, `DialTLSContext` and `TLSNextProto` hooks which can redirect an origin-only Assistant/Project request. The built-in origin HTTP bridge now uses a fresh direct transport, discarding caller hooks and retaining the earlier fail-closed opaque RoundTripper denial.
- [x] A new regression uses an in-process interception sink to assert the injected dialer is never invoked and the intended literal-loopback receiver alone gets the private request. The earlier proxy and opaque-transport tests remain in place. **Hosted CI to be checked against the exact committed head**; this checklist item describes implementation only until CI is green.
- [ ] Next: independently establish physical paired Node/owned-target identity and trustworthy observer vantage, then human-reviewed independently corroborated incidents and explicit bounded reversible Level 5 actions with preconditions, postconditions and rollback. Run low-resource soak, complete code/security audit, and final user-centred UI/UX review LAST before separately approved QA installers. No merge, packaging or claim that local transport entails local model compute.


## RC11 P0-AP — historical incident observation-time fence (2026-10-15)

- [x] Identified that signed instrument self-report correlation matched only server acceptance time, not its distinct signed source-reported observation time. A delayed two-minute challenge receipt could have appeared attached to a later incident window despite being observed before the window.
- [x] Bound BOTH `observed_at` and `accepted_at` to the original bounded historical ±15-minute drift window; preserve existing challenge issuance chronology and proof-class limitations. A new SQLite regression recreates an authentic signed delayed claim and a separate fake-socket alert whose receipt is inside the review window but observation is outside. Existing in-window positive claim remains in its original review.
- [ ] Check exact-head hosted CI before marking the fix verified. Next: physical Node and independently established owned-device identity/vantage, explicitly human-calibrated incident correlation and safe bounded reversible L5 playbook execution. Do not promote signatures, TCP sockets or timestamps to physical device truth. Security/endurance audit then comprehensive human-focused UI/UX review LAST before authorised QA installers. PR #227 stays draft.


## RC11 P0-AQ — finite external Agent Runtime invocation budget (2026-10-15)

- [x] Bound the built-in HTTP Agent Runtime invocation even when no explicit `timeout_ms` and a caller-supplied `http.Client` has no deadline. Configured positive timeout takes priority (already limited to ten minutes); otherwise preserve shorter client deadlines or clamp to ten minutes, so one opaque bridge call cannot occupy an older-hardware Worker without a finite cap. Preserve context cancellation and conservative unknown-outcome semantics after network dispatch.
- [x] Add direct timeout-policy unit cases for omitted, negative, excessive, shorter and explicit limits. Hosted CI **pending** at this source commit; do not claim verified until exact-head CI succeeds.
- [ ] Continue with honest Node/owned-target and vantage provenance, independently corroborated human incident triage, bounded reversible explicit Level 5 playbooks and offline-revocation proof. Then low-RAM endurance, full security audit, and final human-first UI/UX control/customisation review LAST before separately authorised QA installers. Preserve installed Projects/models and draft PR #227.


## RC11 P1-AR — target-scoped historical incident evidence retrieval (2026-10-15)

- [x] Add new additive SQLite migration 0095 with a per-Project, Goal, operator-declared target, source, and acceptance-time lookup index. Avoid broad goal-wide history scans for the human-only bounded signed measurement incident review without changing immutable receipts, retention or approval semantics. A real SQLite integration test asserts the migrated index exists.
- [ ] Hosted CI on exact-head source, upgrade-path/rollback performance, realistic SQLite retention and low-memory endurance remain to be measured. This is indexing only, **not** evidence of physical vantage, sensor independence or release readiness.
- [ ] Next blocking P0: trustworthy physical Node/owned-target validation, independent same-device corroboration, human-approved reversible bounded L5 action with postcondition and rollback; then comprehensive code/security review. User-centred UI/UX and customisation review remains LAST before separately approved Windows/Ubuntu QA installer. PR #227 remains draft/unmerged.


## RC11 P0-AS — display historical instrument observation versus receipt time (2026-10-15)

- [x] Include the original signed **source-asserted** `observed_at` alongside server `accepted_at` in human-only, bounded incident evidence projections. No device/vantage trust escalation, no raw endpoint or signer material, and no mutation or response grant. Real SQLite integration asserts exact claim timing and established non-attested proof status.
- [ ] Source-level CI verification pending for this exact head. Independently signed observation time is *not* hardware-attested or proven physically accurate.
- [ ] Continue physical Node/owned device/vantage acceptance and audited human-approved reversible L5 response development. Low-memory/security review before final comprehensive UI/UX customisation review LAST, then only explicitly authorised QA installer packaging; draft PR unmerged.


## RC11 P0-AT — operator stop and reviewer-revocation visibility in Level 5 response intent (2026-10-15)

- [x] Add effective human incident-reviewer membership and durable observer-pause-latch checks to the non-executable response candidate readiness view. These are only extra blockers: no execution approval, Node action, inferred independent evidence or Process/Network grant is created.
- [x] SQLite integration validates the independent operator pause/resume and reviewer suspension/reactivation transitions with the historical candidate held fail-closed. Original monitor permit and human review decisions remain immutable; no synthetic network dispatch.
- [ ] Exact-head hosted CI to confirm; physical Node/device validation, explicit user-scoped reversible L5 permits and verification, low-memory/retention and complete security audit remain. Comprehensive operator-customisable UI/UX review stays LAST before separately approved installers; draft PR remains unmerged.


## RC11 P1-AU — populated SQLite 0094-to-0095 migration regression (2026-10-15)

- [x] Extend actual signed-instrument/incident SQLite fixture to reconstruct pre-index schema, apply normal embedded migration 0095 on populated history, verify original Ed25519 proof bytes and case link remain unchanged, plus validate target index restoration. No changes to prior migration file checksums or historical records.
- [ ] Hosted exact-head CI still required; this tests staged SQLite upgrade, not installed Windows MSI/Ubuntu .deb replacement, Windows ACLs, large-dataset disk pressure, backup restore from failed upgrade or physical Node identity.
- [ ] Continue Node identity/vantage proof, independently corroborated incidents, bounded reversible human-approved Level 5 playbooks, low-resource endurance and full security audit. Human-centred UI/UX alignment LAST before separately approved QA packaging; branch stays draft.


## RC11 P1-AV — signed instrument evidence retention visibility (2026-10-15)

- [x] Add metadata-only signed-instrument-claim counts to the existing read-only, human-authorised, canonical Goal retention inventory. Count only linked original instrument keys in the same approved Project Workspace, never raw signatures/targets. At 1,024 signed claims per Goal report an advisory; automatic pruning remains disabled, byte measurement unclaimed.
- [x] SQLite integration checks nonzero receipt counts, foreign-Project denial and unchanged 2-receipt count after staged index migration. No monitoring, mutation, retention deletion or permission expansion; exact-head CI verification pending.
- [ ] Continue physical Node and owned-target acceptance, independently corroborated incidents, bounded reversible human-approved L5 actions, measured low-memory/SQLite WAL endurance and source/security audit. Comprehensive human-first UI/UX and customisation review LAST before approved QA installers. Draft PR #227 unmerged.


## RC11 P0-AW — fail-closed malformed origin HTTP adapter policy (2026-10-15)

- [x] The final built-in HTTP Agent Runtime transport no longer ignores an explicit malformed data policy and accidentally drops `destination_kind=origin_node` into an unrestricted remote HTTP path. Deny malformed/non-object/wrong-typed JSON prior to any dial and mark the denial outcome known. The canonical Service remains responsible for authorised complete policy and budget admission; this adapter is independent final defence, not an access grant.
- [x] Direct adapter regression uses a local interception sink to verify zero requests under three malformed policy payloads. Exact-source CI pending.
- [ ] Continue physical Node/device identity and independent-vantage proof, human-correlated incidents, reversible bounded Level 5 effects, low-resource soak and full security audit. Comprehensive human-first UI/UX review remains LAST before approved QA installer; branch stays draft/unmerged.


## RC11 P0-AX — reject explicit ambiguous Agent Runtime destinations (2026-10-15)

- [x] Extend last-boundary malformed-policy denial to explicitly present `null`, empty objects and unknown `destination_kind` values. A partial explicit policy cannot silently acquire an arbitrary HTTP destination; canonical Service still determines authorised capability, spend and tenant context. Preserve legacy missing-policy adapter-unit compatibility, never treat it as a grant.
- [x] Add local interception no-dial regression cases for each ambiguous policy shape, building on P0-AW. Exact-head hosted CI pending.
- [ ] Hardware/vantage proof, calibrated independent incident correlation, reversible bounded operator-approved L5 action permits, low-resource QA and complete security/code audit remain; comprehensive human-first UI/UX pass LAST before separately approved installers. Draft PR unmerged.


## RC11 P0-AY — bounded operator visibility of signer disagreements (2026-10-15)

- [x] Add bounded, human-only `signed_claim_consistency` and `distinct_signing_keys` metadata on historical incident review evidence. Multiple genuine signatures or even different software keys NEVER become independent-vantage proof, incident votes, target-device ownership or remediation authority. Contradictory outcomes remain descriptively visible, not automatically adjudicated.
- [x] Add unit regression matrix for repeated same-key claims, same-key contradiction, multiple un-attested signing keys and absent evidence. Read path remains capped at eight claims with zero new network operations or data migrations.
- [ ] Hosted exact-head Go/SQLite/Windows CI pending; actual separate physical observer custody/verified device identity and safe L5 reversible execution still require engineering and physical acceptance. Low-resource endurance/security audit then human-first UI/UX alignment LAST before approved QA installer.


## RC11 P0-AZ — atomic stored incident-evidence read snapshot (2026-10-15)

- [x] Resolve a human-only incident review's Project/Workspace status, alert, historical TCP return-to-baseline and bounded signed claim list within one SQLite read transaction. Previously separate SQL statements could produce mixed-time records across concurrent case changes. Release snapshot on all returns including no-declaration and query errors; final success commits a read-only transaction.
- [x] Existing real SQLite case regression covers first no-declaration read followed by independent signed-key/incident writes, successful historical reads, and source preservation across migration. Exact-head hosted CI pending.
- [ ] This does NOT establish physical Node target identity/vantage, multi-sensor independence or Level 5 remediation safety. Continue user-scoped reversible permitted actions, physical tests, measured low-resource endurance/security audit and LAST human-first UI/UX review before approved installers.

## RC11 P0-BA — admitted contradictory same-key claims (2026-10-15)

- [x] Add a real SQLite, live human-admission, single-use challenge and Ed25519-signature regression proving opposite outcomes from one instrument signer are metadata only, not independent physical sensors, incident votes or L5 action authority.
- [x] Check delayed third report remains outside a later incident window; retained history and staged migration now assert all 3 signed receipts, while original case association includes only two.
- [ ] Exact-source hosted CI must pass. No claim of physically independent vantage, verified owned-device identity or safe autonomous remediation; those plus low-memory soak, source/security audit and final LAST user-focused UI/UX review still block authorised QA packaging.

## RC11 P0-BB — report bounded incident-evidence truncation (2026-10-15)

- [x] Query up to nine metadata-only signed-instrument receipts, publish at most eight, and set `signed_claims_truncated` if a ninth exists. Human operators can now recognise when reported consistency and key counts are only a limited snapshot, not all historical self-report outcomes.
- [x] Unit test checks fixed limit and positive truncation without inventing unique sensor votes, full-history confidence, physical target identity or remediation consent.
- [ ] Exact-head hosted CI pending. Independently trusted physical Node/device-vantage proof, explicitly scoped reversible Level 5 actions, low-memory/SQLite endurance, full code/security review and LAST human-centred UI/UX remain outstanding before an authorised QA build.

## RC11 P0-BC — SQLite-proven historical signature window saturation (2026-10-15)

- [x] Extend realistic signed-source admission fixture with seven further separate signed challenges over bounded fake-clock intervals, yielding nine accepted same-key reports in the original ±15-minute incident window. Newer eight are consistent; oldest conflicts. A later tenth report is excluded from the original window after delayed receipt.
- [x] Enforce an eight-item returned list and truthful `signed_claims_truncated=true` with a single software key, no physical-vantage or remediation flags, and all 10 persisted signed receipts surviving the real additive schema index migration.
- [ ] Exact-head hosted CI must pass; physical multi-vantage/target ownership, real reversible independently permitted Level 5 response, low-end long-duration WAL/RSS disk tests, security review and final LAST human-first UI/UX still block QA packaging. Draft PR unmerged.

## RC11 P0-BD — pinned Node custody must be an end-entity leaf (2026-10-15)

- [x] Forbid CA and certificate-signing key-usage bits in separately pinned Node instrument custody identities. A certificate that can issue further certificates must never be accepted as the particular human-enrolled Node leaf.
- [x] Add negative certificate-role regressions alongside existing real Ed25519 Node signer/self-test and independent pin/expiry tests. Exact-head hosted CI pending.
- [ ] This does not physically attest the Node or target, establish an independent sensor vantage, or grant L5 remediation. Continue physical acceptance, bounded reversible human-approved effects, measured low-RAM endurance, code/security audit and final LAST user-focused UI/UX before approved QA installers.

## RC11 P0-BE — Node private identity preservation and safe creation (2026-10-15)

- [x] Fail closed on partial existing `federation.crt`/`federation.key` pairs rather than silently generate and truncate surviving Node credentials. Make new file creation exclusive and durable; never replace a concurrently created key. Reject existing linked/nonregular files and Unix group/world-accessible Node identity directory or private key.
- [x] Add regression for both partial-pair cases preserving surviving credential bytes, and for linked/world-readable key denial without modifying an external file; keep original stable same-Node fingerprint and Node-ID mismatch checks. Exact-source CI pending.
- [ ] Windows NTFS ACL/security descriptor physical installed QA, backfill or interactive restoration of incomplete installations, real Node/owned target/vantage proof, reversible L5 actions and low-resource endurance are NOT delivered. Source/security review then final LAST UI/UX alignment before explicitly approved installer.

## RC11 P0-BF — reject unusable Node identity before key creation (2026-10-15)

- [x] Align Node credential creation with existing custody ID bounds (1–128 bytes, no surrounding whitespace/control characters) BEFORE making key directories or generating a long-lived Ed25519 certificate.
- [x] Add no-disk-side-effect regression for empty, newline/NUL, padded and overlong Node IDs. Actual physical device attestation and Windows installed ACL tests remain outstanding. Exact-head CI pending.
- [ ] Continue separate owned-target proof, independently attested vantage, reversible narrowly approved Level 5 effects, low-memory/security review and final human-focused UI/UX LAST before approved QA build.


## RC11 P0-BG — read stable bounded Node identities without second pathname race (2026-10-15)

- [x] Replace unchecked subsequent `os.ReadFile` and `tls.LoadX509KeyPair` filesystem opens with a bounded descriptor-based `Lstat` + `SameFile` and `tls.X509KeyPair` over exactly read bytes. Check regular file, 16KiB cap, post-open Unix key permissions and reject linked/inconsistent certificates. No key rotation or automatic repair.
- [x] Add valid-identity fingerprint preservation and oversized private key/certificate/linked certificate regression. Source hosted CI pending.
- [ ] This is not equivalent to Windows NTFS ACL or simultaneous malicious same-account file mutation protection. Physical Node/owned device proof, independent vantage, safe human-approved Level 5 actions, measured resource endurance and security review remain; operator-first UI/UX is LAST before separately authorised QA build.


## RC11 P0-BH — consistent one-snapshot operator response-readiness diagnostics (2026-10-15)

- [x] Read the entire non-executable L5 response readiness from one transaction and one clock sample, including candidate, Project mode, incident review, original approved permit, human proposer/reviewer membership and durable observer pause. Prior read path sampled multiple changing database states. No Task, action or new grant.
- [x] Existing real SQLite integration already covers proposed, withdrawn, revoked permit, human membership suspension/restoration and pause/resume, while asserting no Tasks and permanent execution denial. Exact-head hosted CI pending.
- [ ] Next: independent owned device/vantage proof, truly bounded separately human-permitted reversible protective side effects, installed Windows/Ubuntu QA and low-memory endurance; full audit, then LAST human-centred UI/UX before explicitly approved installer.


## RC11 P1-BI — distinguish failed local Colibri runtime from deliberate stop (2026-10-15)

- [x] The Colibri hot-swap status endpoint now reads the selected model's exact persisted runtime status, including failed/stopped values outside the active-resident list. Previously a failed start appeared as a clean stop, making constrained-hardware recovery harder to diagnose. No automatic restart, fallback, model eviction or resource permission is added.
- [x] Fail cleanly for nil/unavailable runtime supervisor; regression verifies it cannot panic. Existing Colibri pin/busy/hotswap regression remains in hosted CI. Exact-head hosted verification pending.
- [ ] Real installation, GPU process teardown, low-RAM throughput/performance and crash recovery acceptance remain distinct; physical Node evidence, approved L5 defensive actions, code/security audit, then LAST comprehensive user-focused UI/UX before QA packaging.


## RC11 P1-BJ — SQLite verified Colibri failed/stopped status isolation (2026-10-15)

- [x] Extend the Colibri status regression beyond nil-supervisor checks: real SQLite registered deployments on two Nodes expose an actual failed target, healthy same-Node neighbour, stopped recovery and isolation of the other Node's residency list. Never leak launcher error text or automatically re-run the model.
- [ ] Exact-head hosted CI pending; physical Windows/Ubuntu model teardown and memory/VRAM accounting remain separate acceptance. Continued human-controlled autonomy, safety audit, and LAST UI/UX customisation remain release gates.


## RC11 P0-BK — Colibri status build failure immediately remediated (2026-10-15)

- [x] Hosted #38690800829 exposed Go compile failure `undefined: err` in Colibri status's new out-of-block SQLite read; the earlier `if err:=` for node ownership had block scope. Declare the new query's `err` locally before subsequent reuse, retaining the same Node-scoped persisted status lookup and no mutation.
- [ ] Confirm complete hosted Linux/Windows Go/SQLite and actual same-Node status isolation regression on this exact corrected source commit. A failed predecessor CI is not evidence of release readiness; PR stays draft and unmerged.


## RC11 P1-BL — mixed-case native Colibri pin eligibility parity (2026-10-15)

- [x] Align native `SetColibriPin` update with case-insensitive `ColibriPin` read admission, allowing legacy registered mixed-case Colibri names/backends without weakening the one-pin-per-Node guard or allowing non-native llama.cpp/GGUF models.
- [x] Extend SQLite migration/index integration fixture with mixed-case native deployment and successful durable pin on independent Node. Exact-head hosted verification pending.
- [ ] Physical GPU model tear-down/VRAM admission, hardware acceptance, code audit and LAST human-first UI/UX still required before QA installer.


## RC11 P0-BM — duplicate JSON egress policy keys denied before Agent transport (2026-10-15)

- [x] Deny explicit Agent Protocol HTTP data policies with duplicate top-level properties before performing a request, including origin-only/remote destination overrides in either direction and duplicate authorisation flags. Preserve valid key order, whitespace, canonical Service normalization and host-side Origin isolation.
- [x] Add a no-egress loopback interception regression covering ambiguous policies and a valid-policy unit matrix. Exact-head hosted CI pending.
- [ ] Physical L5 Node trust/owned endpoint acceptance, audited reversible human-approved effects, low-resource endurance and security review remain; operator-first comprehensive UI/UX LAST before approved QA installers.


## RC11 P0-BN — deny proxy/host/framing credential header destinations (2026-10-15)

- [x] Explicit Agent Runtime auth-header config remains customisable for normal HTTP token field names while rejecting proxy authentication, host routing, connection and framing headers or invalid field names. Reject CR/LF/NUL in auth prefix. Denial occurs before resolving a secret or any HTTP effect.
- [x] No-egress interception regressions for reserved headers and invalid names, plus explicit allowlist regression of standard/custom API-token names. Exact-head hosted CI pending.
- [ ] Continue owned Node and independent target/observer proof, bounded operator-granted reversible L5 effects, low-resource endurance and security/code audit. Final comprehensive human-first UI/UX LAST before authorised QA packaging.


## RC11 P0-BO — case-insensitive Agent policy duplicate override denial (2026-10-15)

- [x] Align duplicate-field detection with Go's case-insensitive JSON struct tag matching, rejecting duplicate destination/authority keys even when names differ in capitalization. Bound explicit raw policy body to 16KiB and at most 32 top-level attributes to avoid avoidable parser work on a tiny host.
- [x] Extend before-egress interception with mixed-case endpoint policy conflicts both orders; accept unambiguous alternate-case names. Exact-head CI pending.
- [ ] This prevents ambiguously sourced data-egress config; it is not physical Node attestation or an autonomous network protection grant. Low-RAM, Windows installed ACL/device acceptance and complete audit remain, then LAST operator-centred UI/UX before approved QA packaging.


## RC11 P0-BP — plaintext loopback bearer cannot be redirected by caller proxy (2026-10-15)

- [x] Extend direct no-proxy/no-injected-dial Agent HTTP transport to literal loopback **plaintext-authenticated** requests, not only self-declared `origin_node` destinations. The bearer over HTTP exception must not be accidentally converted into proxy-credential leakage by caller transport hooks.
- [x] Real `httptest` origin+proxy regression checks exact local Authorization value, one origin request and zero proxy requests under injected proxy. Exact-head hosted CI pending.
- [ ] Remote HTTPS providers retain separate configured credential/policy admission. Physical Node/owned target and independent observer truth, controlled reversible L5 effects, long-running resource and security audit still block packaging; final comprehensive user-first UI/UX LAST.


## RC11 P0-BQ — bounded, cancellation-aware read-only physical Node preflight (2026-10-15)

- [x] Protect the rootless Podman capability check from unbounded stdout by keeping at most 64 status bytes, never exposing host/path diagnostics. Reject any partial oversized output as a failed prerequisite, not success; run only read-only Podman info/image-exists.
- [x] Fail closed on already-cancelled checks or deadline-cancelled Podman/image inspectors even if a buggy inspector returns success after cancellation. Local fake inspectors exercise cancellation before entry and during both successful-looking checks; pure fake output writer verifies fixed memory bound.
- [ ] Hosted exact-source CI pending. This only validates operator prerequisites: no container run/pull, physical device or vantage identity, hardware acceptance or remediation authority. Continue protected physical test gates, independent proofs, reversible authorised L5 effects, installed low-resource/Windows ACL acceptance and source audit; LAST human-centred UI/UX before packaging.


## RC11 P0-BR — surface unreconciled/in-flight operator observer effects in response intent (2026-10-15)

- [x] Extend the non-executable L5 response readiness snapshot with exact Goal, Project Workspace, Task and Tool-scoped persisted observer operation uncertainty. Label `observer_mutation_needs_reconciliation` for unknown/blocked/compensation-failed outcomes and `observer_mutation_in_flight` for executing/observing/verified/compensating effects; never infer success or silently grant response authority.
- [x] Real SQLite integration inserts matching scoped historical operator operations (one unknown, one executing) and verifies both human-facing blockers remain alongside permanent execution denial. No adapter call, host/network mutation or Task creation by the readiness endpoint itself. Exact-source CI pending.
- [ ] Actual independently verified physical Node and target, separately authorised reversible effect admission, postconditions/rollback, measured low-resource installed QA and full code/security review remain. Comprehensive operator-first UI/UX LAST before approved installers; branch stays unmerged.


## RC11 P1-BS — refuse partial multi-Colibri eviction on one Node (2026-10-15)

- [x] Reject abnormal states with more than one healthy idle Colibri candidate BEFORE stopping any model. OnePane's single-resident policy means sequentially draining multiple local engines can partially evict working models during a failed swap; require explicit operator reconciliation. Preserve pinned/busy/starting/draining/orphaned denial and regular empty/single-idle behavior.
- [x] Unit regression tests one safe idle model and two abnormal healthy residents with empty eviction result; existing status and pinning regression remain. Exact-head hosted CI pending.
- [ ] Physical low-RAM host/process-tree teardown, VRAM release and installed recovery tests remain, alongside verified Node/target and reversible L5 authority. Full code/security review and LAST human-first customisation pass precede approved QA build.


## RC11 P1-BT — preflight Colibri Python before any working-model eviction (2026-10-15)

- [x] An already alive, healthy/busy or starting selected target returns/waits before attempting any automatic Colibri replacement. For actual switches check the required Python launcher path before evicting an idle predecessor; reuse preflight launcher instead of discovering it again later. No automatic fallback to llama.cpp, remote or another backend.
- [x] Add startup-order source contract to prevent future regression that would interrupt a working local model when Python is missing. Exact-source CI pending.
- [ ] Imported Python module health, executable preservation under concurrent uninstall, subprocess/GPU teardown, VRAM accounting and hardware acceptance require installed tests; physical identity, human-gated reversible L5 effects and last UI/UX gate remain.


## RC11 P0-BU — reject foreign Project observer-operation contamination (2026-10-15)

- [x] Extend actual SQLite response-intent readiness regression with a second active Project/Workspace in the same tenant. A fabricated foreign Operation references the **original** Goal's observer URI but is attached to the separate Project Task; non-executable diagnostics must never treat it as a target Goal's reconciliation debt. Later same-Goal unknown and executing operations must still surface both real blockers.
- [ ] Exact-head hosted CI pending; this is sandbox isolation for operator evidence, not physical sensor truth or network remediation permission. Continue owned hardware/observer proof, approved reversible L5 actions, measured low-memory acceptance and final source audit with LAST human-centered UI/UX before authorised installer.


## RC11 P1-BV — scope Colibri startup-order regression to its own function (2026-10-15)

- [x] Hosted run #38702275530 showed a false-positive source contract failure: test scanned all functions following `startLocked`, finding a legitimate unrelated Python lookup far beyond the launch implementation. Restrict source-order inspection to the `startLocked` body and keep the real order invariant (target already active → Python preflight → healthy predecessor eviction).
- [ ] Exact-head hosted CI rerun required; no fallback, model switching or process mutation added by this test-only correction. Draft branch unmerged.


## RC11 P0-BW — revalidate managed runtime PID before serving reused inference (2026-10-15)

- [x] For existing healthy/busy/starting local runtime instances, verify the alive PID's executable/model/port identity before treating a stale SQLite row as an approved inference engine. A recycled unrelated PID must not be contacted, signalled or silently substituted. Fail closed if the process is uninspectable or no longer matches exact managed identity; no new fallback or restart.
- [x] Add startLocked source-order regression ensuring identity verification precedes reuse and starting-instance health requests. Existing termination identity test remains. Exact-head CI pending.
- [ ] Process identity is not hardware attestation. Windows process-wrapper/child identity and RAM/VRAM real-host acceptance, physical Node/owned-target and independent observation proofs, reversible human-approved L5 actions and last UI/UX review remain release gates.


## RC11 P0-BX — prevent raw local inference logs leaking via startup errors (2026-10-15)

- [x] Replace appended raw process-log tails in failed/local managed runtime health/start errors with a generic local-log-available hint. Preserve original owner-accessible disk logs for future QA; no bearer tokens, model paths, user prompts or process arguments are copied to common UI/API error strings.
- [x] Restrict runtime log lookup to short generated-style IDs and non-symlink regular local log files; test injected bearer/path canaries, hostile path IDs, missing logs and linked files. Exact-source hosted CI pending.
- [ ] This is not a comprehensive QA debug feature or proof that all other diagnostic channels redact sensitive content. Complete source/security audit, physical Node/owned target acceptance, bounded reversible L5 actions and low-RAM installed tests remain. Human-first UI/UX customisation LAST before approved installer.


## RC11 P0-BY — preserve local logs and deny linked/unsafe startup log paths (2026-10-15)

- [x] Managed engine launch no longer opens an existing diagnostic log with `O_TRUNC` or follows a symlink in the final log path. Owner-private log directories and existing regular log files are validated, an existing file is appended after an opened-inode identity check, and absent files use exclusive creation. Existing size-limited rollover behavior is retained.
- [x] Unit regression verifies original diagnostics survive a subsequent launch, an external symlink destination cannot be written, and Unix shared-readable logs are denied. Exact-head hosted CI pending.
- [ ] Audit still needs portable Windows reparse-point/ACL acceptance, physical subprocess and GPU lifetime validation, low-resource filesystem pressure and full security review, followed by LAST user-first UI/UX before authorised installers.


## RC11 P0-BZ — stop leaking OnePane service secrets into local inference subprocesses (2026-10-15)

- [x] Replace full `os.Environ()` inheritance on managed Colibri/llama.cpp process launch with an explicit minimal OS/locale/GPU driver whitelist, then add only required OnePane-owned `HOME`, `ONEPANE_MANAGED_RUNTIME`, managed `LD_LIBRARY_PATH` and Colibri runtime settings. Third-party models no longer implicitly receive cloud API keys, provider tokens, proxy passwords, `LD_PRELOAD` or arbitrary `PYTHONPATH` from the privileged parent service.
- [x] Unit regression uses distinct secret canaries alongside essential PATH/locale/CUDA/Vulkan/AMD environment variables and verifies only allowed names survive. Exact-source CI pending. Installed CPU/Vulkan/CUDA/Windows Python compatibility must still be physically verified, with new nonsecret runtime driver vars added only via reviewed allowlist changes if necessary.
- [ ] This does not scrub secrets already present in application-managed model files, child-generated logs, explicit provider requests or Windows ACLs; complete security audit remains. Owned hardware/observer physical validation and operator-approved reversible Level 5 execution remain release blockers, with comprehensive human-first UI/UX LAST before approved QA installer.


## RC11 P0-CA — direct-only, evidence-based managed local inference health (2026-10-16)

- [x] Managed model `/health` requests use a new direct `127.0.0.1` HTTP client with no caller proxy/dial hooks, blocked redirects, 3-second budget, valid port constraint and at most 1MiB of response data. Private health bytes cannot be misrouted by a caller-configured proxy.
- [x] Empty, malformed, oversized or explicitly not-ready HTTP 200 responses no longer count as a healthy managed model or get relayed raw into error text. Recognised runtime readiness statuses `ok`, `ready`, `healthy`, and llama.cpp `no slot available` remain accepted. Hostile-content regression on actual loopback httptest pending hosted CI.
- [ ] This protects software process readiness, NOT physical owned-device/observer identity or independently trusted network incident evidence. Reversible L5 permissions and effects, installed memory/WAL/GPU/Windows ACL acceptance, full security audit, and LAST comprehensive UI/UX before approved QA installer remain.


## RC11 P0-CB — atomic model-request lease accounting on database failures (2026-10-16)

- [x] Local inference `SetBusy` now advances or releases the in-memory active-request lease **only after** the matching SQLite runtime state transition succeeds and affects exactly one deployed runtime. A failed or cancelled Acquire cannot create a ghost busy lease; a failed Release cannot drop its in-memory protection before the database acknowledges the change.
- [x] Zero affected rows (stopped/starting/absent process) fail closed on lease acquisition; releasing an already-absent lease is a read-only no-op. A live request remains guarded against premature Colibri eviction on failed release. Exact SQLite regression and hosted CI pending.
- [ ] This does not physically measure GPU RAM or solve crashes in the interval between any OS inference call and database persistence. Physical acceptance, genuine independently trusted owned-target evidence, authorised reversible Level 5 effects, full audit and FINAL user-centred UI/UX remain gating.


## RC11 P0-CC — bounded inference lease release survives cancelled task context (2026-10-16)

- [x] Give managed runtime `Release` a dedicated three-second cleanup context carrying the original request's values but not its cancellation. Otherwise cancelled chat/AgentCheck tasks can leave an idle model permanently marked busy even when SQLite is healthy.
- [x] Real SQLite regression confirms cancellation still releases a completed request, while a later SQLite write failure leaves the lease conservatively protected against eviction. Source/hosted CI pending.
- [ ] This does not bypass runtime stop/revocation, allow retries after failed release, or falsely mark a physical process healthy; explicit operator recovery is still required on storage failure. Full physical/low-resource/security validation and LAST UI/UX remain gates.


## RC11 P0-CD — canonical explicit observer-mutation scope and revisions (2026-10-16)

- [x] The human-gated observer pause/resume effect adapter now rejects duplicate, alternate-casing, unknown or omitted scope and optimistic revision fields, and caps input JSON to 4KiB before reading its trusted executing Operation. A missing `expected_fence_revision` may no longer silently mean zero, and later duplicate values cannot override original human scope.
- [x] Unit tests exercise valid exact Project/Goal scope, explicit zero fence revision, duplicate IDs, case aliases, duplicate revisions, unknown fields, missing fields, multi-object payload and oversized input. Exact-source hosted CI pending.
- [ ] Existing observer adapter only changes local scheduled TCP dispatch under approved human authority; it does not remotely repair devices, prove owned-target identity, supply independent physical postconditions or grant unattended L5 effects. All still block final build until rigorous validation and LAST user-first UI/UX.


## RC11 P0-CE — refuse physically impossible memory admission before evicting healthy models (2026-10-16)

- [x] Before the local runtime supervisor stops **any** idle healthy model to admit a different GPU placement, compute whether all eligible idle models' *estimated* device allocations could free enough capacity. Busy/non-evictable models remain in the projected usage; impossible admission now fails without disrupting existing working models. Explicit experimental overrides still require every selected accelerator to be free of other model claims after prospective idle recovery.
- [x] Pure regression matrix covers fully busy devices, adequate idle headroom, insufficient idle+busy mixed usage, multi-device placement and experimental override isolation. Existing placement, Colibri pin and active-request enforcement remain in hosted CI; exact-source check pending.
- [ ] These are conservative declared allocations, not actual GPU memory pressure, so real VRAM/RAM telemetry, Windows/Ubuntu startup and sustained low-memory acceptance remain release gates. Full security audit and LAST UI/UX alignment remain before authorised QA installer.


## RC11 P0-CF — concurrent workers cannot prematurely evict shared local model (2026-10-16)

- [x] Add real SQLite regression with 24 simultaneous local-inference workers sharing one runtime. All acquire an active lease before any release, and the runtime must remain `busy` while any lease is outstanding; after all completions it returns to `healthy` with zero ghost leases. This is a deterministic low-memory software concurrency check and runs with hosted race detection.
- [ ] Physical CPU/GPU scheduling and process teardown, long-run real-host performance and external Node proof remain distinct; exact-source hosted CI pending. Complete audit then LAST human-focused UI/UX before an approved build.


## RC11 P0-CG — only persist redacted local engine readiness metadata (2026-10-16)

- [x] Existing direct-loopback health admission validated a safe status but still returned all arbitrary JSON fields from a third-party model runtime; `waitHealthy` stored those fields in durable `health_json`. Drop all unknown content and retain only canonical lower-cased status and bounded nonnegative integer idle/processing slot counts (0–4096). No raw credentials, private paths or oversized diagnostics from an engine response should become general UI/API state.
- [x] Extend actual loopback HTTP tests with private token/path canaries and implausible slot counts. Source hosted CI pending.
- [ ] This is software readiness only. Physical Node ownership/independent vantage, approved reversible protective effects, installed low-RAM/WAL/Windows ACL/GPU acceptance, full security audit and LAST user-centred UI/UX remain before authorised installer.


## RC11 P0-CH — process provenance before startup health socket probe (2026-10-16)

- [x] `waitHealthy` must validate the managed PID/executable, model and expected loopback-port launch arguments before each startup `/health` HTTP probe. A coincidental or hostile local listener reporting `status=ok` cannot be accepted merely because a stale PID is alive; failure returns a generic error without signalling/replacing the unverified process or spilling command-line/private-path data.
- [x] Source-order safety test covers independent process-identity check BEFORE the first health socket request. Exact-source hosted CI pending. This is not proof that a separate local process cannot race for the socket or independent physical device identity; physical hardware acceptance stays blocked.


## RC11 P0-CI — bind exact observer effect input to approved Tool invocation hash (2026-10-16)

- [x] The final human-governed observer pause/resume adapter independently recomputes the Tool Gateway's canonical four-field JSON SHA-256 and requires equality with the persisted `tool_invocations.input_hash` for its RUNNING invocation and EXECUTING operation. Possession of an otherwise valid invocation ID, tenant, Tool and Goal URI cannot authorise mutation with a different Goal/fence revision or other effect arguments.
- [x] Real SQLite integration uses genuine canonical input fingerprints for pre-seeded test invocations, denies a changed optimistic revision without mutating the fence, and preserves the approved original pause/resume operation. Separate unit verifies canonical hash byte parity with Gateway. Exact-head hosted CI pending.
- [ ] This does not supply physical owned-target identity, independent sensor vantage or authority to execute automatic device remediation; operator-only local observer fence is still the entire implemented effect. Physical/installed hardware, resource budgets, code/security audit and LAST UI/UX remain gates.


## RC11 P0-CJ — deny CPU-only model footprint greater than safe physical RAM bound (2026-10-16)

- [x] Existing CPU-only admission returned success without checking memory at all. Read latest Node memory profile along with GPU data and reject a CPU model whose plan's estimated bytes exceed **total physical RAM minus an OS/OnePane reserve** (10% of RAM, capped at 1 GiB). Reject invalid/nonpositive footprint/profile rather than guessing. This is a no-effect admission rule; it doesn't evict other models, switch routes, call cloud inference or enable unsafe RAM overcommit.
- [x] Unit tests for 512 MiB, 8 GiB, 16 GiB and missing profile/estimated memory bounds. Hosted exact-head CI pending. Cached total RAM is not current *available* RAM; concurrent workloads, process RSS and paging must still be verified on real constrained hardware.
- [ ] Preserve model/data storage and hardware-level verification gates; owned physical device, separate observer evidence, human-approved reversible L5 protective effects, source audit and LAST human-centred UI/UX still block packaging.


## RC11 P0-CK — unify daemon recovery with backend-aware process provenance (2026-10-16)

- [x] `RuntimeSupervisor.Recover` previously independently compared old process arguments to `-m`, valid for llama.cpp but not Colibri's Python `--model` launch. Replace the divergent recovery check with the existing registered-backend-aware `VerifyIdentity` used by reuse and stop. Validate the same instance PID and revision before any localhost health probe; never contact or terminate unverified/recycled processes. Failed proof remains persistently `orphaned`, not recovered.
- [x] Regression source contract asserts the recovery method has no hard-coded `-m` admission and uses `VerifyIdentity` before `Health`, protecting Colibri, llama.cpp and future exact registered backends. Exact-head hosted CI pending. Real Windows process-tree and Linux daemon restart recovery still require installed acceptance.
- [ ] No owner/physical Node identity or independently trusted network endpoint proof is inferred from OS process checks. L5 independently corroborated incidents, operator-approved reversible protective effects, low-memory installed QA, security audit and LAST human-first UI/UX remain release gates.


## RC11 P0-CL — backend-aware executable/argument identity behavioural regression (2026-10-16)

- [x] Extract a pure `managedProcessCLIIdentified` validator from the existing authoritative `VerifyIdentity` boundary, shared by startup, local inference reuse, process Stop and daemon recovery. Test the actual Colibri Python launcher with both legacy server and tiered `coli` entrypoint and llama.cpp native `-m` execution, requiring exact executable, model, loopback host and port; reject wrong model, alternate host and PID/port confusion.
- [ ] Hosted exact-source CI pending. Process command-line matching is not proof of socket ownership under a hostile local user, physical device identity or independent network vantage. Real installed Node and Windows process-tree recovery tests still required, as are human-approved L5 effects, security review, and LAST UI/UX before authorised packaging.


## RC11 P0-CM — deny duplicate or aliased managed process option overrides (2026-10-16)

- [x] Exact PID/executable/model/loopback verification previously used `argsContainPair`, allowing an approved `--model`/`-m`, `--host` or `--port` pair followed by a conflicting duplicate that the actual runtime might parse with last-wins precedence. The backend-aware identity predicate now requires **exactly one** expected model, host and port option and rejects an alternate model-flag alias within the same command line. No native process is signalled or repaired for a mismatch.
- [x] Behavioural tests include same-value duplicates, later conflicting model, host/port and alias overrides for native llama.cpp and Colibri. Exact-head hosted CI pending; this is process CLI provenance, not physical owned-device or independent network-vantage attestation.


## RC11 P0-CN — close bounded restart-recovery SQLite cursor before effects (2026-10-16)

- [x] Separate managed runtime recovery's candidate read into a bounded (max 512) ordered cursor snapshot that explicitly checks `rows.Err` and closes the SQLite reader *before* any further read or state write. The old `defer rows.Close()` spanned recovery's loop of `instanceByID`, identity checks, HTTP probes, and SQL updates, potentially deadlocking when the DB uses a one-connection pool and pinning WAL growth on low-resource hardware.
- [x] Serialize recovery with the supervisor's residency lock; do not reset in-memory active leases when the snapshot read itself fails. The recovery still rejects unknown PID/executable/model/port provenance and uses compare-and-swap revision guards.
- [x] Add an actual single-connection SQLite cursor/lifecycle regression: snapshot returns before a subsequent state mutation, zero-row and >512-row behavior bounded, cancellation fails closed. Exact-commit hosted CI pending.
- [ ] This is daemon startup recovery correctness, not a live hardware acceptance result, physical Node/owned-target trust, independent observer vantage or Level 5 device action authority. Complete those gates, low-RAM/GPU installed tests, source audit and LAST user-focused UI/UX before approved QA installer.


## RC11 P0-CO — never escalate to SIGKILL using stale managed PID identity (2026-10-16)

- [x] Re-verify the same exact registered process identity *after* the 5–35 second graceful-stop window and immediately **before** force-kill escalation. A PID may have exited and been recycled during Colibri's drain; a stale `Alive(pid)` is not permission to SIGKILL an unrelated host process. On mismatch, deny signal, persist `orphaned` state with compare-and-swap revision and require operator reconciliation.
- [x] Start/Stop source-order contract ensures initial SIGTERM occurs before a second explicit `VerifyIdentity` prior to SIGKILL, including fail-closed orphan path. Hosted exact-source CI pending.
- [ ] Unix PID identity can still race between last inspection and signal; physical process handle/pidfd/Windows process-handle safeguards and installed acceptance remain separate. This change grants no autonomous security remediation, Node/target physical attestation or network-vantage proof. Full source audit and FINAL human-centred UI/UX remain build gates.


## RC11 P0-CP — idle reaper cannot terminate active, recently used or newly pinned models (2026-10-16)

- [x] Route cleanup through the same residency → activity lock ordering as inference acquisition, recheck actual active-request lease, pinned Colibri state, model status and most recent activity against the selected cutoff before stopping a process. A previously selected but now busy/starting/recently-used/pinned model must not be killed by the low-RAM idle reaper or Agent Check cleanup. User-invoked Stop remains explicit.
- [x] Close the bounded SQLite idle candidate SELECT cursor before any process stop or state write, removing another single-connection SQLite deadlock and WAL-pinned reader scenario.
- [x] Add one-connection SQLite tests for cursor closure, concurrent active worker guard and pin admission, with no actual network/process effects in hosted tests. Exact-source CI pending.
- [ ] Installed physical model process lifetime and Windows/Ubuntu GPU/RAM teardown still require acceptance. This is not physical Node or independently verified network sensor proof, new Level 5 autonomous authority or completed source security audit; final holistic user-first UI/UX remains LAST before approved QA installer.


## RC11 P0-CQ — release SQLite resident/shutdown readers before stopping models (2026-10-16)

- [x] Both GPU `ensureCapacity` and service `Shutdown` previously used deferred `rows.Close` yet called `Stop` inside the same reader lifetime. Under SQLite single-connection limits this can deadlock while a still-open SELECT occupies the only pooled connection, and holds WAL read snapshots throughout long subprocess drains. Explicitly close the reader after checking `rows.Err` and **before** any stop/eviction; retain deferred closure for earlier scan failures.
- [x] Actual one-connection SQLite test exercises real `Shutdown` of two in-memory already-exited managed models and verifies both are stopped, no signal sent and deployment residency synced. Source-order contract also guards capacity-reader closure before eviction. Hosted exact-source CI pending.
- [ ] Real Windows/Ubuntu process-tree stop/GPU VRAM reclamation and app-host endurance still need physical acceptance; unrelated Node/observer proof, approved L5 reversible security effects and final source/UI review remain release gates.


## RC11 P0-CR — unresolved process-stop failures must remain visibly orphaned (2026-10-16)

- [x] A signal failure on a **still-living** managed local runtime previously left its durable state `draining` and simply returned an error. If SIGTERM or SIGKILL is refused, reclassify the unresolved process as `orphaned` using the original revision-checked transition, with an operator-reconciliation reason; never claim memory was freed. No extra signal retries, force escalation or unapproved network/device actions.
- [x] Ensure no raw OS signal error or potentially private process command-line content is stored in the common durable failure reason. Source-order regressions verify both signal failure branches transition to an orphaned state before returning. Hosted CI pending.
- [ ] Real Windows process-handle race avoidance, GPU process-tree cleanup, installed memory observability and physical Node/observer proof still require separate acceptance. L5 defensive authority, security audit, final holistic human-centred UI/UX remain release gates.


## RC11 P0-CS — reject overflowing or negative accelerator memory estimates before eviction (2026-10-16)

- [x] Managed GPU placement sums and resident memory accounting now refuse zero/negative and signed-int64 overflowing device allocations, rather than silently wrapping. Both immediate admission and best-case idle-reclaim checks compare requested bytes against the **remaining** positive device budget using subtraction, not potentially overflowing `used+target` arithmetic.
- [x] Regression covers two 2^62-byte declared allocations that could overflow to a false fit, invalid signed allocations and unoccupied-only experimental overrides. No model is evicted for an invalid estimate. Exact-source hosted CI pending.
- [ ] These are static estimates, not measured VRAM; real device telemetry and old-hardware QA are still required. Independently trusted physical network evidence, human-approved reversible L5 actions, source audit and LAST human-centred UI/UX remain build gates.


## RC11 P0-CT — inconsistent idle-device allocations cannot fabricate free memory (2026-10-16)

- [x] Protect best-case prospective GPU reclaim from malformed idle records (negative/zero allocations or an idle model claiming more bytes than the current ledger says are resident). Reject admission rather than clamping an underflow to zero and claiming an impossible model swap can fit. Regression covers contradictory memory accounting; no process mutation.
- [ ] Exact-source hosted CI and actual GPU RAM readings remain separate acceptance, along with physical Node verification, safe human-governed L5 effects, code audit and LAST operator-first UI/UX.


## RC11 P0-CU — deny duplicate engine launch while failed/orphaned PID lives (2026-10-17)

- [x] A managed runtime row in `orphaned`, `failed`, `stopped`, `draining` or unknown state must not be overwritten with a new PID while its previous process is still alive. Refuse the replacement before Colibri swap, memory eviction or engine launch. Preserve the original PID and revision for operator reconciliation without signalling an unverified process.
- [x] Behavioural state-table regression covers live orphan/failed/stopped/draining processes, exited processes and healthy/busy/starting identity-gated reuse; source-order regression ensures the guard precedes all launch effects.
- [ ] Exact-source hosted CI, installed Windows process-handle recovery and actual RAM/VRAM/process-tree measurement remain acceptance gates. No automatic recovery or Level 5 network remediation authority is granted. Physical paired-Node proof, independently corroborated observations, full source security audit and LAST human-centred UI/UX still block an authorised installer.


## RC11 P0-CV — protect pinned and unrelated models from GPU pressure eviction (2026-10-17)

- [x] Current Colibri pin flag is included in resident accelerator accounting: pinned/active models continue to occupy GPU memory but never enter the automatic idle eviction set. A plan cannot count their claimed memory as reclaimable.
- [x] Reclaim only idle healthy residents on *constrained* target accelerators, not unrelated GPUs or CPU-only models. Before actual stop, recheck durable pin, recent use, active lease and runtime state under the same residency/activity lock order as inference acquisition; preserve live models when those states have changed. Single-connection SQLite and admission regressions added.
- [ ] Exact-head hosted CI, actual VRAM/RAM reclaim on constrained Windows/Ubuntu hardware, physical Node identity, independently corroborated observations, separately approved Level 5 effects, final source audit and LAST human-centred UI/UX remain pending before installer authorisation.


## RC11 P0-CW — CPU model admission accounts for concurrent managed residency (2026-10-17)

- [x] Aggregate active and unresolved-live-PID CPU-only model estimated footprints on the local Node before admitting another CPU-only model; maintain the existing bounded OS/OnePane reserve. CPU-only models no longer each individually claim the whole physical RAM allowance while already-loaded CPU models are ignored. No evictions, model fallback, cloud routing or permission expansions.
- [x] Explicitly close single-connection SQLite residency cursors, bound the scan, refuse malformed placement data and signed memory-overflow estimates. Regression covers multi-resident 8 GiB admission, orphaned live PID conservatism, changed residency, malformed plans, cancellation and overflow. Does not infer actual available memory/RSS from cached hardware-profile total RAM.
- [ ] Source-head CI and actual Windows/Ubuntu concurrent CPU/RAM pressure, swap/queue latency, GPU-host-shared memory behaviour and unmanaged-process memory remain hardware QA. Paired-Node proof, human-approved L5 reversible effects, independent observations, final source audit and LAST human-centred UI/UX remain build gates.


## RC11 P0-CX — malformed resident GPU plans and VRAM headroom overflow fail closed (2026-10-17)

- [x] Use quotient/remainder arithmetic for GPU VRAM headroom, avoiding full signed-64-bit bytes-times-percentage multiplication overflow. Invalid negative capacity or out-of-range reserve now denies placement instead of manufacturing safe GPU memory.
- [x] Before hot-swap, refuse malformed target plan JSON and unverified/malformed placement for a still-active GPU or CPU resident instead of silently ignoring unknown resident claims. Never evict a working model when ledger trust is missing. Unit arithmetic vectors include max-int64 VRAM and source-order test confirms denial precedes eviction.
- [ ] Exact-head full hosted CI and physical telemetry/device identity remain outstanding. This is static-plan safety, not real VRAM/RAM measurement, independent Node proof or L5 remediation authority. Final source/security audit and LAST operator-centred UI/UX still precede an approved QA installer.


## RC11 P0-CY — routine action must resolve to its own Project and app identity (2026-10-17)

- [x] The recurring Project Routine executor now requires exact selected RoutineBinding ID and nonempty canonical Project/runtime IDs, re-checks the resolved runtime's ID and Project ID against the binding after Task->Project validation, and requires the returned application ID and runtime ID to match. Any mismatch fails before the sandbox Tool Gateway, without issuing effects or relying on a tenant-only workspace label.
- [x] Negative tests attempt stale binding replacement, empty Project identity, cross-Project runtime substitution and foreign app/runtime pairing; legitimate matching routines still execute through the existing governed Tool path.
- [ ] Source-head CI and live Workspace/routine installed validation remain open. The existing independent Task/Project/runtime sandbox and capability boundaries still apply; this does not grant shell/Network/Secrets authority, independent Node identity, Level 5 remediation or release approval. Final security audit and LAST operator-first UI/UX remain gates.


## RC11 P0-CZ — bound persisted Routine shell command arguments (2026-10-17)

- [x] Project Routine app-command execution rejects stored action-spec JSON larger than 64 KiB before parsing, refuses empty executable and NUL bytes, limits each argv element to 4 KiB, all argv bytes to 16 KiB and argv count to 128 before invoking the governed sandbox Tool Gateway. No process or network permission expansion.
- [x] Tests cover empty command, blank argv0, embedded NUL, per-argument/aggregate/whole-JSON limits, excessive count, no-Gateway side effects and a valid bounded command. Exact-source hosted CI pending.
- [ ] This is bounded parser/adapter admission, not independent container/tool hard isolation or a universal command allowlist. Installed sandbox QA, physical Node trust, Level 5 effects, full security audit and LAST human-centric UI/UX still block an approved release.


## RC11 P0-DA — cancelled TCP observer request must durably finish claimed attempt (2026-10-17)

- [x] Manual and separately human-consented recurring TCP observations now finalise already-claimed attempts through a three-second, cancellation-independent SQLite cleanup context, carrying original request values without extending network execution. This prevents a cancelled user request, daemon cancellation or an interrupted socket from silently leaving an indefinitely `claimed` attempt when SQLite is writable.
- [x] End-to-end integration regression creates two separately approved RFC1918 targets and uses fake dialers that cancel the parent context in `DialContext`. Both must produce exactly one immutable cancelled terminal receipt and audit event, refuse a second completion, and remain non-retriable during the same cadence after a simulated worker restart. No external sockets or device-health claims.
- [ ] A physical process kill, SQLite full/disk error or external socket outcome may still leave an `unknown_after_interruption` claim requiring reconciliation. This grants no L5 remediation privileges and does not prove paired-Node/device identity. Exact-head CI, real low-resource QA, source audit and LAST holistic UI/UX remain packaging gates.


## RC11 P0-DB — read-only Level 5 drift alerts must not deadlock SQLite (2026-10-17)

- [x] Bound and fully materialise the at-most-100 TCP reachability alert rows, check scan errors and close the initial SQLite reader *before* issuing per-alert historical return-to-baseline lookups. Previously the nested QueryRow could block on the outer read cursor when OnePane runs with a one-connection SQLite pool, hanging the Level 5 diagnostic/Inspector view.
- [x] Real SQLite integration regression creates a scoped mismatch alert, switches to `SetMaxOpenConns(1)`, reads the alert under a bounded two-second request context and retains human-only acknowledgement and append-only evidence behaviour. No new network probes or runtime changes.
- [ ] Exact-commit hosted CI, physical low-memory/WAL acceptance, physical Node verification and independent L5 health/effect proof remain. Final security audit and LAST operator-first UI/UX still precede any approved installer.


## RC11 P0-DC — TCP terminal evidence bound to immutable claimed metadata (2026-10-17)

- [x] Completing an existing manual or separately consented recurring TCP attempt now requires matching all canonical immutable fields in the SQLite UPDATE: exact Project/Workspace, Goal, permit, approved target, source, cadence, Goal/permit revisions, actor, claim timestamp, recurring grant and baseline claim snapshot. A claim ID alone can no longer finalise a forged/sibling-source observation or generate a false drift/audit notice.
- [x] Real SQLite adversarial regression mutates target, Workspace, source, baseline, timestamp, grant and permit revision and verifies the actual claim stays `claimed` without a terminal effect, while a genuine human-authorised cancellation can still finish once. This does not change approvals, require automatic retries or enable device remediation.
- [ ] Exact-head CI, physical consent/revocation concurrency, tamper-resistant cross-node evidence, independent observation and human-governed L5 effects remain open. Final code/security audit and LAST operator-centred UI/UX remain release gates.


## RC11 P0-DD — competing recurring observers cannot claim false cursor progress (2026-10-17)

- [x] The bounded Level 5 TCP recurring worker now checks actual SQLite RowsAffected for its revision-fenced cursor update. A second poller using a stale revision receives `ErrRevisionConflict` instead of success after a zero-row UPDATE. Per-source/window atomic attempt admission remains the separate no-duplicate-socket authority.
- [x] One-connection SQLite unit regression verifies monotonic cursor advancement, stale-poller denial, unchanged persisted pointer and cancelled updates without extra network effects.
- [ ] Hosted exact-head CI, real multi-daemon scheduling, physical network proof, approved reversible L5 actions and installed QA remain separate. No privileged background operations or release permission are implied; final audit and LAST human-centric UI/UX are pending.


## RC11 P0-DE — restored plan hash cannot revive historical recurring consent (2026-10-17)

- [x] Live/read-only Level 5 TCP recurring-grant admission now checks the immutable manual permit's plan generation against the currently active Goal generation, in addition to the existing hash, permit revision, source/target and human approver checks. Also requires the exact permitted TCP effect class and bounded single-target port/timeout. An old grant cannot appear active when a changed source plan is restored to identical JSON.
- [x] Real SQLite integration exercises human-approved recurring monitoring, two source-plan edits returning to the same plan hash, `TCPRecurringGrantAllowed` negative validation and `GoalEvidenceCoverage` refusing to display live authority, while preserving human revocation of the historical approval. The atomic socket claim already enforced generation; the new change closes status/projection parity.
- [ ] Exact-source CI, last-boundary off-host transport proof, physical Node/target identity and approved reversible L5 effects remain incomplete. No implicit permissions, autonomous remediation or release authority are added. Final security audit and LAST user-centred UI/UX still gate packaging.


## RC11 P0-DF — remote inference dispatch requires both Node confirmations (2026-10-17)

- [x] The final local remote-model dispatch authority now also requires the selected peer to be a remote Node (not the local Node) and requires both `node_pairings.local_confirmed` and `peer_confirmed` to remain exactly 1. A `paired` status alone can no longer transmit private inference prompts when either direction of human pairing consent has been withdrawn.
- [x] Hosted SQLite/transport integration regression first establishes a valid paired manifest, revokes each direction of pairing confirmation independently, and checks both direct dispatch admission and the transport's *pre-network* fail-closed `remote_node_not_admitted` response. No external socket or cloud fallback is introduced.
- [ ] Source-head CI, physical paired-Node certificate/endpoint verification, remote process execution and Windows/Ubuntu installed testing remain open; L5 independent evidence/remediation, final security audit and LAST operator-first UI/UX still block packaging.


## RC11 P0-DG — federation endpoint lookup rechecks dual-confirmed peer trust (2026-10-17)

- [x] `pairedEndpoint`, the shared final federation transport endpoint resolver used for inference, remote model management and Node measurements, now requires the target Node to be remote and both pairing confirmation directions still exactly active. This checks current trust again after the remote model dispatch eligibility snapshot, denying a stale `paired` status at the last local endpoint-selection boundary.
- [x] Real SQLite integration verifies a fully approved endpoint, withdraws local and peer confirmation individually, checks no endpoint is returned for either, restores pairing and confirms legitimate endpoint access; the preceding model dispatch admission remains an independent check. No new network access or remote management capability is granted.
- [ ] This is pre-network software consent and certificate-pin enforcement, not independently attested physical Node/device identity; a revocation after the final local check cannot rewind an already-started TLS request. Real dual-Node transport, L5 independent observation and separately approved effects, integrated security audit and LAST operator-first UI/UX remain release gates.


## RC11 P0-DH — pinned federation transport never follows POST redirects (2026-10-17)

- [x] The shared pinned-mTLS federation HTTP client now explicitly refuses all 3xx redirect following with `http.ErrUseLastResponse`. This protects private inference prompts, Pairing credentials, model-management inputs and approved Node measurement payloads from Go's 307/308 POST-body replay to an unapproved Location, including an HTTP downgrade or other unpinned origin.
- [x] Synthetic HTTPS paired-leaf test sends representative JSON POSTs to a TLS-pinned server returning both 307 and 308 redirects to a separate HTTP receiver. A redirect must be reported as failure while the external capture endpoint remains untouched. No production network scan or scope expansion.
- [ ] Hosted CI, real authenticated paired-Node TLS dispatch and browser/CLI installed acceptance still required. Other physical Node/Level 5 verification, security review and LAST user-first UI/UX remain release gates.


## RC11 P0-DI — reject CA-capable federation peer certificates (2026-10-17)

- [x] Pinned federation server-side TLS trust now requires the enrolled peer to present a leaf certificate that is neither marked as a CA nor capable of signing subordinate certificates. A matching SHA-256 fingerprint, server-auth EKU and valid dates are necessary but no longer sufficient for a CA-capable key to be used as the Node endpoint identity.
- [x] Existing certificate-validation unit regression extended with explicit `IsCA` and `KeyUsageCertSign` negative cases. All approved normal OnePane Node leaf certificates still retain the required server-auth and digital-signature purpose.
- [ ] Full hosted CI and actual physical peer certificate/rotation tests are separate gates; this strengthens software credential scoping, not hardware attestation or Level 5 external observation. Final audit and LAST human-first UI/UX remain pending.


## RC11 P0-DJ — inbound mTLS peer must have an unexpired client-auth leaf (2026-10-17)

- [x] Because the existing Go federation TLS listener uses `tls.RequestClientCert`, simply possessing a paired SHA-256 fingerprint did not enforce certificate validity time, client-auth EKU, leaf-only/non-CA purpose or digital-signature key usage on inbound inference and remote management requests. The `peerFromTLS` gate now validates all of these before database pairing lookup, and restricts the joined peer to a remote Node.
- [x] Pinned-node tests use a real OnePane-generated Ed25519 client certificate, exercise current vs expired/not-yet-valid/wrong-purpose/CA/cert-signing variants and, with migrated SQLite paired records, deny an expired client at the actual request identity boundary. No new pairing permission or network scope.
- [ ] Exact-head hosted CI and installed cross-Node TLS acceptance remain pending. A valid software certificate is not physical-device attestation or independently verified L5 observation; final code audit and LAST human-first UI/UX still block an approved installer.


## RC11 P0-DK — cancelled inbound federated inference retains exact terminal receipt (2026-10-17)

- [x] After an already admitted remote request completes, inbound federation inference finalises its existing idempotency receipt using a three-second cancellation-independent SQLite cleanup context. This is receipt-only (never a retry or a new network effect). The error, invalid-output and successful-output branches now propagate DB failure rather than return a misleading completed response.
- [x] Direct `context.Canceled`/`context.DeadlineExceeded` errors without a known transport outcome are classified as `unknown` instead of definitively failed; subsequent matching requests remain blocked from repeating an uncertain effect. SQLite integration uses an executor that cancels the HTTP context during inference, verifies the durable known/unknown status and one-time idempotent replay semantics.
- [ ] A genuine host kill, DB disk failure or model process crash can still leave a historical `executing` receipt pending manual reconciliation. CI and physical dual-Node interruption tests remain, together with trusted L5 observation, operator-approved reversible effects, source/security audit and LAST human-centred UI/UX prior to installer authorisation.


## RC11 P0-DL — terminal federation receipt must atomically match executing scope (2026-10-17)

- [x] After an already admitted inbound inference effect, every terminal receipt writer requires the original `executing` state and exact immutable row identity (receipt ID, request digest, peer Node, remote request ID and deployment ID), and checks SQLite RowsAffected=1. A reconciled, superseded or missing receipt cannot be silently overwritten or returned as a successful inference. Retains the prior bounded cancellation-independent receipt-only cleanup.
- [x] Real SQLite integration injects an intervening independent `unknown/operator_reconciliation_pending` receipt transition *during* a simulated model execution. Success, error and invalid-output branches must all return a conflict without overwriting the original unknown evidence or running the effect a second time.
- [ ] Exact-head hosted CI and real host interruption / distributed receipt reconciliation remain unverified. This does not add a retry, grant physical Node identity or L5 remediation authority. Final integrated security audit, physical Windows/Linux QA and LAST human-first UI/UX remain release gates.


## RC11 P0-DM — bound federated inference success payloads (2026-10-17)

- [x] Refuse empty, malformed or greater-than-3-MiB federated model response JSON and malformed or greater-than-64-KiB usage JSON before terminal success. Persist explicit `invalid_remote_response`/`invalid_remote_usage` known-error receipts with the original executing-claim CAS instead of storing non-replayable or oversized success JSON on low-memory Nodes. Preserve the existing one-time model execution contract.
- [x] Non-network unit regression covers valid response/optional usage, malformed JSON and both oversized response/metadata classes. Historical large streaming results require a separately designed chunked protocol rather than silently increasing HTTP/SQLite resource use.
- [ ] Hosted CI and installed model stress acceptance remain open; physical pairing, independently trusted Level 5 observation, separately approved remediation, final audit and LAST operator-first UI/UX remain release gates.


## RC11 P0-DN — reject duplicate JSON fields on every federation API request (2026-10-17)

- [x] Shared paired-Node federation request decoder now reads at most 4 MiB before parsing, applies existing depth-limited Unicode-alias-aware `ValidateUnambiguousJSON`, and then applies the current strict unknown-field and single-document EOF checks. Repeated or escaped-equivalent request keys (including in nested inference JSON) cannot silently override previously authorised fields. Failures remain generic `400 invalid request`, without echoing payloads.
- [x] Decoder regression covers two root documents, duplicate root keys, escaped aliases, nested repeated keys in allowed raw message envelopes and oversized requests; valid requests continue to decode. No new network rights, agents or pairing paths.
- [ ] Hosted source CI, real paired-Node payload compatibility, and independent physical Node/Level 5 trust remain acceptance gates, along with code audit and LAST operator-first UI/UX before an authorised installer.


## RC11 P0-DO — bound peer JSON transport and redact untrusted error bodies (2026-10-17)

- [x] Shared federation outbound JSON sender now rejects marshal failures and requests larger than 4 MiB before network dispatch, refuses successful peer responses larger than 4 MiB (rather than silently truncating), and never includes remote HTTP error response bodies in local errors/logs. Those bodies may reflect private prompts, pairing tokens or device metadata. A non-2xx status preserves only its HTTP code.
- [x] In-process HTTP regression verifies a legitimate reply, private-marker 403 rejection without text disclosure, oversized successful response denial, over-budget request pre-network admission and failed marshal pre-network admission. No change to human grants or paired TLS policy.
- [ ] Hosted exact-source CI, installed mTLS tests and final human-centred UI/UX remain open. This is bounded transport hygiene, not physical Node identity or independent L5 verification; no release packaging or new privileges implied.


## RC11 P0-DP — bind inbound compute authority to exact live peer manifest (2026-10-17)

- [x] Inbound federated inference now checks a positive sequence, persisted reception timestamp, syntactically valid manifest whose `node_id` matches the paired Node and whose protocol matches the supported federation version, as well as current two-way pairing and expiry. A misattributed, empty or stale heartbeat no longer admits local model execution.
- [x] Migrated SQLite integration covers valid pairing, a different Node's claim, wrong/missing protocol, absent origin, zero sequence/reception provenance, expiration, revoked confirmation and unavailable peer.
- [ ] Hosted exact-source CI, real independent Node/device execution and scoped Level 5 postconditions remain pending, along with security review, installed Windows/Linux QA and LAST user-centred UI/UX before authorised packaging.


## RC11 P0-DQ — reject empty, contradictory or unbounded peer inference acknowledgements (2026-10-17)

- [x] The final remote-inference transport no longer treats a syntactically successful HTTP response with no valid model payload as an inference success. Explicit error codes require no success/usage fields and a bounded simple identifier; positive replies require a known outcome and valid bounded response+usage JSON. Ambiguous, contradictory, empty or malformed peer replies are `invalid_federation_reply` with `OutcomeKnown=false`, so routing cannot silently interpret them as successful work or safely retry a potentially executed effect.
- [x] Pure transport-contract regression exercises valid success, empty response, missing outcome, malformed usage, positive/negative error states, contradictory payloads, oversized and unsafe error-code metadata. No additional Node or model privileges.
- [ ] Hosted exact-source CI, real independently owned paired devices, L5 observation/response postconditions, installed constrained-host QA, final integrated security audit and LAST user-first UI/UX remain before installer authorisation.


## RC11 P0-DR — reject ambiguous paired-Node HTTP response envelopes (2026-10-17)

- [x] Shared bounded `postJSON` transport now rejects empty/`null` acknowledgements, malformed JSON, multiple root documents and duplicate field names at any depth, including Unicode aliasing, for operations expecting a typed response. Uses the existing 64-level strict validator with generic non-echoing `ErrFederationResponseInvalid`; untrusted peer content is never appended to error messages. Explicit fire-and-forget operations with `out=nil` retain optional empty replies.
- [x] Test HTTP endpoint covers valid response, empty/null, duplicated top-level and nested fields, escaped-equivalent field names, multiple roots, malformed payloads and legitimate empty reply when no result was requested. Pinned TLS identity, permission gates, body-size limits and redirect-denial stay unchanged.
- [ ] Exact-head CI and physical two-Node compatibility remain outstanding. This still cannot establish independently authenticated device observations or automatic Level 5 remediation; final security audit and LAST human-first UI/UX before approved packaging remain open.


## RC11 P0-DS — distinguish pre-send input rejection from unknown Node execution (2026-10-17)

- [x] The outbound federated-model transport now classifies only bounded JSON-serialization and oversize-request rejections that occur **before creating a network request** as `OutcomeKnown=true`, using explicit typed sentinels. Timeout, TLS, peer 429, invalid peer acknowledgement and oversized received response remain outcome-unknown; an already executed remote model must not be silently repeated.
- [x] Non-network classification regression verifies direct and wrapped pre-send refusals, transport failures, cancellation, and malformed or overlarge peer replies. Failed JSON marshalling no longer embeds a possibly sensitive marshaler error in externally propagated messages.
- [ ] This is a local certainty taxonomy, not a new retry policy or proof of physical effects. Exact-head hosted CI, real two-Node interruption acceptance, independently verified L5 action postconditions, code/security audit, installed QA and LAST human-first UI/UX remain open.


## RC11 P0-DT — negative manifest test respects the migration's storage constraints (2026-10-17)

- [x] The integration fixture no longer attempts an impossible `node_capability_manifests.sequence=0` update. Migration 0016 already enforces `sequence >= 1` and `json_valid(manifest_json)` in SQLite. Continue testing caller-level source/protocol/receipt provenance, expiration and permission revocation as independent reasons for denial; keep the service-side positive-sequence predicate as defence in depth.
- [ ] Hosted exact-head CI is the acceptance check; actual independently paired physical-Node verification and separate L5 effects remain unavailable without authorised physical QA. No installer.


## RC11 P0-DU — sender-side strict inference JSON preflight before network transport (2026-10-17)

- [x] A federated model request now locally validates the supported protocol, exact nonblank request and deployment identifiers, and ambiguity-free JSON in its model request, context and classification envelopes before opening a remote HTTP transport. Unicode-escaped duplicate object field names, nested duplicate keys and malformed input fail as `invalid_federated_payload` with `OutcomeKnown=true`: no remote model invocation has occurred, and the untrusted content is not echoed in error text.
- [x] Pure deterministic regression verifies legitimate JSON, missing/padded request identifiers, missing deployment, repeated/escaped keys, malformed body, duplicate context/classification data and wrong protocol. Receiver-side decoding and exact one-time idempotent receipts remain separate checks.
- [ ] The real Node still requires human pairing, transport pins and model authority; this check grants none. Hosted full source CI, physical paired-Node and L5 verified postconditions, security audit, installed QA and LAST user-first UI/UX remain release gates.


## RC11 P0-DV — end-to-end synthetic dual-Node inference and consent revocation (2026-10-17)

- [x] Extend real TLS 1.3 two-Node + two SQLite stores integration beyond direct HTTP request testing: the actual `RemoteTransport.Dispatch` now routes one imported trust-scoped remote deployment through final local manifest/consent admission, pinned two-way mTLS, B-side model admission and persisted idempotent inference receipt. A repeated matching request reuses the same response and retains one original receipt.
- [x] Withdraw the source Node's local pairing confirmation between requests; the next remote model request must be denied as a known `remote_node_not_admitted` pre-network failure and may not add any B-side receipt. No external discovery, privileged sandbox or cloud network required.
- [ ] Hosted exact-head CI pending. This synthetic loopback demonstrates software transport and idempotency only; a real independent physical Node/device, measured L5 outcomes, operator-approved reversals, final integrated security audit and LAST user-first UI/UX remain installer gates.


## RC11 P0-DW — enforce live Node compute-policy disable at the last network send (2026-10-17)

- [x] Federated model final pre-network admission now checks the current `remote_node_compute_policies.enabled` alongside exact destination, two-way pairing, manifest and deployment/model trust. An explicitly disabled Node is denied before a prompt can leave the host even when previously imported, ready and scheduled. A missing policy preserves the existing default; no new network grant is created.
- [x] Migrated SQLite integration disables/restores the remote policy, verifies the eligibility transitions and demands that direct `RemoteTransport.Dispatch` return known `remote_node_not_admitted` for a sensitive prompt when policy is disabled. This closes a last-adapter bypass of the earlier Task-side compute policy guard.
- [ ] Exact-source CI, selected-Project policy provenance at final transport, real paired physical Node acceptance, human-approved L5 effects, independent postconditions, integrated audit and LAST operator-first UI/UX remain release blockers.


## RC11 P0-DX — canonical Task selected-project Node compute admission (2026-10-17)

- [x] Before transferring Task content to trusted Nodes, enforce BOTH pairing confirmations, live manifest origin+protocol+sequence+reception, and the current `remote_node_compute_policies`. For `project_scope=selected`, the *persisted* Task Project must appear as an exact string in the operator's allowed-project JSON array; approval for another Project cannot authorise this Task. Absent legacy policies and `project_scope=all` preserve existing Task-specific consent requirements.
- [x] Extend migrated SQLite gateway tests for disallowed vs exact Project ID, policy shutdown, one-sided pairing, wrong manifest origin and restored valid admission. This is an additive condition to canonical Project/Workspace off-host transfer consent, not a cloud/spend grant.
- [ ] Current direct RemoteTransport request type carries no Task/Project provenance; its separate last network boundary checks global enable/disable but selected-project proof remains exclusively at the Task gateway. This gap is tracked for a complete identity-bound transport audit and physical validation. Final L5 effects, installed QA, code/security review and LAST user-first UI/UX still block an installer.


## RC11 P0-DY — last transport binds selected Node compute to canonical Task Project (2026-10-17)

- [x] The inference engine carries the already authorised Task ID and owning Workspace from `ExecuteCommand` in `DispatchRequest`; the final paired-Node transport uses those IDs only to re-fetch the canonical active Task/Project from SQLite and applies the operator's live `remote_node_compute_policies.project_scope=selected` allowlist on the same read as Node trust, pairing, model and manifest. Missing Task, unrelated Project, wrong Workspace or unknown policy scope cannot send a private prompt. No caller-supplied raw model JSON can create authority.
- [x] Two-Node integration fixture checks explicitly selected nonmember Project denial, matching Task Project admission, missing Task provenance, mismatched tenant and the unchanged legacy all-Project route. The earlier Task gateway remains an independent outer check; the direct RemoteTransport adapter is no longer an alternate way around the operator's project restrictions.
- [ ] Source CI and actual installed physical Node acceptance pending. Real signed device evidence, verified reversible L5 actions and security/code audit remain, with LAST human-first UI/UX and explicit packaging approval.


## RC11 P0-DZ — require canonical Project AND Workspace off-host grants at final transport (2026-10-17)

- [x] Final `RemoteTransport.Dispatch` no longer accepts a ready paired deployment or model routing JSON as transfer authority. In addition to selected-Project compute policy, it checks `featurepolicy.StoredScopes.AuthorizeNodeTransfer` against the persisted Task, active tenant, active Project and canonical Project Workspace's two independent human-controlled Node grants immediately before peer contact. A missing Task, Task with a foreign tenant, revoked Project/Workspace grant or unapproved Node fails as a known no-send condition. The previous unscoped internal checker is retained only for snapshot tests and does not open sockets.
- [x] Hosted real mTLS two-Node integration is seeded with valid canonical Task Project+Workspace grants before sending; source-admission fixture independently revokes both grants while an enabled selected Node compute record exists, then restores them. This preserves genuine authorised remote execution without granting taskless access.
- [ ] Atomicity across human grant revocation and already-started TLS transmission remains fundamentally bounded by the last local pre-send check. Physical Node/device identity, L5 independently verified remediation, installed acceptance, final integrated security audit and LAST UI/UX pass still block installer approval.


## RC11 P0-EA — deny duplicate-key Project and Workspace Node transfer grants (2026-10-17)

- [x] The shared `featurepolicy.StoredScopes` resolver now requires strictly unambiguous JSON across the complete persisted Project policy and Project Workspace settings documents before intersecting trusted-Node transfer grants. Duplicate root `onepane_routing`, duplicate inner `enabled`, Unicode-escaped aliases and nested duplicate unrelated fields all fail closed rather than relying on `encoding/json` last-value-wins semantics.
- [x] Unit regression covers each ambiguity class. Migrated SQLite integration mutates first the Project and then the Project Workspace human approval source and demands zero transferable Nodes plus explicit errors, then restores legitimate grants. Both the Task admission gate and final Node transport now consume this same checked authority.
- [ ] Exact-source CI, physical paired-Node isolation/consent acceptance, safe L5 reversible defensive actions, integrated security audit and LAST operator-first UI/UX remain release gates. This adds no transfer or cloud privileges.


## RC11 P0-EB — end-to-end dual-Node grant revocation produces zero remote effects (2026-10-17)

- [x] Extend the existing real TLS 1.3 two-service, two-SQLite federation transport integration: after a successful and idempotently replayed scoped Task inference, independently withdraw the Project Workspace transfer grant and the root Project transfer grant, then attempt new private inference requests via `RemoteTransport.Dispatch`. Each must return `remote_node_not_admitted` with a known no-send outcome and create **zero** remote-Node inference receipts. Restore each grant separately; also verify that a Taskless direct transport invocation cannot borrow historical approvals.
- [x] This asserts the live end-to-end effect boundary rather than checking only a preflight method. No extra outbound destinations or physical Node permissions are enabled.
- [ ] Exact-head CI and independently authenticated physical two-Node test remain open. Real network-device observations, human-approved reversible Level 5 effects with independent postconditions, integrated security review, installed QA and LAST human-first UI/UX still block packaging.


## RC11 P0-EC — exact live inference origin at final federated transport (2026-10-17)

- [x] The last `RemoteTransport.Dispatch` now requires a durable, currently `executing` origin `inference_requests` row before sending remote model bytes. It must match the exact request ID, canonical Task and Workspace IDs, deployment ID, provider-free route, and byte-identical stored JSON payload; a direct caller cannot borrow an authorised Task's transfer grants for unrelated work.
- [x] Real dual-Node mTLS + independent SQLite integration seeds the original inference records, confirms live authorised one-shot inference and replay, rejects a missing origin, changed model-request bytes, mismatched Task/Workspace and terminal `succeeded` request, and still requires Project and Workspace grant revocation to prevent new receiver receipts.
- [ ] Hosted exact-source CI in progress. Queued-model and process-restart evidence must still be checked on actual constrained hardware; no physical Node action or remote network scan was performed. Independently verified reversible Level 5 effects, final integrated security audit and LAST operator-first UI/UX remain release gates.


## RC11 P0-ED — Colibri planner output bounded during process execution (2026-10-17)

- [x] The read-only native Colibri placement planner no longer accumulates arbitrary stdout+stderr using `exec.Command.CombinedOutput` before performing an after-the-fact 128-KiB size check. A concurrency-safe writer drains the subprocess while retaining at most 128 KiB in OnePane RAM and reports oversized output without leaking the launcher output into errors. The existing 20-second context timeout remains in place.
- [x] Deterministic unit regression covers normal output, a large burst of planner output, and continued output after the limit. This preserves existing Colibri native package and model-pool restrictions and changes no runtime backend selection.
- [ ] Hosted exact-head CI and real constrained-hardware Colibri model-plan acceptance remain pending. This limits OnePane's memory capture, not the native subprocess's independent RAM/VRAM allocation or peak RSS.

## RC11 P0-EE — Colibri automatic context reads cannot allocate unbounded metadata (2026-10-17)

- [x] Automatic Colibri context detection now requires a regular non-symlinked `config.json`, rejects files statically over 1 MiB, and uses `io.LimitReader` for a hard 1 MiB + 1 bound even if the metadata file changes after `Lstat`. Oversize, inaccessible or invalid metadata falls back to the existing conservative 8192-token initial context.
- [x] Regression covers a legitimate model context declaration, oversize metadata and a symlinked config. Other model-discovery limits are preserved; nothing silently promotes declared context to verified inference capacity.
- [ ] Hosted source CI, actual Windows/Ubuntu SSD/model install, full Colibri-native executable QA and independent peak-RSS measurements on low-memory hardware remain release acceptance work.


## RC11 P0-EF — one bounded Colibri config reader for auto-context and model discovery (2026-10-17)

- [x] `ColibriPoolModels` now uses the same `readBoundedColibriConfig` helper as automatic context inference. Both read at most 1 MiB + 1 from model metadata, check regular-file status and decline oversize or symlinked `config.json` values. Model-pool discovery previously performed `os.ReadFile` after a bounded `Lstat`, allowing a replaced or expanding file to allocate unbounded RAM between those operations.
- [x] Unit regression independently calls the common bounded helper with an oversized file, confirms automatic context fallback to 8192 and keeps a legitimate declared context. This is read-only; it does not change model routing, downloaded model artifacts, installer storage or Colibri's native memory manager.
- [ ] Exact-head full source CI, externally invoked package robustness and peak memory proof on the Windows/Ubuntu constrained hosts remain pending, as do physical Node and Level 5 effectiveness gates, final code/security audit and LAST UI/UX review.


## RC11 P0-EG — atomic Colibri tier/pin changes under shared runtime residency lock (2026-10-18)

- [x] `SetColibriTier` now owns the supervisor residency lock continuously while reading the current managed Colibri deployment, checking whether the engine is busy, safely stopping an idle engine and persisting the new tier. A concurrent inference `Acquire`, model start/swap or idle reaper cannot interleave between the idle check and the config write. A missing supervisor is an explicit refusal.
- [x] Tier persistence uses SQLite `json_set` on the `colibri_tier` field rather than rewriting a stale full `runtime_config_json` snapshot; independent deployment properties and the operator's pin preference survive. `SetColibriPin` shares the same residency lock when a supervisor is present, removing the pin/eviction window. Regression holds the residency mutex, verifies tier/pin updates block until released, and checks the exact resulting settings. Source [CI #39059608276](https://github.com/DigiLogicTech/OnePane/actions/runs/39059608276) passed.
- [ ] Real Colibri backends, Windows/Ubuntu model swap, post-crash pin/residency and CPU/GPU resource pressure are not established by synthetic tests.

## RC11 P0-EH — cap live managed-model runtime logs while inference continues (2026-10-18)

- [x] The managed subprocess launcher now drains stdout/stderr into one concurrency-safe writer with a 4-MiB total on-disk limit for the active runtime log. The old `openManagedRuntimeLog` initial rollover alone could not prevent a long-running, verbose engine from exhausting a small SSD/C: partition. Rejected surplus bytes are acknowledged and discarded rather than blocking inference.
- [x] File descriptors are closed after the process has exited and the logging pumps finish (`cmd.Wait`), not immediately after `Start`. Regression writes concurrent oversized streams against the protected log and an actual child process writes over 24 MiB through the production `OSProcessBackend.Start` path. Existing diagnostic prefix and the strict 4-MiB bound must survive.
- [x] On Windows, verified process-tree termination via `taskkill.exe` no longer buffers unbounded utility output or includes raw command stdout/stderr in surfaced errors. Exit errors remain visible without private utility output.
- [ ] Hosted exact-head full CI [#39059928089](https://github.com/DigiLogicTech/OnePane/actions/runs/39059928089) pending at this checkpoint; physical Windows model logs, process-tree cleanup and installer storage location must be independently tested. Bounded OnePane log growth is not a limit on upstream model cache or memory allocation.

## RC11 release gates unchanged

- [ ] Independent physical paired-Node source/observer trust and actual local model runtime acceptance; human-approved reversible Level 5 actions with independent verified postconditions; deployed constrained-host Colibri CPU/GPU/RAM and Windows x64 + Ubuntu amd64 installed QA; complete integrated code/security audit. The human-focused UI/UX review is LAST before user-authorised MSI/release packaging.
- [ ] No merge or installer authorisation from this hosted source-only batch. Preserve Project, model, secret and operator-selected non-C: data locations.


## RC11 P0-EI — bounded Colibri descendant pipe-drain lifetime (2026-10-18)

- [x] The managed local model process now sets an explicit five-second `exec.Cmd.WaitDelay` after the launcher exits, so inherited stdout/stderr pipes held open by a Python Colibri child engine cannot keep the supervisor's parent PID bookkeeping apparently alive forever. Together with the runtime log writer cap, this bounds Go-side log-copy lifetime while retaining normal full stream draining for healthy processes.
- [x] The real subprocess log regression on P0-EH exercises normal launch/exit and >24 MiB emitted output. The WaitDelay behavior itself still requires installed child-engine process-tree acceptance, especially on Windows, because the hosted helper process does not model Colibri's specific child inheritance.
- [ ] Full exact-head hosted CI [#39060121768](https://github.com/DigiLogicTech/OnePane/actions/runs/39060121768) pending at documentation time. No physical runtime, Node, network or L5 side effect was invoked. All existing user-approved security/code, native Colibri resource, UI/UX LAST and installer gates remain.


## RC11 P0-EJ — rollover at an exact filled managed runtime log (2026-10-18)

- [x] The next managed model launch rotates a current diagnostic log as soon as it reaches the live 4-MiB limit, including equality. Previously a full 4-MiB log was reopened but the new bounded writer had zero available bytes, so the next model run's entirely new startup diagnostics were silently lost. The old full log is retained as `.previous` while a new empty protected log begins.
- [x] Regression writes precisely 4 MiB through the real capped writer, closes and reopens via `openManagedRuntimeLog`, and verifies preserved archived diagnostics and a new zero-length writable log. No installer paths, secrets or model configurations are modified.
- [ ] Exact-head CI, actual long-lived Colibri child-engine storage acceptance and disk-volume selection QA remain outstanding, alongside independently approved physical Node/L5 effects, final code/security review and LAST UI/UX before authorised packaging.


## RC11 P0-EK — preserve blocked Worker attempts on corrupt saved wait state (2026-10-18)

- [x] Persisted Agent Worker continuation is now validated as exactly one non-null JSON object, rejecting duplicate keys at every nesting depth including Unicode-escaped aliases. Unlike ordinary `encoding/json`, it cannot interpret `null` as "no pending local model" or accept conflicting saved retry times. Both model and approved-toolchain wait decoders use the same validator, and the Worker resume gate enforces it before considering any dependency or Operation.
- [x] A saved continuation may describe only one wait category: local model, approved Workspace toolchain or pending Operation. Mixed resource+operation checkpoints are refused rather than letting the first expired condition resume a Task with uncertain effects. An explicitly present but null or empty `operation_id` also requires operator reconciliation instead of looking like a completed wait.
- [x] Integrated migrated SQLite regression mutates an existing waiting Task's continuation through null, array, duplicate keys, Unicode alias, conflicting categories and null/blank Operation IDs. Every variant must keep its original Task and Attempt waiting. Another integration uses two independent corrupt waits and an unrelated healthy Task to prove the bad records never starve eligible work, including across repeated reconciliation.
- [x] The periodic Worker reconciler parses only the wait category present, avoiding redundant scans of model/toolchain conditions when a Task has no such wait—a small CPU saving on slower local-first hosts. No automatic cloud/model substitution, new model permissions, Task Attempt duplication or uncertain external-effect retry was introduced.
- [x] Hosted exact-source CI for the first strict decoder patch [#39064390899](https://github.com/DigiLogicTech/OnePane/actions/runs/39064390899) passed. Combined batch CI [#39064636211](https://github.com/DigiLogicTech/OnePane/actions/runs/39064636211) pending exact-head verification.
- [ ] Full constrained-hardware Worker and Colibri model queue recovery, separately authorised paired physical Nodes and reversible L5 effect postconditions, final integrated code/security audit and LAST human-focused UI/UX review remain necessary before explicit MSI approval. Do not merge or package.


## RC11 P0-EL — complete orphaned Agent-Worker recovery beyond the first 256 (2026-10-18)

- [x] Startup reconciliation previously loaded only the first 256 qualifying orphaned Agent Worker attempts. On hosts that accumulate more queued/in-flight work before a crash, later attempts were not inspected and their uncertain external effects lacked a recovery block and operator review. `recoverOrphanedAgentAttempts` now traverses the complete orphan backlog in **256-row keyset pages** ordered by task creation time, Task ID, Attempt number and Attempt ID, with bounded per-page memory. Advance the cursor by the last *inspected* row so an individually malformed/blocked Attempt cannot starve independent later Tasks, including ones with identical timestamps.
- [x] Each candidate is still independently re-authorised under its own recovery write transaction with the original live-run exclusion, Task revision compare-and-swap, and atomic interruption+audit+outbox. No unknown external outcome is automatically retried or claimed to have failed; unrelated Task executors remain excluded.
- [x] Migrated SQLite regression seeds **260** active orphan attempts, injects an unrecoverable first Attempt, expects the other **259** to be blocked and have exactly one corresponding recovery event/outbox job, repairs the first record and confirms idempotent recovery without duplicating previous Task events. This deliberately crosses the old fixed 256 scan limit.
- [ ] Full exact-source CI and practical recovery acceptance on long-running constrained-host queues are required. Real independently paired Node hardware, authorised reversible Level 5 actions with independently verified postconditions, full code/security audit, installed Windows/Ubuntu QA, and the **LAST operator-first UI/UX pass** remain prerequisites for an approved installer.



## RC11 P0-EM — bounded paginated waiting-worker reconciliation (2026-10-18)

- [x] Worker `syncResumedRuns` no longer materialises every waiting Task/Attempt on each daemon tick. It scans stable `agent_worker_runs.id` keyset pages of 256, explicitly closes each SQLite read cursor before mutating the corresponding Tasks, and advances by the last *inspected* run even when its resource/Operation continuation is malformed. Unlike OFFSET pagination, successful wakeups cannot cause other waiting Tasks to be skipped.
- [x] The existing per-Task fail-closed wait admission, Task/Attempt/Worker transactional wake, revision CAS and independent corruption reporting remain unchanged. A corrupt waiting Task cannot starve a healthy one merely because it falls on an earlier page.
- [x] Added migrated-SQLite integration using **one DB connection**, 260 healthy waiting Task/Attempt/Worker records and one corrupted model wait. The test demands all healthy work resumes with the same original Attempt, corrupted work remains suspended and reported after repeated reconciliation, and no additional Attempts are created.
- [ ] Exact-source hosted CI [#39077763132](https://github.com/DigiLogicTech/OnePane/actions/runs/39077763132) pending verification. Installed constrained-host queue throughput, hot-swap/resource pressure, crash interruption and model persistence still require hardware acceptance. No autonomous cloud fallback or unknown external-action replay was enabled.
- [ ] Independently authenticated physical Node/device tests, safe explicitly approved reversible L5 effects with verified postconditions, final integrated code/security review, Windows/Ubuntu installed QA and LAST user-first UI/UX remain mandatory before authorised packaging. PR must remain draft/unmerged.


## RC11 P0-EN — bounded active Worker crash-recovery paging (2026-10-18)

- [x] The remaining `RecoverLostRuns` startup path no longer loads every `running` Worker into one growing Go slice. It traverses in 256-record pages using `(started_at,run_id)` keyset ordering, closes each SQLite reader before performing Task/Attempt recovery writes, and advances by the last *inspected* row so one corrupt Attempt cannot prevent later Tasks from being reconciled. Unlike OFFSET paging, transitioning successfully recovered Workers out of `running` will not skip unprocessed rows.
- [x] Each Task still uses its existing atomic `InterruptForRecoveryInTransaction` and Worker compare-and-swap. Uncertain external effects are marked interrupted and require review, never automatically replayed or misreported as failed. Independent Task failures are reported while other Tasks continue; the previously paginated orphaned-Attempt reconciliation remains unchanged.
- [x] Real migrated-SQLite integration restricts the database to **one open connection**, creates **260** live running Workers, injects an unrecoverable first Attempt and forces the remaining 259 to share an identical timestamp across the keyset page boundary. It requires all 259 independently recover and emit exactly one recovery event/outbox, then verifies the repaired Attempt is recovered once and a repeated startup is idempotent.
- [ ] Full exact-head hosted CI for [P0-EN source #39083618736](https://github.com/DigiLogicTech/OnePane/actions/runs/39083618736), [initial integration #39083677102](https://github.com/DigiLogicTech/OnePane/actions/runs/39083677102), and [tied-timestamp integration #39083752064](https://github.com/DigiLogicTech/OnePane/actions/runs/39083752064) was still in progress at writing. Do not mark successful until completed.
- [ ] Real low-memory model/Colibri crashes, installer storage preservation, independent paired physical Nodes and safely reversible L5 defensive response postconditions require separately authorised hardware QA. Full code/security review and LAST human-centred UI/UX remain release blockers. Keep PR draft and do not package.


## RC11 P0-EO — failed CREATED Tasks cannot starve healthy autonomous work (2026-10-18)

- [x] Ported the isolated #222 priority-keyset admission fix into the current RC11 integration head: process `created` Tasks in stable 64-row pages ordered by priority descending, creation time and Task ID. Each page's SQLite read cursor closes before any task transition, including with a one-connection database. A failing high-priority Task remains unchanged with an actionable error; its failure does not block independent lower-priority Tasks or silently consume the successful-admission limit.
- [x] The top-level Worker `Tick` now joins and reports independent admission errors **after** processing eligible existing/ready work, instead of dropping all progress upon the first failed admission. The error is retained for operator attention; privilege/policy denials are not bypassed.
- [x] Ported the original real-SQLite regressions for 66 broken high-priority Tasks ahead of a healthy Task and multiple independent broken siblings; the 66-Task test now enforces a single database connection and confirms the healthy Task's ready event is not duplicated after another reconciliation.
- [ ] Full hosted exact-head Go/SQLite and Windows source CI is required before calling this validated. Physical constrained-host routing, model queue, failed-worker isolation and recovery throughput are separate acceptance gates.
- [ ] No alteration to model/cloud/Node grants, physical remediation authority, RC10 baseline, installer or release approval. Independently paired physical Nodes, authorised reversible L5 verification, final integrated security/code audit and LAST user-first UI/UX remain blocking.


## RC11 P0-EP — READY queue corruption isolation and single-connection progress (2026-10-18)

- [x] Ported bounded 64-row priority/time/Task-ID keyset pagination for the READY queue. Instead of `LIMIT` selecting a malformed first Task repeatedly, each candidate is read independently after the SQLite cursor is closed, so healthy lower-priority Tasks may proceed under a single database connection. Invalid records remain unmodified and their IDs are reported (first 32 errors plus an overflow count).
- [x] Worker `Tick` now preserves eligible healthy results from READY admission even when independent Task decodes fail, while reporting the failures. No invented tool, model, cloud or external effect is considered successful.
- [x] A migrated-SQLite regression seeds a damaged legacy Task JSON with a healthy lower-priority sibling and verifies queue isolation and Tick's safe blocked result after the Project Workspace is archived; no new Attempts are granted.
- [ ] Full exact-head Go/SQLite CI and real low-memory local-host acceptance outstanding.

## RC11 P0-EQ — active Worker decode isolation (2026-10-18)

- [x] Active Worker selection no longer stops on the first malformed persisted Worker JSON and hides every healthy Worker behind it. Scan bounded 64-row `(updated_at,id)` keyset pages, close the reader before loading and validating each run independently, recheck current running state and cap diagnostic allocation at 32 individual failures plus overflow count. A corrupt record is not retried, repaired or executed automatically.
- [x] Migrated-SQLite integration seeds one corrupt high-priority active Worker and one healthy sibling, verifies the second is returned with the first's ID in diagnostics, and ensures archived-Project admission blocks unsafe execution on Tick. The fixture limits SQLite to one connection.
- [ ] Exact-head hosted CI, actual crash-recovery/Colibri/Windows process observations, security and final human-centred UI/UX audit remain gates. Existing RC10 build and operator-selected non-C: model/project storage are untouched. No installer/merge has been authorised.


## RC11 P0-ER — bounded diagnostic memory for failed Worker recovery (2026-10-18)

- [x] Recovery of damaged Worker state is now bounded not only in SQLite query page size but also in error retention. Shared `boundedWorkerRecoveryErrors` samples at most 32 independent failures and counts additional failures without allocating an unbounded slice or flattening an unbounded error string. Terminal query/SQLite cursor errors remain visible even when per-record samples are exhausted. A nil error does not consume a slot.
- [x] Applied the bound to all three previously unbounded paths: startup live `RecoverLostRuns` error collection, per-tick `syncResumedRuns` waiting-state reconciliation and `recoverOrphanedAgentAttempts` recovery. Every candidate still receives its original safe transactional check; surplus *diagnostic copies* are omitted, not Tasks or attempts. Failures are not classified as proof of failed external effects.
- [x] Unit regression checks the first 32 identities, accurate overflow count, `errors.Is` preservation of original and fatal causes, nil/empty behavior and bounded report length. New migrated SQLite tests cover 41 corrupt persisted waits with one healthy resuming, 42 failed orphans with three healthy recoveries and 42 corrupt running Workers with three healthy recoveries; each verifies bounded error visibility, one-connection database access where relevant, no duplicate Task Attempts, post-repair idempotence and event/outbox safety.
- [ ] Latest integration-head hosted CI must pass before this batch is considered validated. Controlled low-memory long-running Node acceptance, actual model queue/Colibri faults and operator-visible diagnostic UX still require physical QA.
- [ ] No more cloud, Node, filesystem, network or remediation privileges are introduced. Real paired-node authorization, independent L5 postcondition verification, final integrated code/security audit, LAST user-first UI/UX and explicit Windows/Ubuntu installer/upgrade QA continue to block release or merge.


## RC11 P0-ES — bounded CREATED admission scan with fair continuation (2026-10-18)

- [x] After priority-keyset paging and per-Task error isolation, a corrupted backlog could still cause one `admitCreated` scheduler tick to attempt an unbounded number of failed Task transitions. Constrained CPU and SQLite write time remained proportional to total corrupt queue length. The admission loop now inspects at most **128 candidates per call** in two 64-record keyset pages and preserves its position on the owning `agentworker.Service` between calls under a mutex. Cursor ordering is stable by priority descending, creation time and Task ID.
- [x] Existing best-effort semantics remain: no Task is silently marked READY, no failed Task is deleted, priority order applies within each scan slice, and the first 32 admission failure records plus overflow count remain visible. On reaching the queue end, the cursor wraps safely for new or previously failed high-priority work. Service restart resets the process-local cursor conservatively; it neither persists privileges nor retries an uncertain external effect.
- [x] Migrated-SQLite regression with **one database connection**, 130 repeatedly failing high-priority CREATED Tasks and one healthy lower-priority Task: the first call must cap at 128 failures, the second must advance past the remaining two failures to promote the healthy Task once, and subsequent cycles must neither silently promote corrupt Tasks nor duplicate its ready audit event.
- [ ] Full exact-head hosted CI and real low-resource/large-backlog tick latency acceptance required. This is bounded CREATE-admission work, not a claim that all scheduling paths or physical Worker/Colibri load are yet bounded.
- [ ] Final integrated code/security review, independent paired physical Nodes, verified reversible consented L5 effects, installed Windows/Ubuntu QA and LAST user-first UI/UX remain mandatory. PR #227 remains draft; no MSI/release approved.


## RC11 P0-ET — bound active Worker candidate scans per scheduling Tick (2026-10-18)

- [x] The existing active Worker selector used 64-row keyset pages, but could still traverse an arbitrarily long prefix of corrupted running rows on **each** Tick before returning one healthy Worker. It now limits a call to 128 inspected candidates and uses a mutex-guarded, process-local `(updated_at,id)` cursor to resume fairly on subsequent calls. Cursor reads close before individual `getRun` decoding and no run status, Attempt, grant, model choice or external effect is changed by selection.
- [x] SQLite integration adds 130 synthetically corrupt active runs ahead of one healthy run, single-connection enforcement, an initial 128-failure report with 32 sampled IDs plus overflow, next-call healthy selection, retained running/Attempt state and safe cursor wrap.
- [ ] Exact-head full source and integration CI still required at writing; real low-resource Worker throughput, crash/restart and model hot-swap acceptance remain independent hardware gates. Process restart resets the fair cursor rather than persisting hidden scheduler authority.

## RC11 P0-EU — bound READY Task candidate scans per scheduling Tick (2026-10-18)

- [x] Previously, `readyTasks` keyset-paged but could inspect an unbounded backlog of malformed READY rows to load one healthy Task. It now inspects no more than 128 rows per call, keeps a mutex-guarded, process-local `(priority,ready_at/created_at,id)` continuation and safely revisits the highest-priority head when the suffix is exhausted. Each candidate is loaded after closing the SQLite read cursor, and state is checked again before inclusion. Operator-visible error details remain capped at 32 with overflow; these read-only scans never grant a tool or execute a Task.
- [x] New one-connection SQLite regression builds 130 bad high-priority READY records plus a valid lower-priority Task, checks first-call 128 inspection failures, second-call fair selection, unchanged Tasks/Attempts and safe wrapping without silently discarding corrupt records.
- [ ] Source-level CI confirmation, real constrained-hardware latency and Task admission observations are pending; this does not claim bounded `syncResumedRuns` waiting reconciliation, runtime/model process memory, remote paired-Node authority or tested Level 5 postconditions.
- [ ] Both P0-ET and P0-EU remain inside draft PR #227. Mandatory outstanding gates: independently paired physical Nodes, actual Windows/Ubuntu Colibri/model process and storage acceptance, consented reversible L5 effect verification, final integrated code/security audit, LAST human-focused UI/UX review, explicit installer approval and real installed upgrade/data-preservation QA. Never merge/release based on hosted tests alone.


## RC11 P0-EV — preserve queue cursor after early quota on short pages (2026-10-18)

- [x] The rotating CREATED, READY and active Worker selectors previously reset their process-local cursor whenever a SQLite query returned fewer than 64 rows—even if processing stopped **before the end of that returned page** because the successful admission/selection quota was reached. With a persistent earlier high-priority failure, this could re-read that failure each Tick, and READY/active selectors could repeatedly choose the same first healthy record instead of other valid siblings.
- [x] All three paths now reset the cursor only after the returned short page is **actually exhausted**, not when the caller's quota ends iteration early. The next call resumes strictly after the last inspected candidate; after the tail is truly reached it wraps once to the queue head. Existing limits of 128 inspected candidates, 32 sampled errors plus an overflow count, fixed Task/Worker identity and per-effect authority checks remain unchanged.
- [x] Added migrated SQLite, single-connection integration regressions for 3 healthy active Workers (repeated limit-one calls must rotate 1→2→3→1); 3 READY Tasks with priority order (rotate 1→2→3→1 without creating Attempts); and 1 persistently failing high-priority CREATED Task ahead of 2 healthy Tasks (each healthy Task admitted exactly once without unnecessarily revisiting the failure, then the cursor wraps to preserve visible bad-state diagnostics).
- [ ] Hosted exact-head full CI for this short-page fix and regression must complete before validated. No change to physical Node identities, research-mode pinned models, privileged action grants, Colibri RAM/VRAM residency, L5 permissioning or installer storage. Repeat complete final security/code audit, independent physical Node, consented reversible L5 and installed Windows/Ubuntu QA before the mandatory LAST user-focused UI/UX review and explicit packaging approval.


## RC11 P0-EW — bounded waiting Worker reconciliation with durable continuation identity (2026-10-18)

- [x] `syncResumedRuns` previously keyset-paged waiting Workers but traversed *all* pages in one Tick, repeating dependency/Operation/resource checks and consuming unbounded CPU/database work in a large stalled queue. It now inspects at most **128** waiting Workers per invocation in 64-record pages and retains a mutex-protected, process-local `run.id` cursor between calls.
- [x] Only actually inspected Worker IDs advance the cursor. SQLite page cursors close before dependent read/write transactions; an exhausted short page resets to the head for future resource/approval rechecks, and an old cursor positioned at an emptied suffix may wrap once without losing a whole Tick. Cancellation/errors do not skip uninspected Tasks. Failed continuations retain the 32-identity diagnostic cap and accurate overflow count.
- [x] Original `resumeWaitingWorkerAndAttempt` atomic Task/Attempt/Worker/outbox transition, matching id/revision guards, pinned local model and approved Workspace toolchain checks, human approval and committed Operation requirements remain unchanged; no external step is re-executed from a presumed failed wait.
- [x] Updated the pre-existing 261-wait SQLite one-connection regression to require bounded multi-Tick completion rather than an unsafe all-in-one Tick. New 132-wait migrated SQLite regression has 131 corrupt waits, one healthy continuation sorted after them, an exactly-128 first-pass error budget, fair second-pass wake, original Attempt survival and safe repeat inspection with no duplicate Attempts. Hosted source head `cffb3b8a4c86a95779ae8a5232333aa1aeec865f` passed full CI.
- [ ] Paired Node, truly resource-constrained long-running Colibri/model, cross-platform installed performance, timeline-based operator UX and independent hardware acceptance are NOT tested by hosted SQLite.

## RC11 P0-EX — active vs READY queue starvation prevention in Tick (2026-10-18)

- [x] Previously, when at least `limit` healthy active Workers remained, `Tick` always processed the entire active slice and returned before inspecting any READY Tasks. This could starve new queued autonomous work indefinitely even while eligible.
- [x] `Tick` now reads bounded READY candidates before allocating active slots and reserves **one** READY opportunity per Tick when `limit>=2`, while guaranteeing at least one active slot. Spare slots still process additional READY Tasks. When `limit=1`, a small mutex-protected process-local preference alternates active/READY opportunity when READY work exists, so a repeatedly ineligible READY Task does not itself starve active Workers. Existing `startRun` CAS, project/workspace admission, authority and auditing remain authoritative; queue selection alone does not create an Attempt.
- [x] New migrated SQLite one-connection regression supplies two or three active Workers and an ineligible READY Task under an archived Project Workspace. Multi-slot Tick must surface both queues on the same call and progress every active Worker over repeated calls. Single-slot Tick must alternate opportunities with an explicit denied READY result and no unauthorized/new Task Attempt. None of these regressions grants remote tool/model authority.
- [ ] Verify latest combined exact-head hosted CI, then accept independently paired physical Nodes, local Colibri runtime limits, Level 5 consent/postconditions, Windows/Ubuntu upgrade and chosen non-C: data preservation, final code/security audit and LAST human-centred UI/UX before explicit packaging approval. This branch stays draft and unmerged.


## RC11 P0-EY — reject overlapping scheduler/recovery cycles while inference is in flight (2026-10-18)

- [x] A keyset cursor and `markReasoningDispatch` revision CAS do not alone guarantee that a second concurrent Tick on the same Agent Worker Service cannot select a now-revision-incremented active Worker and issue another reasoning request before the first model response/proposal has completed. This risks duplicate paid/subscription inference, uncontrolled resource contention on old hardware and overlapping application of independent proposals.
- [x] A single `workerCycleMu` now bounds the **complete** `Tick` cycle—waiting reconciliation, CREATED admission, active/READY selection, external inference and proposal application—and startup `RecoverLostRuns`. A competing call returns `ErrWorkerCycleInProgress` immediately, without entering the database or issuing an external action. The guard uses Go 1.23 mutex `TryLock`, serializes only calls through the **same** Service object and never persists implicit execution authority.
- [x] Migrated SQLite/one-connection integration regression simulates a still-active model step by retaining the cycle lock while issuing concurrent `Tick` and `RecoverLostRuns` calls. Both must return the explicit busy error promptly with zero results/recoveries; original running Task/Attempt/Worker and step count remain unchanged. Once the guard releases, startup recovery must interrupt and audit the uncertain Attempt once with exactly one outbox recovery signal, remaining idempotent on repeat calls.
- [ ] Validate combined source/test/docs exact-head hosted CI. **Cross-process** scheduling/recovery, unexpected daemon restart in mid-inference, multiple independent instances using the same SQLite database and actual model/tool side-effect tracing remain independent security acceptance gates. This per-Service mutex is not a distributed ownership lease and cannot prove external execution did not occur.
- [ ] Read-only follow-up review: daemon bootstrap currently fails startup when `RecoverLostRuns` returns any unrecoverable corrupt/unknown-outcome run error. Before changing that fail-closed contract, design an operator-only safe recovery mode; never silently resume uncertain Tasks or discard failed recovery diagnostics to make the UI start.
- [ ] Final integrated code/security audit, actual low-memory Colibri CPU/GPU model queuing, independent physical Node pairing/permissions, L5 consent and verified reversible effects, Windows/Ubuntu installed data-drive QA and **LAST** human-first UI/UX review remain blocking. Keep PR #227 draft; do not merge/package/release or modify the known-good install.


## RC11 P0-EZ — configuration mutation fenced by scheduler-cycle ownership (2026-10-18)

- [x] `Service.SetConfig` previously validated then directly assigned `s.cfg` while an already-running `Tick` could concurrently compile context, route models or admit new Workers. Mid-step mutation of `ContextMaxBytes`, role, capability, protocol, replan/step limits or escalation thresholds was a data-race and a risk to deterministic model/tool execution provenance.
- [x] `SetConfig` now applies validated settings only after `workerCycleMu.TryLock`, returning `ErrWorkerCycleInProgress` without mutating settings when a scheduler/recovery cycle holds ownership. Invalid configs still reject without mutating prior good settings. There is no automatic mid-flight force-update, implicit privilege expansion, retry of uncertain inference, or persistence of a new grant.
- [x] Go unit test holds the execution-cycle mutex, verifies a concurrent settings update explicitly fails and leaves the previous config unchanged, then releases it and verifies the same valid update succeeds; invalid configs remain rejected afterward. Existing `Tick` and `RecoverLostRuns` cycle-boundary regression continues to confirm no second inference/recovery during uncertain external effects.
- [ ] Latest integration-head hosted CI and actual installed Node/concurrent UI/API configuration QA still required. This is an in-process ownership fence, **not** a cross-process distributed database lease, model unloader barrier or guarantee that cloud inference can be canceled after dispatch.
- [ ] Next pre-release audit should trace how daemon startup treats individual failed Agent Worker recoveries: current bootstrap intentionally aborts startup rather than silently resuming unknown external effects; any operator-only degraded mode needs a separate security design, explicit human review and tests. Final integrated security/code audit, real paired Nodes, physical Colibri/CPU/GPU residency, Level 5 verified consent/postconditions, installed Windows/Ubuntu QA and **LAST** UI/UX review still block packaging and merge.


## RC11 P0-FA — refuse automatic fallback when external dispatch outcome is unknown (2026-10-19)

- [x] `b0dc4ea`: Agent Worker no longer treats an UNKNOWN/EXECUTING/DISPATCHED inference request, unreadable successful inference response, or ambiguous external Agent-runtime invocation as an ordinary model failure eligible for immediate automatic fallback. A documented `ErrDispatchRequiresReconciliation` discriminator preserves the original error chain; known terminal inference failures and provider rate limits retain their existing, separately policy-governed fallback behavior.
- [x] Unknown or response-unreadable dispatches keep potentially spent model/Agent quota reservations for reconciliation instead of releasing them and silently issuing a second candidate call. No cloud, Node, sandbox or filesystem rights were granted.
- [x] Unit tests cover status classifications for both transports and a retained evidence/error sentinel. The first source commit passed full hosted CI [#39170644606](https://github.com/DigiLogicTech/OnePane/actions/runs/39170644606).
- [x] `043a662`: receipt and safety transitions now occur in **one** SQLite transaction: verify exact running Worker and Task revision, interrupt the original Task Attempt with durable recovery audit/outbox, mark the Worker interrupted and journal an UNKNOWN step. If any write fails or context is cancelled, roll back the whole transaction and poison that in-process Agent Worker scheduler until successful explicit `RecoverLostRuns` recovery. Return a truthful failure rather than reporting an uncommitted BLOCKED state.
- [x] `144d6be`: verify the **original active Attempt** ID before changing anything and fail closed when corrupt historic records contain a stale/mismatched or additional active Attempt; avoid persisting raw external transport error strings in the new UNKNOWN step journal. The record uses reason code and durable request/invocation reference, not sensitive error payloads.
- [x] Migrated, single-SQLite-connection integration regressions cover atomic interruption of Task/Attempt/Worker, UNKNOWN journal, one durable Attempt, cancelled transaction with stop-and-recover, and synthetic stale original Attempt with no partial mutation or scheduler replay.
- [ ] Full exact integration-head hosted CI for all follow-up code, tests and this manifest must complete before validation. Hosted regressions do **not** prove external remote side effects did not happen. No automatic replay/reconciliation of an UNKNOWN outcome is permitted.
- [ ] Wider pre-release audit still needs to examine generic `blockRun`/`failRun` paths, which currently treat Task and Worker transitions separately; the new uncertain-dispatch path deliberately does **not** reuse those helpers. Multi-process scheduler ownership and safe handling of malformed startup recoveries remain separate design/security tasks. Real constrained hardware and quota-metered provider testing, physical paired Nodes, authorised Level 5 verified actions, Windows/Ubuntu installed QA, security audit and the **LAST** human-centred UI/UX review are outstanding. PR #227 stays draft and unmerged; no MSI/release permitted.


## RC11 P0-FB–P0-FH — Worker terminal, wait and verified completion transaction safety (2026-10-19)

- [x] **P0-FB — ordinary BLOCKED/FAILED atomicity**, `147b087`: no more Task state commit followed by ignored Agent Worker completion error. New trusted `task.WorkerTerminalInTransaction` checks Task revision, original active Attempt ID, worker principal, sole active Attempt and legal Task/Attempt transition. The caller commits Task, Attempt, audit and Worker terminal state in one SQLite transaction. A failed write retains the previous state and halts scheduling on the owning Worker Service pending explicit `RecoverLostRuns`, rather than advertising a completed terminal transition. One-connection migrated SQLite tests cover successful block/fail, cancelled commit, invalid Task transition and stale Attempt. Full hosted CI passed [#39172053619](https://github.com/DigiLogicTech/OnePane/actions/runs/39172053619).
- [x] **P0-FC — pause/human WAIT atomicity**, `5e18310`: previous Worker-first waiting update could strand `agent_worker_runs.status='waiting'` while Task/Attempt failed to enter waiting. New trusted `task.WorkerWaitInTransaction` and `waitRunAtomically` bind original active Attempt and commit Task/Attempt/Worker plus schema-valid WAITING step journal atomically. Failed write poisons the local scheduler for safe startup-style recovery. SQLite regression covers human-approval wait, model-proposed pause, cancellation rollback and stale attempt. Full hosted CI passed [#39172230905](https://github.com/DigiLogicTech/OnePane/actions/runs/39172230905).
- [x] **P0-FD — model-proposed failure**, `029621e`: removes direct `Task.Fail` followed by unchecked `finishRun`, reusing the same trusted atomic terminal path. Full hosted CI passed [#39172297675](https://github.com/DigiLogicTech/OnePane/actions/runs/39172297675).
- [x] **P0-FE — V0 verified completion atomicity**, `da2ce3e`: refactors `task.CompleteVerified` to share a transaction-capable implementation, with stricter exact original Attempt for Agent Worker completions. V0 completion now commits Task completion, original Attempt success, Worker success and verification audit in one transaction. The independent valid-checkpoint/verification gate remains mandatory. Application-source hosted CI passed [#39172521498](https://github.com/DigiLogicTech/OnePane/actions/runs/39172521498).
- [x] **P0-FF — real verification regressions**, `ccf04d5`: migrated single-connection SQLite tests exercise V0 successful Task/Attempt/Worker/step/audit completion, cancelled completion with full rollback plus recovery, and a stale Attempt that cannot complete. These tests discovered an **existing strict SQLite result persistence bug**. Hosted CI [#39172593970](https://github.com/DigiLogicTech/OnePane/actions/runs/39172593970) failed specifically because `repository_sql.Transition` bound `[]byte` JSON into the strict TEXT `tasks.result_json` column. This failure is **not** to be misreported as passed.
- [x] **P0-FG — higher verification WAITING audit**, `a7da383`: prior higher-level verification path wrote unsupported `agent_worker_steps.status='queued'` and ignored the write error. A single guarded transaction now verifies the active Task/Attempt and records `RunBlocked` with schema-valid `verification/waiting` journal or halts on failed persistence. Tests cover successful parking, cancelled database context and stale Attempt. Hosted CI on the unfixed JSON result baseline also failed due to the above pre-existing BLOB/TEXT defect, not assurance code.
- [x] **P0-FH — strict SQLite completion-result fix**, `1afddc5`: `repository_sql.Transition` now binds JSON results as TEXT rather than BLOB while preserving SQL NULL semantics. A regression uses the existing public `Task.CompleteVerified` path to ensure a verified result round trips from strict SQLite. Exact-head hosted CI must complete before this corrected batch can be considered green: [#39172810622](https://github.com/DigiLogicTech/OnePane/actions/runs/39172810622).
- [ ] **Release gates remain:** do not confuse source/SQLite simulation CI with real CPU/RAM/VRAM/Colibri model unloading, external invocation uncertainty, physical trusted Node identity, approved independently verified Level 5 actions or Windows/Ubuntu installer preservation of non-C: data. Before packaging: full integrated code/security audit (including task verification and generic Worker status paths), multi-process scheduler lease and startup corrupt-run operator-safe recovery review, and the LAST user-centred UI/UX review. Explicit human approval is required for installer/merge. PR #227 remains draft/unmerged, installed known-good RC10 is unchanged.


## RC11 P0-FI–P0-FL — single-daemon ownership and restart recovery integrity (2026-10-19)

- [x] **P0-FI — data-root OS exclusivity**, `19db52c`: daemon acquires an exclusive OS-held lock at `<configured data_dir>/state/harnessd.instance.lock` **before** bootstrap, migration, any crash reconciliation, listeners or Worker loops; holds it for the full backend lifetime and releases on crash/exit. Linux/macOS use nonblocking `flock`, Windows uses nonblocking `LockFileEx`; no lease TTL, no PID staleness assumptions, no deletion/recreation of a live lock inode. Existing lock path must be a regular file and post-acquisition path must match the open handle. Offline `--startup-evidence-root` read-only mode does not take the lock; user-selected data directory rather than C: is respected. Unit regressions check exclusive ownership, independence across roots, stale-file reacquisition, unsafe path and independent helper process rejection. Full hosted source CI passed [#39173849672](https://github.com/DigiLogicTech/OnePane/actions/runs/39173849672).
- [x] **P0-FJ — exact restart Attempt and pending-verification preservation**, `0d6e10f`: `RecoverLostRuns` now checks the same exact original Worker Attempt and owner principal, active status, Task revision, Workspace binding and precisely one active Attempt under its recovery transaction before interrupting. Malformed ownership fails closed, allowing independent healthy Tasks to recover without mutating another Attempt. The orphan recovery predicate now treats a durable `blocked` Worker as its Attempt's legitimate owner: pending independent V1–V5 assurance and operator-blocked Tasks cannot be mistakenly marked orphaned on restart. Migrated SQLite regressions cover corrupt foreign Attempt principal, nonmutation and repair/retry, and pending verification remaining VERIFYING with WAITING original Attempt and BLOCKED Worker (no duplicate recovery receipt).
- [x] **P0-FK — stop the same Worker Tick on failed persistence**, `dcda419`: after `step()` raises the in-process reconciliation alarm, the existing Tick returns immediately with `ErrDispatchRequiresReconciliation` instead of admitting or dispatching other already-selected Tasks in the same scheduling cycle. This is in addition to the existing guard on future Tick calls.
- [x] **P0-FL — simulated abrupt process crash**, `eda8979` / `660d334`: tests launch an independent holder executable, verify a competing process receives the busy response, kill the holder without normal defers and demonstrate kernel-level ownership recovery without deleting a stale lockfile. Fixed helper to sleep rather than risk the Go runtime's all-goroutines-asleep panic.
- [ ] CI for post-`19db52c` integration changes and this exact manifest head must complete before declaring the batch verified green. Source-hosted Linux tests plus Windows cross-compilation do not prove Windows SCM lock semantics on a real Windows service, nor do they cover a third-party process that bypasses the harnessd startup lock by directly using internal libraries.
- [ ] **Pre-release remaining:** check all supported backend startup entrypoints for lock enforcement and fail-closed errors; real Windows SCM install/restart/relaunch, Ubuntu service crash recovery and chosen D: data directory preservation; single model slow inference under load; physical paired Nodes, Colibri, VRAM/RAM scheduling, independently approved Level 5 effects, final integrated code/security audit and LAST user-centred UI/UX review. PR #227 remains draft/unmerged. No release, MSI or live machine changes authorised.


## RC11 P0-FM — external Task resume requires original Worker dispatch identity (2026-10-19)

- [x] `524950d`: when an operator or another trusted Task control-plane path resumes a paused/waiting Task, the reconciler may change `agent_worker_runs.status` from `waiting` to `running` **only** using the canonical dispatch predicate: same original Attempt ID, Task/Workspace, active system Worker principal, exactly one active Attempt, valid `agent_worker=v1` metadata and still-active canonical Project/Workspace. A malformed legacy half-resume (RUNNING Task with WAITING Attempt or foreign principal) is reported as an explicit Worker reconciliation error instead of silently returning success and leaving the Task forever stranded.
- [x] Migrated SQLite regressions exercise malformed legacy half-resume, no implicit second Attempt, successful repair and exact-owner wake, and an Attempt with a different Worker principal. No extra cloud calls, device operations or expanded permissions.
- [x] Previous exact checkpoint `4dec6f3` full hosted CI passed [#39174363441](https://github.com/DigiLogicTech/OnePane/actions/runs/39174363441). This new code's exact-head CI [#39174596487](https://github.com/DigiLogicTech/OnePane/actions/runs/39174596487) must pass before closing the batch.
- [ ] Remaining gates are unchanged: real Windows service-exclusive launch and recovery test, physical Ubuntu/Windows constrained models and Colibri, physical Node/Level 5 approvals, integrated security/code audit and the LAST human-centred UI/UX review; draft PR unmerged and no installer authorised.


## RC11 P0-FN — native Windows ownership, SQLite and diagnostics CI gates (2026-10-19)

- [x] `fed4a197`: extended the existing RC11 Windows hosted job to execute `internal/processlock` tests **natively** using `LockFileEx`, including independent-process refusal, forced child-process crash, stale lock-file reacquisition and nonregular lock-path denial. Added `internal/processlock/**` to the PR workflow path filter. Earlier CI run [#39175365244](https://github.com/DigiLogicTech/OnePane/actions/runs/39175365244) completed the Windows job successfully; the full source job is independently reported by that run.
- [x] `96f1b610`: added a Windows-native `internal/storage/sqlite` and `internal/startupevidence` regression step, in addition to Linux/Go cross-compilation, to exercise actual hosted Windows SQLite and filesystem behaviour. [Full exact-code CI #39175481373](https://github.com/DigiLogicTech/OnePane/actions/runs/39175481373) requires a confirmed conclusion before marking this source/test batch green.
- [ ] Native hosted Windows unit checks **do not** establish actual SCM service ownership, an installed RC10-to-RC11 populated upgrade, non-C: persistence or reboot/crash semantics on the operator's hardware. Retain those release-blocking real-environment acceptance cases, along with physical paired-Node/Colibri and L5 effect verification, final integrated code/security audit and **LAST** human-centred UI/UX review.
- [ ] Keep PR #227 draft/unmerged and make no RC11 installer, release, remote-node or live-device changes without explicit approval.


## RC11 P0-FO — Windows migration privacy and installer storage-root integrity (2026-10-19)

- [x] The new native Windows storage test in `96f1b610` exposed the existing `TestCreateMigrationBackupProducesConsistentCopy` Unix-only mode assumption: [#39175481373](https://github.com/DigiLogicTech/OnePane/actions/runs/39175481373) failed on Windows with backup `Mode().Perm()=0666`. This is a **real portability and security finding**, not a successful CI gate or an excuse to disable the test.
- [x] **P0-FO1**, `2553d4aa`: replaced reliance on `os.Chmod(0600)` for SQLite migration snapshots and restore copies with Windows-native protected DACL application (exact current service SID, SYSTEM, local Administrators) and negative Windows tests rejecting broad `Everyone` access. Kept Unix 0600 checks. [Full hosted source CI #39175839497 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39175839497), including the actual Windows SQLite/privacy tests, Go regression and cross-platform compilation. No live operator data or installer files were modified.
- [x] **P0-FO2**, `d339126a` and `cbec8bda`: Windows service now treats an explicitly present but invalid/unreadable MSI-selected `DataRoot` as an actionable launch error rather than silently inventing a C: data root; startup evidence and startup-error logging use the same fail-closed selection. Fixed the additional SCM startup-error logging call identified by the new Windows compile test. Windows native tests prove selected D: path and invalid relative/empty path behaviour without altering HKLM. The initial `d339126a` Windows job [#39176167961](https://github.com/DigiLogicTech/OnePane/actions/runs/39176167961) failed on the missed call; the corrected `cbec8bda` Windows job [#39176289435](https://github.com/DigiLogicTech/OnePane/actions/runs/39176289435) passed.
- [x] **P0-FO3**, `eb2e5db5`: extended typed Windows installer registry-path lookup and present-versus-absent fail-closed handling to `ProjectRoot` and `ModelRoot`. Missing legacy registry settings still derive paths under the chosen data root; explicitly invalid paths or registry access errors cannot silently pick unrelated model/project storage. Windows test job [#39176385964](https://github.com/DigiLogicTech/OnePane/actions/runs/39176385964) passed; the remaining exact-code Linux source job must finish before the consolidated head can be called fully green.
- [ ] **Remaining security review:** verify NTFS parent directory access and short creation-time ACL exposure during `VACUUM INTO`; inherited permissions on pre-existing config/models/projects; symlink/reparse and Windows service-identity permissions; ensure an existing populated legacy `config.yaml` does not reference an unintended prior drive. Do not claim these have been fully addressed by hosted tests.
- [ ] **Mandatory physical acceptance:** Windows SCM upgrade/reboot/crash, selected D: drive preservation and rollback from populated RC10, Ubuntu service/migration, constrained CPU/Colibri model/SSD/VRAM behaviour, independently paired Nodes and human-approved bounded Level 5 device effects/postconditions. Final integrated code/security audit and **LAST** user-first UI/UX review precede an explicitly approved installer. PR #227 stays draft and unmerged; no package/release performed.


## RC11 P0-FP — private snapshot created before SQLite backup writes (2026-10-19)

- [x] Reviewed the authoritative SQLite `VACUUM INTO` contract: the destination must be nonexistent **or an empty existing file**. This permits ownership/ACL enforcement on a unique, empty destination before any private database rows are written (https://www.sqlite.org/lang_vacuum.html).
- [x] **P0-FP**, `37b34b2f`: `createMigrationBackup` now exclusively pre-creates an empty destination, protects it with the existing Windows DACL / Unix 0600 checks, closes its handle, then calls `VACUUM INTO`. After SQLite writes, it verifies that the same filesystem object remains, checks private permissions again and removes incomplete output on any error. Existing migrated SQLite tests now exercise this real call path on Linux and Windows, not a mock.
- [ ] [Exact-code full hosted CI #39176629652](https://github.com/DigiLogicTech/OnePane/actions/runs/39176629652) must complete successfully before this new source head is considered accepted. Windows native and Linux source results must each be checked; a skipped opt-in physical Node job is **not** hardware acceptance.
- [ ] These file permissions do not replace NTFS parent-directory ACL review, reparse/symlink race review, selected D: populated-service upgrade/rollback testing, nor independent physical Node/Colibri/Level-5 security sign-off. Final integrated security/code audit, LAST human-first UI/UX and explicit installer approval remain required. No release, merge or local-device operation authorised.


## RC11 P0-FQ — Windows service first-run configuration privacy (2026-10-19)

- [x] **First-run confidentiality:** `7ca9053e` adds a dedicated native Windows secure-config creation path. New `config.yaml` is opened exclusively and receives a protected current-service/SYSTEM/Administrators DACL **before** any generated YAML is written. An existing normal configuration is retained byte-for-byte; an existing symlink or nonregular path is refused rather than followed. The caller verifies that the same opened file still occupies the expected name before the first write. This does **not** rewrite or silently migrate any populated legacy config.
- [x] **Adversarial Windows regression:** native tests exercise repeat initialisation without clobbering user config, a directory in place of `config.yaml`, a symlink substitution where supported by the test runner, explicit broad `Everyone` ACL rejection, and successful protected private creation.
- [x] **Real Windows defect from initial CI:** `7ca9053e` attempted handle-based `SetSecurityInfo` with an ordinary Go file handle lacking `WRITE_DAC`; native Windows [CI #39183317307](https://github.com/DigiLogicTech/OnePane/actions/runs/39183317307) correctly failed with `Access is denied`. `daf93f22` / `fc22d696` switch to the already-tested named-object Windows ACL API, with file identity checked before private content is written; do not claim the first attempt passed.
- [x] **Fail-closed cleanup and recovery:** `d6282724` closes the Windows writer before removing an incomplete config; `f0ccbce7` / `24a33b81` add deterministic ACL-denial injection and validate the partially created file is removed and can be safely recreated. An ACL error cannot be reported as successful creation or leave a deliberately empty first-run config under normal filesystem conditions. The original failure remains visible to service startup diagnostics.
- [ ] **Exact-code verification:** Windows-native service/security regressions and the full Linux/integration Go suite must be green on [latest source CI #39183585313](https://github.com/DigiLogicTech/OnePane/actions/runs/39183585313) before declaring the batch verified. The older [CI #39183317307](https://github.com/DigiLogicTech/OnePane/actions/runs/39183317307) failed and is not an acceptance result.
- [ ] **Outstanding release blockers:** audit existing populated `config.yaml` ACLs without unexpectedly revoking operator access; inspect NTFS ancestor/reparse directories, inherited logs, service identity and data-root migration; physically verify SCM restart/crash and D: installation with models/projects and rollback. Unchanged broader gates: CPU/Colibri constrained acceptance, paired physical Nodes, bounded approved L5 effects, final integrated security/code audit and LAST operator-first UI/UX. No MSI, branch merge, off-host action or remote access is authorised.


## RC11 P0-FR — per-launch authenticated Windows SCM readiness (2026-10-19)

- [x] **Service-readiness integrity:** previous Windows SCM wrapper accepted any HTTP 2xx from `127.0.0.1:18181/v1/health`, even if another process had bound the same port. `54b9536f` adds a service-only `/__onepane/service/ready` loopback handler gated by the same ephemeral per-launch service token used for authenticated graceful stop. Ordinary non-service backend launches do not expose privileged endpoints. Windows SCM no longer trusts the public health endpoint, disallows redirected replies and checks for a still-live child after a readiness response. Token state is protected by the service's child mutex.
- [x] **Native Windows test coverage:** `3c3f19e3` adds Windows service-probe regression testing for an unrelated HTTP 200 listener, an older service token, a wrong status or redirect; it also runs `cmd/harnessd` service-control tests natively on hosted Windows, not only cross-compiling the backend. It does not run an installed SCM service.
- [x] **Impersonation hardening:** code review discovered that even a secret-bearing request to a listener which blindly returned 204 would have been accepted. `0e301a1f` adds a fresh random 32-byte challenge per readiness attempt and validates a SHA-256 HMAC response against the unique 32-byte launch secret. Shared `internal/serviceproof` code enforces secret/nonce lengths, hex decoding, and constant-time MAC verification. Backend tests validate proof generation, rejection of stale/forged proof, a missing/invalid challenge, request method and remote loopback requirements. Native Windows SCM tests explicitly reject unrelated blind HTTP 204, forged response, previous service proof, unauthenticated 200 and redirects while accepting only the matching response.
- [x] **Hosted Windows regression:** code CI [#39201424959](https://github.com/DigiLogicTech/OnePane/actions/runs/39201424959) completed its Windows-native instance lock, service-control HMAC probe, SQLite migration and offline MSI diagnostics job successfully.
- [x] **Full source gate passed:** [#39201424959](https://github.com/DigiLogicTech/OnePane/actions/runs/39201424959) completed BOTH native Windows security/regression and Linux/integration Go and cross-platform compilation jobs successfully for source commit `0e301a1f`. The physical Node approval-status job was intentionally skipped, not a hardware acceptance result.
- [ ] **Remaining physical acceptance and release gates:** test real Windows SCM service launch, port collision, process crash/reboot, and preservation of populated user-selected D: `DataRoot`, `ProjectRoot` and `ModelRoot` through upgrade and rollback; assess inherited legacy config and ancestor NTFS ACLs before migration. Physical constrained CPU/GPU/Colibri tests, authorised paired Nodes and safe bounded Level 5 effects, integrated code/security review and **LAST operator-first UI/UX** remain mandatory before any installer approval. PR #227 stays draft/unmerged. No MSI, device action, cloud fallback change or privilege expansion performed.


### RC11 P0-FR verified branch continuation (2026-10-19)

- [x] The exact HMAC-readiness code `0e301a1f` passed both the native Windows and complete Linux/integration GitHub Actions jobs [#39201424959](https://github.com/DigiLogicTech/OnePane/actions/runs/39201424959). This includes the forged/blind 204 and stale service-proofs negative tests.
- [x] The follow-up workflow `b6ac035b` adds `internal/serviceproof/**` to PR path filters and runs the shared proof package explicitly in the Windows-native job. **Full exact-code workflow CI passed** [#39201723814](https://github.com/DigiLogicTech/OnePane/actions/runs/39201723814), including both required jobs; the opt-in physical Node acceptance job was skipped intentionally.
- [ ] This remains source/hosted OS assurance, **not** installed SCM/port collision acceptance, D: persistent-data upgrade/rollback testing, physical Node/Colibri qualification, safe authorised Level 5 operations, final security/code review, or the last operator-first UI/UX gate. Leave draft PR #227 unmerged; no installer or release.


## RC11 P0-FS — Windows SCM bearer removal and explicit data-root confinement (2026-10-19)

- [x] **P0-FS1 critical protocol correction**, `6c635484`: follow-up review identified the old service-ready HMAC design still transmitted the per-launch secret in `X-OnePane-Service-Token` **before proving who held the HTTP port**, defeating port-hijack secrecy. The Windows SCM probe now sends only a random 32-byte challenge, and the backend returns an HMAC signed using the secret it received through its launch environment. Graceful shutdown no longer sends any bearer token either: a fresh nonce and separately domain-bound `SignShutdown`/ `VerifyShutdown` HMAC are required. Readiness proofs cannot authorise shutdown. The service refuses redirects for its shutdown request. Shared `internal/serviceproof` and native Windows + backend tests cover forged, old, wrong-purpose proofs, missing nonce, unauthorised shutdown, fake HTTP 204 and captured request headers with no leaked token. **Full hosted CI passed [#39202902605](https://github.com/DigiLogicTech/OnePane/actions/runs/39202902605)** (native Windows and Linux/integration Go).
- [x] **P0-FS2 existing-config storage identity**, `0d061f3e`: an existing `config.yaml` is read-only checked against the MSI-explicitly-selected `DataRoot`. If its `storage.data_dir` is missing, relative, stale or points at a different directory (e.g., copied from `C:\ProgramData\OnePane\data` to a selected D: root), the service refuses to start another unintended database instead of silently redirecting state. It does **not** rewrite, move or delete that YAML or user data, and it leaves legacy systems without an explicit DataRoot registry value unchanged. Windows tests cover correct root retention, mismatches, invalid YAML, directory substitution, and exact byte preservation. **Full hosted CI passed [#39203081940](https://github.com/DigiLogicTech/OnePane/actions/runs/39203081940)**.
- [x] **P0-FS3 explicit-root path inspection**, `7234232a`: Windows service validates existing path components of an MSI-selected DataRoot before writing configuration, logging or SQLite, rejecting existing non-directory, symlink and NTFS reparse/junction ancestors. The check is read-only, restricted to explicit DataRoot choices (legacy absent-value installations retain their old behaviour), and includes Windows-native negative tests with symlink coverage when runner permissions permit it. The inspect-versus-open filesystem race and other reparse behaviours still require physically installed Windows testing; this is not a hard filesystem sandbox.
- [x] **Exact-code CI for FS3 passed:** [#39203443043](https://github.com/DigiLogicTech/OnePane/actions/runs/39203443043) successfully completed both native Windows and Linux source/integration jobs for the explicit NTFS root-reparse guard at `7234232a`. Earlier FS1 and FS2 application code independently passed #39202902605 and #39203081940. The physical Node job remained intentionally skipped, not accepted.
- [ ] **Hard release gates unchanged:** assess legacy existing config file/ancestor directory ACLs without permission-breaking rewrites; explicitly reconcile legitimate user-customised project/model paths with MSI registry values; verify a **populated D:** Windows SCM install, port collision, recovery/reboot and rollback with no user-data loss. Physical constrained local inference/Colibri, independently paired Nodes, approved bounded reversible Level 5 operations, integrated code/security audit and **LAST operator-first UI/UX** remain pending. PR #227 is draft/unmerged; no installer, release or physical device operation authorised.


## RC11 P0-FT — Windows service log confidentiality and rotation (2026-10-19)

- [x] **P0-FT1**, `dc9c1043`: second-pass Windows service security audit found service and backend logs created using `os.OpenFile(...,0644)`, which does **not** enforce an NTFS private ACL, and error-logging used a separate unprotected append path. The bounded log writer now opens a regular file only (refuses existing nonregular/symlink path), applies the same protected current-service/SYSTEM/Administrators DACL as new service configuration **before any log content is appended**, checks that the named file matches its opened handle, and uses the same private writer for pre-HTTP startup errors. Rotation closes the old file, preserves its contents at `.1` with its protected ACL and secures the replacement before writing.
- [x] **P0-FT2**, `b26e9009` + `83ae674f`: protect an already-existing `.1` diagnostic log on startup, even if it inherited broad permissions under earlier versions. It is not emptied or overwritten. Native Windows tests cover repair of intentionally broad Everyone ACL on an existing log, preservation of prior log bytes, private rotated/new files, private existing `.1` history and refusal of a directory log target. This changes permissions only on service-owned diagnostic files, not operator Project/Model data or existing configuration.
- [x] **CI and Windows remediation:** early source [#39204033595](https://github.com/DigiLogicTech/OnePane/actions/runs/39204033595) and historical-log [#39204120667](https://github.com/DigiLogicTech/OnePane/actions/runs/39204120667) both failed native Windows service-log tests: append-only Go file handles could not perform handle-based NTFS DACL verification (`Access is denied`). `34fec781` changes security-descriptor verification to Windows named-object inspection, preserving the required same-file identity checks before writes. The corrected code passed **both native Windows and Linux/integration CI** [#39204331543](https://github.com/DigiLogicTech/OnePane/actions/runs/39204331543), including the service/rotated-log history and ACL regressions. The physical Node job was intentionally skipped, not hardware acceptance.
- [ ] **Release blockers:** review historical log files beyond `onepane.log.1`, inherited ACLs on legacy configuration, NTFS parent-directory and reparse time-of-check/time-of-use races. Verify actual installed Windows SCM with a populated D: data root, upgrades/reboot/rollback, constrained CPU/Colibri, independently paired physical Nodes, bounded human-approved L5 postconditions, integrated code/security audit and LAST operator-first UI/UX. No installer/release, merge, cloud permission expansion or live device change was performed. PR #227 remains draft/unmerged.


## RC11 P0-FU — bounded Windows diagnostics and explicit storage-root validation (2026-10-19)

- [x] **FU1 — bound a single large streamed log write**, `fd597cdd`: `boundedLogWriter.Write` now splits a write larger than the 16 MiB segment limit into bounded chunks and rotates as needed. Its Windows-native regression writes a single payload larger than two entire segments, verifies each of the current and one rotated file stays at or below 16 MiB, the latest bytes remain intact and both files retain protected ACLs. Empty writes do not rotate. [Exact-code hosted Windows and Linux CI #39207642729 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39207642729).
- [x] **FU2 — local drive-letter storage roots**, `44a85140`: Windows installer-selected DataRoot, ProjectRoot and ModelRoot reject UNC/network shares and extended/device namespace paths that `filepath.IsAbs` alone would allow. Valid local drive-letter choices remain supported. Explicit ModelRoot and ProjectRoot selections receive the same read-only path-component and NTFS reparse/junction ancestry checks previously applied to an explicit DataRoot. [Exact-code hosted Windows and Linux CI #39207869336 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39207869336). No actual operator filesystem was changed.
- [x] **FU3 — validate registry paths on retained configurations**, `c84c74dc`: moved registry ProjectRoot/ModelRoot resolution and explicit ancestry checks ahead of the existing `config.yaml` early return. A populated legacy config is not overwritten, but cannot bypass an unsafe installer-selected model/project location. Native tests cover normal temp-root paths, legacy absent keys, file-ancestor refusal and UNC refusal. [Exact-code hosted Windows and Linux CI #39208026957 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39208026957).
- [x] **FU4 — restore logging after simulated Windows rotation rename failure**, `e2df27a0`: `rotateWith` supports deterministic fault injection for the platform rename operation, leaves the active log open if deletion of the old archive fails, and safely reopens and re-identifies the same protected active log if a rename fails after close. Tests check the original bytes survive and subsequent appends work after a simulated sharing violation.
- [x] **FU4 exact-code CI verified:** [#39208316331](https://github.com/DigiLogicTech/OnePane/actions/runs/39208316331) passed BOTH native Windows service/log tests and the complete Linux/integration Go and cross-platform compilation jobs for `e2df27a0`. The physical Node job remained intentionally skipped; no hardware acceptance is implied.
- [ ] **Remaining gates:** real installed Windows SCM on populated selected D: including non-destructive upgrade/recovery/rollback and log handles inherited by child processes; ancestor NTFS ACL and reparse TOCTOU review, historical log privacy and genuine Project/Model custom-folder reconciliation; Linux installed service and constrained local model/Colibri behaviour; authenticated physical paired Nodes, human-approved bounded reversible Level 5 operations, final integrated code/security review and **LAST operator-first UI/UX** before any explicitly authorised installer. PR #227 remains draft/unmerged. No MSI, release, external effects or cloud privilege changes.


## RC11 P0-FV — local-first initial Windows service and child secret inheritance (2026-10-19)

- [x] **P0-FV1**, `d90c8951`: a brand-new MSI-managed Windows `config.yaml` now starts with `node_federation.enabled=false` and `node_federation.discovery_enabled=false`, in addition to its existing loopback-only HTTP and Preview binds. `config.Default()` remains unchanged for unrelated callers. This avoids automatically opening the Node federation listener or emitting multicast discovery before the operator opts in. **Existing config.yaml is retained byte-for-byte**, including any previously enabled Nodes; no migration or network action is attempted. Native Windows tests check localhost-only endpoints, disabled federation/discovery, explicit storage paths and config validation.
- [x] **P0-FV2**, `c4e4247e`: the ephemeral SCM service-control HMAC secret arrives in the backend environment for one process launch. The backend now consumes and unsets `ONEPANE_SERVICE_SHUTDOWN_TOKEN` immediately after flag parsing, before config/bootstrap, so normal model/runtime/tool subprocesses do not inherit the secret through `os.Environ()`. The handler retains a private Go variable for authenticated service readiness and shutdown. A unit test asserts the secret is captured exactly once and the environment variable is gone. This improves worker isolation but does not eliminate same-identity Windows process inspection or physical SCM security acceptance requirements.
- [x] **FV1 exact-code CI passed:** [#39208772958](https://github.com/DigiLogicTech/OnePane/actions/runs/39208772958) successfully completed hosted Windows-native and Linux integration/cross-platform Go jobs for the fresh-Windows local-first default. Physical Node job intentionally skipped.
- [x] **FV2 exact-code CI passed:** [#39208940739](https://github.com/DigiLogicTech/OnePane/actions/runs/39208940739) independently passed hosted Windows-native and Linux integration/cross-platform Go jobs for consumed/unset SCM environment secret. Physical Node job intentionally skipped, not hardware acceptance.
- [ ] **Remaining release/security gates unchanged:** installed Windows SCM launch and kill/restart/port-collision, populated D: upgrade/reboot/rollback and configured storage ACLs, bounded logging under inherited child handles, actual physical CPU/GPU/Colibri and paired Nodes, independently approved safe Level 5 effects/postconditions, final integrated security/code review and **LAST operator-first UI/UX** before an explicitly approved installer. Windows default federation opt-in needs product/UX confirmation in that final review. PR #227 stays draft/unmerged, with no installer, release, live remote device operation or cloud permission expansion.

- [x] **FU5 — post-rename replacement failure handling**, `d5c1c3de`: if rotation successfully renames the active log to `.1` but creating a private replacement fails, the writer now rolls back the exact original file to its original path **only when that path remains vacant**, revalidates its file identity and protected ACL, and permits subsequent appends. If a different file has appeared, it fails closed rather than overwrite someone else's content. Windows-native fault-injection tests cover both recovery and refusal to overwrite an unexpected new path.
- [x] **FU5 exact-code CI verified:** [#39209551784](https://github.com/DigiLogicTech/OnePane/actions/runs/39209551784) completed BOTH Windows-native tests and the full Linux/integration source suite successfully for `d5c1c3de`. The physical Node approval-status job was intentionally skipped. This is hosted fault-injection assurance, not proof of installed SCM behaviour or of a populated D: upgrade.


## RC11 P0-FW — Windows SCM failed-start lifecycle and selected storage identity (2026-10-19)

- [x] **FW1 — drain failed-start backend before SCM STOPPED**, `30050aa4`: the Windows service no longer reports STOPPED on readiness failure without attempting bounded management of an already-launched harness. A 5-second grace period gives the authenticated shutdown route a chance to unload models; then managed tree termination is requested and a bounded final reap wait records if the child still cannot exit. Normal stop also waits briefly after forced termination for the owned Windows Job Object/Wait path. Native Windows unit tests simulate prompt graceful completion, forced completion and an unresponsive child. [Hosted Windows + Linux CI #39213387166 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39213387166). This does not represent installed SCM or physical model-worker acceptance.
- [x] **FW2 — reconcile selected Model/Project locations with retained config**, `87f0bfe5`: an existing YAML must match each installer-explicit DataRoot, ProjectRoot and ModelRoot before Windows service startup; the MSI registry itself does not move Project libraries or model weights. Missing, relative, copied-from-C: or contradictory YAML is rejected with a reconciliation/migration explanation rather than silently starting a second empty instance. Windows tests cover matching choices, each independent mismatch, absent legacy registry selections, invalid YAML and byte-for-byte preservation of existing config. [Hosted Windows + Linux CI #39213544507 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39213544507).
- [x] **FW3 — clean unexpected backend exit must be observable**, `fec9e7fa`: if the managed backend unexpectedly exits without an SCM stop request, the wrapper now marks a service failure even if process exit code was zero. Explicit SCM-initiated stops still report a clean stop. A native Windows regression asserts both classifications. The failure code alone is **not** proof that Windows Recovery Actions are configured or that SCM will actually restart a crashed service. [Hosted Windows + Linux CI #39213664839 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39213664839).
- [x] **FW4 — fence launches canceled during bootstrap**, `20cf9e3d`: the SCM-owned `context.Context` is carried into `runBackendWithContext` and `exec.CommandContext`. A stop/readiness timeout before `cmd.Start()` cancels startup so a service cannot create a fresh backend after SCM has declared startup failed. For a launched child the graceful path remains first, with context cancellation and a tree kill after the bounded drain deadline. Native Windows tests assert a pre-cancelled or nil startup context cannot proceed. This covers the earlier gap where checking `child.Process != nil` alone missed the prelaunch phase.
- [x] **FW4 full CI verified:** [#39214158485](https://github.com/DigiLogicTech/OnePane/actions/runs/39214158485) completed both hosted Windows-native tests and the full Linux/integration regression and Windows cross-compilation for `20cf9e3d`. The physical Node job remained intentionally skipped; no physical acceptance is claimed.
- [ ] **Remaining blockers:** physical installed Windows SCM startup/stop/reboot/health/port collision, service failure recovery configuration, populated local D: upgrade/rollback with database+models+Projects and permissions preserved; race-safe NTFS ancestry and existing YAML ACL evaluation; realistic constrained hardware/Colibri; paired real Nodes and human-approved bounded L5 postconditions; final integrated security/code review and **LAST operator-first UI/UX**, then explicit installer authorisation. All checks so far are hosted/source-level only; PR #227 remains draft/unmerged. No MSI, physical action, cloud expansion or release merge.

- [x] **FW5 — exercise actual Windows Job Object termination on a hosted runner**, `4a139f50` and `89ac92f5`: a native Windows test starts a bounded helper instance of the Go test process, assigns it to OnePane's `KILL_ON_JOB_CLOSE` ownership job, closes the job, and asserts the child is reaped without waiting for its 20-second simulated workload to finish. A second test covers invalid PID refusal and idempotent closure. The fixture's cleanup performs a single `Wait` and kills a remaining helper on failure, not an operator's process. This is **hosted OS process-tree evidence**, not a physical installed SCM test.
- [x] **FW5 exact-source CI passed:** corrected Job Object test head `89ac92f5` passed BOTH native Windows managed-child termination/SCM regressions and Linux full Go/integration/cross-compilation in [#39214626204](https://github.com/DigiLogicTech/OnePane/actions/runs/39214626204). The earlier first-pass test is superseded; no installed Windows SCM or physical Node was exercised (physical status job intentionally skipped).


## RC11 P0-FX — managed local inference credential confinement and native Windows log privacy (2026-10-20)

- [x] **P0-FX1 — empty runtime Env must not inherit service credentials**, `4a8c1816`: `OSProcessBackend.Start` previously constructed `Cmd.Env` using `append([]string(nil), spec.Env...)`; Go interprets the resulting nil `Env` as inherit-everything, including cloud/SCM credentials. It now always allocates a non-nil, explicit environment, even when `LaunchSpec.Env` is missing. A real Go test subprocess verifies that parent `OPENAI_API_KEY`, `AZURE_CLIENT_SECRET` and `ONEPANE_SERVICE_SHUTDOWN_TOKEN` do not reach a child launched with nil, empty, or explicitly small environment lists. The existing approved Colibri/llama environment allowlist remains functional.
- [x] **P0-FX2 — private model-runtime stdout/stderr on Windows**, `2c748e84`: `openManagedRuntimeLog` now applies and verifies a protected NTFS DACL permitting only current service identity, SYSTEM and Administrators before appending to new or existing runtime logs. It verifies path-to-open-handle identity and refuses unsafe replacements. It does not truncate existing diagnostic bytes and cleans up a failed newly-created log only when its identity still matches. POSIX remains on `0600` under a private `0700` log directory. Native Windows tests deliberately start with an Everyone-readable legacy file and confirm permission repair plus byte retention.
- [x] **P0-FX3 — protect older log archives before rotation**, `d4ba905c`: the old managed model `.previous` file is read-only inspected and protected on startup. An existing full runtime log also receives the private DACL before it is renamed to `.previous`, so archives from pre-fix Windows installations do not remain broadly readable. A Windows-native negative-path regression applies a broad Everyone ACL to a full legacy log, exercises real rotation and verifies the archive's protection and byte-preserving contents. This is not an arbitrary-file cleanup or a user-data migration.
- [x] **CI workflow repaired without reducing checks:** initial workflow-edit heads [#39322471827](https://github.com/DigiLogicTech/OnePane/actions/runs/39322471827) and [#39322651416](https://github.com/DigiLogicTech/OnePane/actions/runs/39322651416) failed before running jobs due to a malformed extra workflow footer. `ababaca5` removed the duplicate footer; `24c0bc82`/`0a6c92cf` fixed explicit build-constraint separation in the new platform-specific Go files. The hosted workflow now explicitly runs real Windows child-process and DACL regressions rather than claiming cross-compilation proves NTFS behavior.
- [x] **Native Windows FX validation:** [#39323208726](https://github.com/DigiLogicTech/OnePane/actions/runs/39323208726) passed the native Windows job, explicitly including `Native Windows managed-model environment and private-log regressions`, existing SCM/service tests and the offline MSI diagnostics fixture.
- [x] **Full exact-source validation passed:** [#39323208726](https://github.com/DigiLogicTech/OnePane/actions/runs/39323208726) completed BOTH the native Windows process/NTFS model-security regression job and the full Linux/integration Go/cross-platform compilation job for source commit `d4ba905c`. The optional physical Node approval-status job was deliberately skipped, not hardware acceptance.
- [ ] **Remaining acceptance gates:** real Windows SCM and populated operator-selected D: upgrade/reboot/rollback, protection of inherited parent directories and short ACL creation windows, real Colibri/llama RAM/VRAM/SSD hot-swap and slow-model queue throughput, independent paired physical Nodes, human-approved bounded Level 5 postconditions, final integrated code/security audit and LAST human-focused UI/UX before an explicit release/installer decision. Keep PR #227 draft and unmerged. No installer, model download, physical effects, broad cloud credentials or network privileges authorised.


## RC11 P0-FY — active inference lease fencing for direct model Stop (2026-10-20)

- [x] **P0-FY1 — Stop cannot evict a serving inference runtime**, `7db119d4`: direct `RuntimeSupervisor.Stop` previously skipped the active-request protection held by the idle reaper and Colibri eviction path. It now executes under the same `activityMu` and refuses a model with a positive active inference lease before persisting draining state or signalling any model process. It also refuses a persisted `busy` runtime with **no matching in-memory lease**, avoiding unsafe inference after daemon restart. Existing `stopIfIdleBeforeLocked` and `prepareColibriSwapLocked` call the lock-aware stop body directly rather than double-locking. No implicit kill/privilege or cloud-model routing change.
- [x] **P0-FY2 — residency/lease lock ordering**, `97e91ee0`, `0c70b5f6`: the public direct stop now acquires `residencyMu` before `activityMu`, in the same order as normal Acquire, model swap, and idle reaper. Failed Colibri hot-swap rollback already holds the residency lock, so a separate internal `stopWhileResidencyLocked` wrapper acquires only `activityMu` and avoids recursive deadlock. A deterministic SQLite-backed regression asserts safe rollback while holding residency ownership.
- [x] **P0-FY3 — real SQLite-backed safety regressions**, `6bcd35f4`, `97e91ee0`, `0c70b5f6`: tests deny Stop for active leases without mutating database revision or status, deny an unknown persisted busy state, permit a clean stop only after the last Release, and exercise stop-versus-in-flight lease and model-residency sequencing. These are in-process simulated inference admissions, not real CPU/VRAM process lifetimes or multi-process DB leases.
- [x] **P0-FY4 — preserve signal/identity audits through refactor**, `2a76e01e`: preexisting source-level regression tests inspected only public `Stop` and failed after safety logic moved into `stopWithActivityLock`. Updated both to inspect the actual signal implementation and continue enforcing revalidated PID identity before SIGKILL and orphaned state on failed signal. Earlier [CI #39329658310](https://github.com/DigiLogicTech/OnePane/actions/runs/39329658310), [#39329837179](https://github.com/DigiLogicTech/OnePane/actions/runs/39329837179), and [#39329980709](https://github.com/DigiLogicTech/OnePane/actions/runs/39329980709) failed those audit tests; they are not acceptance evidence.
- [x] **P0-FY source and race CI verified:** [#39330148076](https://github.com/DigiLogicTech/OnePane/actions/runs/39330148076) passed both native Windows and Linux full source/integration tests for guarded Stop and the updated PID-safety audit. An added bounded hosted Linux `-race` lane in `e91eb58b` separately checks SQLite-backed Stop-versus-lease and Colibri rollback locking. [#39330530875](https://github.com/DigiLogicTech/OnePane/actions/runs/39330530875) passed the new race lane, full Linux source/integration/cross-platform tests and native Windows source checks. Earlier workflow-head [#39330418039](https://github.com/DigiLogicTech/OnePane/actions/runs/39330418039) failed to schedule because the workflow edit was malformed; `e91eb58b` restored the known-good full test job and added the race steps without altering any live systems. The physical Node job was deliberately skipped, NOT accepted.
- [ ] **Remaining release gates:** independently observe live CPU/Colibri serving-lease refusals, hot-swap/idle-reaper queuing under memory pressure, Windows SCM and populated selected D: upgrade/reboot/rollback, physical paired Nodes and human-approved Level 5 postconditions, final integrated code/security review and mandatory LAST user-centred UI/UX review. PR #227 remains draft/unmerged; no installer, live model process or operator data was changed.


## RC11 P0-FZ — bounded explicit daemon teardown distinct from manual model Stop (2026-10-20)

- [x] **P0-FZ1 — deliberate daemon shutdown may reconcile remaining managed inference processes**, `da081b03`: after harnessd's HTTP servers have been asked to drain, explicit `RuntimeSupervisor.Shutdown` must not leak local model children simply because a request was still marked active/busy when the grace period expired. The public `Stop` and automatic eviction STILL refuse active or unexplained busy leases. Only the private `stopForDaemonShutdown` route takes `residencyMu` then `activityMu` and calls the existing guarded stop/signal implementation in teardown mode. Original PID-identity checks, status CAS, orphan-on-uncertain-signal and bounded graceful/forced process controls remain unchanged. After a **confirmed stopped** process, daemon-owned active lease counts are released; failure leaves them intact. This does not grant ordinary operators a new unsafe force-stop API.
- [x] **P0-FZ2 — SQLite-managed daemon cleanup regressions**, `df56d810`: tests prove an active `busy` runtime cannot be stopped manually but can be reconciled through explicit daemon `Shutdown` without retaining a false busy residency; an unknown legacy busy status likewise fails the normal operator stop but is processed by deliberate teardown. These fixture instances have no running OS PID; **real process identity and Colibri child cleanup on a physical daemon remain separate release gates**.
- [x] **P0-FZ3 — lease and Colibri-race hosted regression gate**, workflow `e91eb58b`: [#39330530875](https://github.com/DigiLogicTech/OnePane/actions/runs/39330530875) passed native Windows checks, both explicit Linux `-race` test selections, the full Linux regression and cross-platform compile. Earlier malformed workflow [#39330418039](https://github.com/DigiLogicTech/OnePane/actions/runs/39330418039) did not schedule jobs; the checked-in workflow has a single, restored full-Go job and no malformed suffix.
- [x] **P0-FZ4 daemon-teardown exact-source CI accepted after remediation:** initial [#39331181802](https://github.com/DigiLogicTech/OnePane/actions/runs/39331181802) failed two new SQLite tests because `Shutdown` still called the ordinary guarded `Stop`; the first `da081b03` refactor had added a teardown helper without wiring the call site. `ac8d9759` routes `Shutdown` through the explicitly private `stopForDaemonShutdown`. [#39331408546](https://github.com/DigiLogicTech/OnePane/actions/runs/39331408546) passed BOTH the hosted native Windows job and Linux race, SQLite, full Go regression and Windows cross-platform compilation. Physical Node approval was intentionally skipped; live GPU/Colibri worker eviction was NOT accepted.
- [ ] **Remaining gates unchanged:** real Windows/Ubuntu daemon lifecycle under active CPU/GPU/Colibri requests, in-flight task interruption/worker persistence, process descendants/VRAM release, selected non-C: populated install/rollback, independently paired physical Nodes and human-approved Level 5 effects, final integrated security/code review and **LAST human-focused UI/UX** before explicitly approved installer/merge. PR #227 stays draft/unmerged. No live model, external machine or persistent operator data was modified.


## RC11 P0-GA — Colibri cancellation compensation and durable managed-model launch ownership (2026-10-20)

- [x] **GA1 — cancellation-safe explicit Colibri hot-swap recovery**, `c723c116`: an explicit Colibri swap's failed-launch cleanup uses `context.WithoutCancel` plus an explicit 165-second overall bound instead of reusing the cancelled caller request. The post-failure resident query, uncertain replacement-process inspection, verified drain and optionally restored predecessor share that bounded cleanup context. Model cleanup cannot be skipped merely because a user request timed out after spawning a Python/engine process. On uncertain `Instance` database errors or a `starting` record without a persisted PID, rollback is *withheld*, not guessed. SQLite and cancellation tests check detached cleanup and failure-closed inspection; this is not proof of Colibri VRAM reclamation on physical hardware. [Exact source Windows + Linux CI #39375231795 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39375231795).
- [x] **GA2 — durable PID after failed/cancelled launch ownership write**, `8cce6a3d`, `2755ccf2`, `7da109b1`, `64dcc547`: `startLocked` now checks both the SQLite PID persistence error and exactly one affected row rather than ignoring a zero-row compare-and-set. A model already launched while SQLite admission fails must have its PID recorded as `orphaned` **before** attempting process inspection or signalling; the PID is bound to the exact original launch revision and CLI fingerprint. A fresh, bounded persistence context survives the canceled request; a separate cleanup window validates managed executable/model/port identity before any SIGTERM; a confirmed dead process can be CAS-finalised as `failed`, otherwise the orphaned PID remains visible and another launch is withheld. If the ownership record itself cannot be written or another generation owns the row, it fails closed with an operator-reconciliation error rather than overwriting someone else's launch.
- [x] **GA2 targeted SQLite regressions:** cancelled caller with a still-live unverified PID records `orphaned` without signalling; already-dead PID records `failed` without a signal; state conflict and stale generation do not clobber the newer row; model process inspection runs only after SQLite reflects its PID and orphaned status. Full source and platform test lanes are also included. [Exact source CI #39376238633 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39376238633). This is hosted/synthetic process-state evidence, not an installed model teardown or OS PID handle lifetime assertion.
- [x] **GA3 — refuse Colibri-to-Colibri swaps around stale live failed/stopped model processes**, `b9e9f88e`, `d8110b2e`: `colibriResidents` checks PID presence and current process-backend liveness when saved state is `failed` or `stopped`. A still-live process is an explicit admission blocker even when another healthy candidate could have been evicted; the operator's status view identifies its process as resident with a reconciliation warning. A recorded PID independently confirmed dead does not unnecessarily occupy a seat. Native Windows/SQLite and Linux tests verify that no model process receives a stop signal merely during admission. **Limit:** following a Windows daemon crash, the in-process OSProcessBackend may not know a live historical PID; this is NOT equivalent to physical Windows process enumeration/attestation. Keep physical Colibri/SCM acceptance open.
- [x] **GA4 — explicit CI contracts**, `a8c06579`, `e5c0dff5`: extended hosted Linux `-race` and native Windows Go steps for cancelled Colibri compensation, uncertain/stale PID binding and admission around failed/stopped-but-live Colibri processes. The existing full integration, cross-platform compilation and offline MSI evidence steps remain intact; no real model is downloaded, installed or started by this change.
- [x] **GA3/GA4 exact-code CI verified:** [#39376774814](https://github.com/DigiLogicTech/OnePane/actions/runs/39376774814) passed BOTH the native Windows runtime/Colibri process tests and Linux full regression, dedicated SQLite/concurrent race checks, and Windows cross-platform compilation for exact source/workflow head `e5c0dff5`. Initial [#39376624946](https://github.com/DigiLogicTech/OnePane/actions/runs/39376624946) failed because one new status test assumed a fixed `ActiveDeploymentIDs` order; `d8110b2e` corrected the test to sort IDs. The corrected intermediate [#39376669096](https://github.com/DigiLogicTech/OnePane/actions/runs/39376669096) also passed both jobs. The physical Node approval job remained intentionally skipped and is NOT hardware acceptance.
- [ ] **Hard pre-release gates unchanged:** physical CPU/GPU/Colibri SSD/RAM/VRAM process identity and hot swap under canceled inference; measured constrained-hardware queues; installed Windows SCM failure/reboot/port collision and populated operator-selected non-C: storage upgrade/rollback with library preservation; Ubuntu installed service; independently paired and authorised real Nodes; approved reversible Level 5 effects/postconditions; final integrated security/code review and mandatory **LAST operator-first UI/UX review**, then explicit installer/merge permission. PR #227 remains draft/unmerged. No physical devices, live models, cloud fallback privilege or operator files changed.


## RC11 P0-GB — Windows post-restart PID liveness and cross-model memory ownership (2026-10-20)

- [x] **GB1 — recover OS liveness of untracked Windows model PIDs**, `a166bbd9`: earlier `OSProcessBackend.Alive` returned false for every PID absent from the current daemon's private child map on Windows, so an older Colibri/llama engine still running after a backend crash/restart could be treated as dead and its RAM/VRAM reused. The new Windows-native probe uses `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` and `GetExitCodeProcess` to distinguish confirmed exit from still-active state; access denied or an ambiguous inspection error is treated conservatively as **occupied** rather than falsely free. An actual hosted Windows helper process not registered in the backend verifies live-untracked detection and exit after reap. **Identity remains separately denied for untracked PIDs**; a live PID alone NEVER authorizes reuse, termination or control. Linux's previously authenticated /proc/process checks remain unchanged. [Exact-code CI #39390078082 passed Windows and Linux](https://github.com/DigiLogicTech/OnePane/actions/runs/39390078082).
- [x] **GB2 — fail-closed RAM/VRAM admission for stale abnormal processes on any managed backend**, `fe1b74e9`: `startLocked` now invokes `refuseUnreconciledMemoryOwners` under the established residency lock, **before** any Colibri eviction/capacity allocation or child start. On the same Node, other `starting`, `draining`, `orphaned`, `failed` and `stopped` instances with live PIDs block new model admission until operator reconciliation. An unrecorded PID for an uncertain `starting`, `draining` or `orphaned` runtime likewise blocks admission rather than silently freeing memory. Confirmed-dead historical PIDs, unrelated Nodes and the target's own instance do not spuriously block. The snapshot is bounded (rejects over 512 abnormal entries) and closes its SQLite cursor before OS process queries. This is read-only prelaunch enforcement: no existing model is signalled or configuration overwritten. Native and Linux SQLite tests cover each status, unknown PID, dead PID and oversized registry. The live Windows PID detection from GB1 makes the protection meaningful after a Windows daemon restart.
- [x] **GB3 — native OS and race workflow coverage**, `25ec95d0`, `8ceba491`: the hosted Windows lane explicitly runs the real untracked child-PID/kernel test and model admission fixtures; Linux `-race` additionally runs the SQLite-backed memory ownership tests. The full Linux/integration suite, cross-platform builds and existing MSI evidence tests remain in place. These are hosted native-process tests, not a real operator machine.
- [x] **GB2/GB3 exact-source final CI VERIFIED:** [#39390405751](https://github.com/DigiLogicTech/OnePane/actions/runs/39390405751) passed both hosted native Windows process/SQLite tests and the full Linux regression, managed-model `-race` lane and cross-platform builds for source/workflow head `8ceba491`. Physical Node approval-status job intentionally skipped; none of these hosted tests replace installed-machine GPU/VRAM acceptance.
- [ ] **Critical constraints still open:** Windows untracked-PID **identity** remains untrusted after a crash: no automatic kill/reuse of a PID merely because it is live; require independent post-restart ownership attestation or safe operator reconciliation. Test the actual installed Windows SCM with populated operator-selected non-C drive through upgrade/reboot/rollback, real local llama/Colibri GPU child/process tree/VRAM reclamation and queued inference under memory pressure, Linux installed service, paired physical Nodes, opt-in and reversible Level 5 operations, final integrated code/security audit and mandatory LAST user-centred UI/UX review before any approved installer/merge. PR #227 remains draft/unmerged and no live operator machine or installed models were touched.

- [x] **GB4 — Unix permission-denied PID cannot be mistaken for freed memory**, `a5f7a0c1`: `OSProcessBackend.Alive` now interprets `signal(0)` yielding `EPERM` or another indeterminate error as evidence that a model process may still be present, not proof that an older model exited. Only `ESRCH` or Go's `os.ErrProcessDone` prove absence. This retains the original non-intrusive Linux PID lookup and never expands signal/kill authority. A cross-platform pure regression covers success, EPERM, unexpected errors, ESRCH and ProcessDone. It is relevant to constrained Linux Nodes running historical engines under a different service UID; it is not physical process-tree verification.
- [x] **GB4 exact-source CI passed:** [#39391288654](https://github.com/DigiLogicTech/OnePane/actions/runs/39391288654) completed BOTH hosted Windows-native and Linux full Go/integration, memory-admission race regressions and cross-compilation successfully for `a5f7a0c1`. Physical Node acceptance remained intentionally skipped; actual cross-UID model processes and VRAM reclamation on installed Linux systems remain unverified.

- [x] **GA5 — post-stop liveness recheck before restoring Colibri predecessor** (`1b5275fe`): when a failed replacement has a recorded PID, the rollback path refuses to infer reclaimed memory from a successful stop call alone. It checks process liveness again after the stop, and withholds predecessor restoration if the PID is still alive or the process backend is unavailable. This is a conservative host-process check, not physical GPU memory attestation. CI pending for this exact code change.


## RC11 P0-GC — ordinary chat Colibri admission shares verified hot-swap rollback (2026-10-21)

- [x] **GC1 — remove divergent chat-versus-explicit Colibri failure recovery**, `b620f8a9`: `RuntimeSupervisor.Acquire` previously called bare `startLocked`, which can evict the current healthy Colibri model before encountering a cancelled request, SQLite error, failed engine health or other launch failure; only the explicit `ColibriHotSwap` entrypoint had bounded, identity-verified compensation and optional predecessor restoration. The chat/worker admission path now detects Colibri via the deployment's resolved backend and calls the same private `colibriHotSwapLocked` under `residencyMu` before recording the inference busy lease. Ordinary llama.cpp admission still uses `startLocked`. No cloud routing, model download or new auto-kill authority was introduced.
- [x] **GC2 — one internal implementation for bounded Colibri rollback**, `f2a5c0e7`: explicit hot-swap and chat acquisition now use `colibriSwapOnNodeLocked`, which snapshots prior residency, calls the managed starter, inspects actual post-start residency under the correct context, verifies cleanup of a failed replacement, and attempts restoration only for a demonstrably evicted healthy predecessor. A restored predecessor does NOT misrepresent the replacement request as successful; uncertain PIDs or failed identity checks withhold rollback. All calls continue to hold the established residency lock; the real starter is injected only by a package-private Go test seam, not an operator API.
- [x] **GC3 — deterministic cancellation and unresolved-PID regressions**, `1af399ea`: SQLite fixture simulates a chat cancellation **after** the previous Colibri residency is set to stopped, then asserts predecessor restoration uses an independently bounded recovery context and preserves the original cancellation error. A separate test asserts an uncertain replacement in `starting` without a stored PID refuses to restart the predecessor. A source-contract regression ensures chat `Acquire` is wired into the identical guarded hot-swap function. No actual GPU/Colibri process or persistent operator data is used in these fixtures.
- [x] **GC4 — native target and race verification in workflow**, `7d5712ac`: hosted Windows validates the cancelled-chat and unknown-PID regressions explicitly. Hosted Linux `-race` additionally executes those and the shared-path contract, before full Go regression/integration and cross-platform compile. The previous Windows SCM, MSI synthetic evidence, SQL/WAL and Node-governance checks remain unchanged.
- [x] **GC exact source/workflow CI verified:** [#39473291227](https://github.com/DigiLogicTech/OnePane/actions/runs/39473291227) passed BOTH hosted native Windows process/SQLite/Colibri compensation tests and Linux dedicated inference-lease plus cancelled-chat rollback `-race` regressions, full Go regression, and Windows cross-compilation for exact source+workflow head `7d5712ac`. The earlier source-only [#39473110775](https://github.com/DigiLogicTech/OnePane/actions/runs/39473110775) is now superseded; physical Node approval/status remained skipped (not real hardware acceptance).
- [x] **GC5 — accurate restored-predecessor reporting**, `baeb598c`: when a failed Colibri replacement is successfully compensated, `ColibriSwapState` reports `status=failed` for the requested replacement, lists the predecessor in `active_deployment_ids`, clears the now-false `evicted_deployment_ids`, and includes an explicit operator explanation. The original replacement failure still returns an error, not a false success. The cancelled-chat SQLite regression verifies this returned state. This changes reporting, not process authority.
- [x] **GC5 exact-source CI verified:** [#39474160834](https://github.com/DigiLogicTech/OnePane/actions/runs/39474160834) passed BOTH native Windows managed-model/SCM/SQLite and Linux explicit inference/Colibri race tests, full Go regression/integration, and cross-platform compilation for `baeb598c`. This validates corrected returned residency metadata only; no real Colibri GPU, SSD expert-cache, or physical Node acceptance is implied.
- [ ] **Unresolved release gates:** actual CPU/GPU/Colibri requests and memory release under cancellation and pressure, physical Windows SCM crash/restart and populated chosen non-C: drive upgrade/reboot/rollback, Ubuntu service acceptance, authorised physical paired Nodes and bounded Level 5 effects, integrated code/security review and **LAST** human-focused UI/UX pass, then explicit installer/release authorisation. PR #227 stays draft/unmerged. No physical operator hardware, installed models, credentials or Projects changed.
