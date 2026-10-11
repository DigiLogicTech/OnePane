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
