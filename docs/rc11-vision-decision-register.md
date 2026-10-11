# OnePane RC11 — user vision alignment decision register

This register follows the **first assistant code/security review** of draft
[PR #227](https://github.com/DigiLogicTech/OnePane/pull/227).
The core vision and Decisions 1–3 (including Level 5 remediation approval
defaults and universal Workspace permissions) have been confirmed by the user. Decisions 4–6 remain open.
Approval of a design direction does not grant runtime privileges, authorise
release packaging, or approve a merge.

## Product purpose — confirmed by user (2026-10-11)

OnePane began as a **personal, local-first autonomous work harness** intended
to finish jobs even when the available hardware is old, memory-constrained
or slow. It must optimize for **eventual task completion**, predictable
resource usage and minimal human babysitting, not just high tokens/second.

The foundational design elements are:
- **Model queueing and hot swapping:** a model does not need to stay resident
  after its work; jobs and stages can queue while the hardware is reused.
- **Colibri-assisted larger-model use:** resource-constrained execution
  is a first-class supported path rather than a second-class fallback.
- **Capability-aware model routing:** send each stage to a model/Worker that
  can use the necessary tools, favor local or trusted compute, and minimize
  paid cloud usage subject to user-configured permissions and deadlines.
- **Accessible automation:** clear defaults, checkboxes and drop-down controls
  with automated worker dispatch, scheduling and resumability. Avoid requiring
  the user to manually wire routine orchestration details.
- **Research and rigorous operations:** advanced governance, isolation,
  provenance, councils and debug controls extend the core harness; they
  must not make ordinary personal usage harder.

Performance is not the sole success metric. Jobs should survive model
swaps, resource contention, restarts and delayed execution wherever they
can safely and deterministically resume. No hidden cloud fallback,
cross-Workspace access or tool privilege is implied by this goal.

## Decision 1 — unified OnePane Assistant and scoped authority

**User decision (2026-10-11):** JOIN the global and Project Assistants into
**one cohesive Assistant**. There is insufficient benefit in two parallel
assistant interfaces when the Project Assistant cannot directly interfere
with isolated sandboxes. Do not require a distinct Home Workspace assistant.

**Implementation contract to carry into design and second audit:**
- One assistant/chat entry point is always available for general assistance,
  project development and orchestration; Project/Workspace context may be
  selected or changed in the same interface, without assistant switching.
- Display the currently selected Project/Workspace clearly; no selected
  Project means a general conversation, **not** implicit permissions on
  every Project or filesystem.
- A chat/model response has no direct sandbox authority. To act on a Project,
  create or route authorised Tasks/Workers bound to the canonical Project
  and Workspace, inheriting its security and sharing policy.
- A single assistant can coordinate separate Workspaces via explicit
  brokered grants and auditable artifacts; it cannot bypass sandbox
  boundaries by changing its chat context or choosing another model.
- Preserve the distinction between user-facing conversation/context and
  authorised background execution as **modes/capabilities**, not separate
  assistants. Keep model selection, resource-aware routing and automated
  queues readily accessible without overwhelming novice users.

**Status:** CONFIRMED BY USER — design direction. Code implementation and
acceptance tests are still outstanding.

## Decision 2 — local-first autonomy and continuous operation modes

**User decision (2026-10-11):** LEVEL 4 AUTONOMOUS PROJECT ORCHESTRATION
is the standard OnePane operating model; LEVEL 5 GOAL-DRIVEN CONTINUOUS
OPERATIONS must ALSO BE IMPLEMENTED as a first-class selectable mode.

**User scenario:** "Stand up a project" is Level 4 (plan, create workspaces,
deploy workers/models/tools, implement, test and deliver). A project whose
objective is "protect my home network" is Level 5: deliver initial protection,
then continuously monitor, evaluate, maintain and improve the approved
defensive posture. Completion criteria for Level 5 are continuous compliance
with an objective, not a single terminal deliverable.

**Design contract:**
- Default new Projects to Level 4. Expose a clear Project **Operating mode**
  choice: Autonomous Project (L4) or Continuous Operations (L5), with an
  ordinary UI selector and ability to promote an existing Project.
- Both modes can create the necessary subordinate Agents, Workers, Tasks,
  queues, Sandboxes and Workspaces *inside the approved Project authority*.
  Neither mode silently grants new filesystem, host, secret, network, cloud
  spending or cross-Workspace access.
- L4 can decompose goals, iterate, validate and finish. It preserves Work
  progress through model hot swaps, Colibri operations, resource starvation,
  rate limits and resumable failures.
- L5 does everything in L4 plus maintains durable objectives, risk thresholds,
  evidence baselines, schedules or event-driven triggers, drift detection,
  self-healing workflows, change history and recurring verification.
- For home-network defence L5 may maintain an inventory of authorised network
  devices and services, compare observable DNS/uptime/exposure/config signals
  with baselines, detect meaningful anomalies, and propose or run **approved**
  defensive remediation with backup, rollback and post-change verification.
- Revocable permissions, scope, rate/budget limits, cooldowns, safe-change
  classes, maintenance windows and explicit stop/pause/hand-off conditions
  constrain L5. Unknown effects or risky changes pause/escalate rather than
  retrying blindly or locking out the operator.
- L5's observability must be truthful: "monitoring", "needs attention",
  "remediation in progress", "validated" and "paused" are distinct states.
  An empty queue does not mean the ongoing goal is complete.
- All user workflows should be accessible through simple toggles, dropdowns
  and descriptions, with advanced policy panels optional.

**User decision — remediation authority (2026-10-11):** Both choices
must be supported. By default, Level 5 can autonomously execute only
preapproved, reversible, low-risk fixes within granted Project/Workspace
scope. High-impact changes require explicit per-action approval. Users may
opt in to **specific preauthorised high-impact playbooks**, allowing those
actions to execute without asking again when all of their constraints match.

**Permission invariants and UI:**
- Default mode: `safe_automatic` — low-risk, reversible, bounded actions
  automatically; disruptive or ambiguous effects escalate for approval.
- Optional advanced mode: `custom_preapproved` — individual higher-impact
  playbooks must be selected and explicitly authorised by the user.
- Never provide an unrestricted "allow anything" grant. Every playbook
  constrains Project/Workspace, resource/device targets, capability/action,
  allowed arguments, effect/risk class, approval owner, permit lifetime,
  cooldown, execution-count limits, and permitted maintenance windows.
- Capture pre-state/backup where supported, check fresh permissions at the
  actual side-effect boundary, record an auditable decision, verify post-state
  and roll back when both safe and supported. If outcomes are uncertain, hold
  for reconciliation; no blind repeat of risky changes.
- Revocation and pause must immediately prevent **new** invocations, including
  those made by newly created Agents/Workers or through a different adapter.
  Existing external side effects may not be reversible; report this honestly.
- New Projects and upgrades retain the safe default; selecting Level 5 alone
  never activates high-impact rules. Use clear checkboxes, playbook-specific
  prompts, an easily accessible kill switch and plain-language risk labels.
- The user can authorise a narrowly scoped router/firewall/device action in
  advance, but OnePane may not silently extrapolate that permission to other
  rules, targets, networks or hosts.

**Status:** CONFIRMED BY USER — Level 4 default, Level 5 required, and both
remediation approval strategies supported; safer approval-gated mode is the
default. Implementation and testing are outstanding.

## Decision 3 — universal Workspace permissions across tools

**User decision (2026-10-11):** CONFIRMED. Provide simple, separate
Workspace permission controls for **Read files, Write files, Execute,
Network, and Secrets**, with advanced scoping for users who need it.
The same effective permissions must apply regardless of whether a
Worker uses Python, shell, Git, MCP, an OCI container, a GUI tool, an
artifact API or an internal tool adapter.

**Product and security contract:**
- Present user-friendly checkboxes/dropdowns and appropriate presets.
  Keep advanced settings for exact paths, mounts, targets, network
  destinations, commands, secret IDs, limits and approval requirements.
- Project permissions form the upper bound; a Workspace can narrow
  them, and individual Agents/Tasks may only narrow them further.
  Switching model, Agent or tool transport cannot broaden permissions.
- Read and Write are distinct; Write includes create, modify, delete,
  publish and mutations performed indirectly through Git, shell,
  Python, toolchains and MCP, with finer action classes where needed.
  Execute, network and secret access are independent permissions.
- Separate sandbox file access from host file access. A Workspace
  write grant must not imply access to the host, unrelated mounted
  directories, another Workspace, host credentials or external services.
- Evaluate **capabilities and actual effects**, not just a tool name.
  Unclassified or opaque operations must be denied or require an
  explicit narrowly scoped permit; a generic shell or MCP tool must
  never bypass a denied file operation.
- Enforce revocable policy at the last controllable side-effect
  boundary and with runtime isolation where available. UI switches
  alone are insufficient: direct API and background Worker paths
  must enforce the same effective rules.
- Keep explicit, auditable brokered sharing for cross-Workspace
  artifacts and connectivity; authorization cannot be invented by
  a prompt or inherited from a chat context change.
- Preserve local-first functionality: a Workspace may be able to
  read/write within its approved isolated directory and run its
  approved tools without repeatedly requesting permission.
  Extra privileges require transparent, revocable approval.
- Never enable additional host, network, secret or cross-Workspace
  privileges when upgrading existing installations without consent.

**Implementation and verification:** See
`docs/rc11-universal-workspace-permissions.md` for action taxonomy,
enforcement adapters, UI behaviour and negative-path tests.

**Status:** CONFIRMED BY USER — design and UX requirements approved.
Complete end-to-end enforcement is still implementation work.

## Decision 4 — Agent policy and revocation
**Question:** Should Agent policies use immutable versioned snapshots,
live revocation checks, or both?

**Proposed starting position:** Versioned Agent policies for provenance,
plus rechecks at each Task, inference and tool side-effect boundary.
A revoked Agent must not start a new action; in-flight external calls
need explicit cancellation/unknown-outcome semantics and safe recovery.

**Status:** AWAITING USER.

## Decision 5 — diagnostics visibility and privacy
**Question:** Is operator-controlled, time-limited live policy tracing
needed in addition to the offline support-bundle reviewer?

**Proposed starting position:** Opt-in, time-limited local trace sessions
with redacted evidence and explicit export, disabled by default. No raw
prompts, tokens, credentials or persistent cross-process correlation key
in support bundles.

**Status:** AWAITING USER.

## Decision 6 — compute and provider trust categories
**Question:** Can verified on-device OpenAI-compatible model runtimes and
paired trusted LAN Nodes count as local-first when cloud permission is off?

**Proposed starting position:** Distinguish **origin-host local**, **trusted
paired Node** and **external/cloud** by observed and verified network
destination, node identity and data-flow policy, not model/provider labels.
Require an explicit grant for off-host transfers even to trusted Nodes.

**Status:** AWAITING USER.

## Unresolved release gates
- [ ] User approval of first code review and this vision alignment.
- [ ] Apply decisions to Workspace, Agent, UI and runtime permission boundaries.
- [ ] Repeat independent code/security audit on the finalized contract.
- [ ] Obtain approval to create Windows x64 / Ubuntu amd64 installers.
- [ ] Run installed-build persistence, hardware and manual QA.
- [ ] Authorize release merges.

**Hard stop:** No merge, packaging or claim of full product QA from draft CI.
