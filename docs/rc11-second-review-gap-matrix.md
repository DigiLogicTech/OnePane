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
- [ ] Wire the allowlist to **authoritative persisted Workspace/Agent transfer approval**, not to model output or an arbitrary request. Until then, legacy caller behaviour must remain conservative and no new off-host grant is issued.
- [ ] Verify endpoint trust for provider-backed localhost and Node transports before reclassifying them as host-local or trusted rather than cloud.
