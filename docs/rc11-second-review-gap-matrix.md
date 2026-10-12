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
- [ ] **Operational performance follow-up:** durable fairness/cursor for
  large sets of invalid source permits, full retention/compaction policy
  for historical review ledgers, resource-pressure prioritisation and
  installed Windows/Ubuntu CPU-only and paired-Node QA. Current source
  candidate prefetch is capped at 256 and can starve a healthy source
  beyond that prefix if the earlier candidates repeatedly fail; do not
  claim starvation-free recurring delivery.
