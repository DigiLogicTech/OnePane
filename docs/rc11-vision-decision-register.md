# OnePane RC11 — user vision alignment decision register

This register follows the **first assistant code/security review** of draft
[PR #227](https://github.com/DigiLogicTech/OnePane/pull/227).
Decision 1 and the core product vision have been confirmed by the user.
Decisions 2–6 remain open. Approval of a design direction is not approval
to merge, package, or grant new runtime privileges.

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

## Decision 2 — local-first autonomy defaults
**Question:** Once the user approves an isolated local sandbox, should
autonomous work be active by default, even on slower hardware, or require
additional per-Project/Workspace consent?

**Proposed starting position:** Automatic action is available for
explicitly approved Workspace sandboxes, subject to per-Workspace
caps and revocable permissions. Never give implicit host, network, secrets
or cross-Workspace authority; no cloud fallback without user grant.

**Status:** AWAITING USER.

## Decision 3 — file permissions across all tools
**Question:** Should disabling Files block all file-capable paths including
shell, Git, MCP, artifacts and OCI execution, or only high-level File tools?

**Proposed starting position:** All file-capable operations must be
classified as read, write, publish or execute before execution. Denied
read/write/publish cannot be bypassed via a generic shell or MCP adapter.
Trusted toolchains use explicit scoped grants, never hidden exception lists.

**Status:** AWAITING USER.

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
