# RC11 — Agent policy provenance and live revocation

**Decision:** confirmed by user on 2026-10-11 — BOTH immutable
versioned Agent policy records and active revocation checks are the
standard. Applies to Level 4 Autonomous Project and Level 5
Continuous Operations, including Research mode and delegated Workers.

**Status:** accepted implementation requirements, not a claim of
complete implementation. Requires a second code/security audit and
user review before packaging or release.

## Model: snapshot for evidence, live policy for authority

An Agent configuration consists of an immutable **versioned revision**
and a currently effective **authorization policy**. Preserve separate
identities for the Agent, Project, canonical Workspace, owning principal,
Task, execution Attempt, Team seat and any approved playbook. Never
infer authority from a chat transcript, model instructions or a stale
execution snapshot.

Each run or research round should record:
- Agent ID and configuration revision, model/provider/runtime identities,
  tool and skill versions, prompt/template revisions and relevant
  provenance hashes (without leaking sensitive content);
- Effective policy scope ID and revision at initial admission, approval
  owner and permit identifiers, restrictive child overrides, and
  evidence of Workspace/Project ownership;
- Decision timestamp and capability/target classification, without
  raw prompts, secrets, credentials or unconstrained tool outputs;
- Subsequent revocation/approval events and whether the run stopped,
  canceled, completed safely or entered unknown-outcome reconciliation.

Configuration changes create a new revision. Historical policy
revisions must remain immutable for audit, even after an Agent is
updated, disabled or removed. **The historical snapshot never grants
present-day rights:** each new side-effecting action checks the current
effective policy and valid, unrevoked permits.

## Enforcement and execution

1. Compute effective permissions from current Project, canonical
   Workspace, Agent and Task restrictions; child grants cannot exceed
   parent grants. Unknown, ambiguous and missing execution mappings
   must not fail open.
2. Recheck at the durable Task/Worker admission transaction, resume,
   delegated Worker creation, model dispatch, operation prepare/commit,
   last controllable external tool boundary, and Level 5 scheduled
   playbook execution. Ordinary chat does not confer sandbox authority.
3. Use OS/runtime confinement for filesystem/process/network/secrets,
   alongside application policy checks: a generic shell/MCP/GUI tool
   must not bypass revocation by choosing a different adapter.
4. On revocation, block new work, invalidate outstanding permits and
   cancel queued operations that require revoked authority. An ongoing
   model or tool call should receive a cancellation request when the
   adapter supports it; stopping a local Worker cannot roll back an
   already-committed external action.
5. If an external action was started and its result is unknown,
   transition to an explicit **outcome unknown / operator reconciliation**
   state. Avoid replay until verification proves that retry is safe.
6. When policy changes affect only one capability, unaffected Tasks
   may continue using their still-authorized scope. A full pause or
   Agent disablement should suspend its queue without deleting
   recoverable intermediate results.
7. Model hot swapping, Colibri use, cloud/provider route changes,
   service restarts and resource starvation must not revive an
   obsolete permit or discard relevant policy provenance.
8. Level 5 preauthorized playbooks remain individually scoped with
   expiry, target/argument restrictions, version references,
   cooldown and verification; live revocation overrides prior approval.

## UI contract

Keep ordinary usage simple: one Agent detail panel with the assigned
model/routing preference, enabled tools, visible Project/Workspace
association, permissions, status and an emergency **Pause / Revoke**
control. Advanced view exposes the precise action grants, version
history, policy differences, effective inheritance and incident logs.

Avoid a hidden distinction between what the model *was configured to
believe* and what the run may *actually execute now*. Display clear
reasons for blocks: permission denied, awaiting approval, revoked,
budget capped, resource unavailable, or unknown outcome.

## Implementation workstreams (all pending)

- [ ] **AGPOL-01** Immutable policy revision and provenance schema;
      migrate without granting new rights or rewriting old evidence.
- [ ] **AGPOL-02** Canonical effective-grant resolver across
      Project -> Workspace -> Agent -> Task -> operation permit.
- [ ] **AGPOL-03** Last-boundary enforcement in inference, tool,
      operation, sandbox/OCI/MCP, Workspace sharing, secrets and network.
- [ ] **AGPOL-04** Revocation event and task cancellation service;
      invalidate outstanding permits and distinguish cancellation
      confirmed from unknown external effect.
- [ ] **AGPOL-05** Durable Agent queue/routing pause/resume with safe
      recovery and persistent resource/model wait.
- [ ] **AGPOL-06** Research Council pinned-seat/versioned-round
      provenance, isolation and failed-seat retention.
- [ ] **AGPOL-07** Level 5 recurring playbook authorization, expiry,
      cooldown, recheck and pre/post verification.
- [ ] **AGPOL-08** Simple permissions UI and advanced revision/history
      viewer, approval/revoke/pause action explanations.
- [ ] **AGPOL-09** Unit, SQLite integration, fake-adapter and sandbox
      negative-path tests; Windows/Ubuntu hosted CI.
- [ ] **AGPOL-10** Post-alignment second code/security audit and
      user-approved installed-build QA.

## Negative-path acceptance

- Permission is disabled between Task admission preflight and the
  transaction: no Worker/Attempt/Task mutation is committed.
- A Waiting Worker is resumed after Agent/Workspace revocation:
  continuation stays waiting/blocked and cannot execute a model.
- The model or tool is already running when permission is revoked:
  cancellation is attempted and tracked; if outcome is unknown,
  no automatic duplicate external side effect occurs.
- A delegated Agent/model swap tries to use a different tool to perform
  the same denied operation: policy and sandbox both prevent bypass.
- Team/Council pinned model fails or is revoked mid-round:
  preserve isolated provenance and failed seat; never substitute
  an unapproved provider.
- Project L5 high-impact permit expires, is revoked or targets another
  device: execution is rejected even if the model recommends it.
- An old config snapshot requests a permission since removed:
  only current effective grants authorize actions.
- Agent is paused during resource starvation or restart:
  queue persists without privilege resurrection or success claims.
- A normal user can inspect and revoke an Agent permission using
  direct UI controls without editing configuration files.

**Hard release gate:** Approval of design ≠ implementation or package
approval. Keep RC11 draft until subsequent design alignment, second
audit, full CI and user-directed QA.
