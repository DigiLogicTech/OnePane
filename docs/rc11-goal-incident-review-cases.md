# RC11 — Operator-owned Level 5 incident evidence review cases

**Scope:** canonical human triage of one existing TCP reachability drift alert. This is a functional review-state milestone, **not** a verified security incident, same-device cross-signal correlation, autonomous response or privileged Node action.

- New append-retained `project_goal_incident_reviews` records tie an operator's explicit case to exactly one existing alert and canonical Project, Goal, Workspace and source; an alert can create at most one case. Goal mode must be Level 5; a human with Project write access and active tenant membership must open it.
- The case starts `pending_review` and may transition exactly once through revision CAS to `reviewed`, with one of three honest human decisions: `needs_investigation`, `insufficient_evidence` or `referred_out_of_band`. All decisions are immutable audited events and historical context, not device-health states.
- Scoped human-only GET/POST endpoints: `/v1/projects/{projectID}/continuous-goals/{goalID}/incident-reviews` and `POST .../incident-reviews/{reviewID}/review`. Reads use Project read authority; opening and completing require Project write. Agents may not self-open or dismiss cases.
- Review does not mutate the original TCP alert, return-to-baseline evidence, Goal health, monitoring schedule, Task graph, permission grants, or any actual device; `device_health=unverified` and `remediation_authorized=false` remain explicit.
- Real SQLite integration covers source-alert provenance, cross-Project denial, human-only decisions, duplicate/forged status rejection, CAS replay, immutable history and absence of Tasks.

**Next gates:** later cross-source correlation requires independently attested common target/instrument identity and user-approved acceptance rules. Approved reversible playbook execution requires distinct granular permits, safe rollback and independent postconditions. Endurance, physical Node and installed Windows/Ubuntu QA, final code audit and the last-step UI/UX review are still open. Draft PR #227 remains unreleased.

## Effective operator and observer-stop readiness (2026-10-15)

The non-executable Level 5 response-candidate readiness view now reports `incident_reviewer_inactive` when the human who historically reviewed the originating incident is no longer an active tenant member, separately from `proposing_operator_inactive`. It also reports `observer_pause_active` when the operator has paused unattended TCP observation through the independent durable stop latch. These are *additional blockers*, not eligibility checks that ever grant execution: every response candidate remains `state=blocked`, `execution_authorized=false`, without a mutation adapter. SQLite integration exercises pause/resume and reviewer suspension/reactivation, checking the original proposed intent cannot be used as a process restart grant.

## Consistent operator response-readiness snapshot (2026-10-15)

The non-executable human response-intent readiness view now reads the original candidate, active canonical Project/Workspace, incident review, permit, proposer/reviewer membership and operator TCP-observer stop latch from **one SQLite read-only snapshot**, with one server clock value for all expiry decisions. This prevents reports assembled from mutually inconsistent concurrent database revisions. Each early exit rolls back its read transaction; the successful complete view commits it. The result remains `state=blocked` and `execution_authorized=false` with permanent no-adapter and no-postcondition blockers; it is a snapshot advisory, **not** a live operation grant, independent physical evidence, current device-health guarantee or authorisation to restart any process.
