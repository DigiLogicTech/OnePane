# M31 Team Mode

Team Mode is OnePane's human-in-the-loop multi-agent orchestration mode. It sits between ordinary Supervisor execution and formal Council deliberation:

- **Supervisor**: one orchestrator autonomously delegates bounded work and drives a Task to verified completion.
- **Team**: a human and multiple named agent roles share a durable deliberation room, produce an explicit TeamPlan, then hand that accepted plan to ordinary OnePane execution. The room may be reopened later without discarding verified progress.
- **Council**: a later, more formal independence/critique/synthesis protocol. Council may be invoked from a Team session but does not replace Team Mode.

## Durable model

M31 adds:

- `teams` — reusable team definitions.
- `team_members` — human, agent and supervisor roles. Agent roles carry scheduler capability/role/protocol requirements and a route policy rather than being permanently tied to one model.
- `team_sessions` — one Team deliberation associated with one Task.
- `task_execution_profiles` — explicit Task mode (`direct`, `supervisor`, `team`, `council`). Legacy Tasks without a profile retain existing Supervisor semantics.
- `team_messages` — append-only conversational turns with principal/member provenance.
- `team_turn_requests` — durable fan-out work for selected/all non-human members.
- `team_plans` — versioned candidate/accepted plans.
- `team_objections` — retained dissent with severity and explicit resolution/risk acceptance.
- `team_decisions` — durable human decisions, including acceptance of a plan or risk.

## Admission invariant

A Team-mode Task is not executable just because a model says the discussion is finished.

```text
Task CREATED
    + TeamSession DELIBERATING
          |
          v
      PLAN_PROPOSED
          |
      human accepts
          |
          v
      PLAN_ACCEPTED
          |
          v
Task READY -> normal Supervisor/AgentWorker execution
```

`task.Service` has a trusted admission guard. If a `task_execution_profile` says `team`, both `MarkReady` and `Start` fail closed until the linked TeamSession has an accepted TeamPlan. The autonomous AgentWorker also excludes unaccepted Team Tasks from ordinary CREATED-task admission so they do not produce repeated admission failures.

The accepted TeamPlan is inserted into the AgentWorker Context Compiler as required `AUTHORITATIVE_DATA`. The raw Team transcript remains derived evidence; other agents' comments cannot silently become execution instructions.

## Deliberation rounds

Posting a message and requesting a round are separate operations. This lets the human write several messages or target a subset of the team without creating unintended inference calls.

A round creates one durable `team_turn_request` per selected active non-human member. The TeamWorker independently routes each turn using that member's:

- `role_name`
- `capability_id`
- Agent Protocol level
- cost/qualification route policy
- optional BudgetAccount

Therefore an Architecture role can route to one deployment while Security or Research routes to another. Agent roles are not permanently model-bound.

The TeamWorker sends a bounded Agent Protocol request with **reasoning-only** proposal types. Tool/delegate proposals are not permitted and Team deliberation cannot produce external side effects. Context trust labels are preserved: human turns are `USER_INSTRUCTION`; model/agent turns are `UNVERIFIED_DERIVED`.

Protected subscription/paid/provider-managed routes retain M27 rules. They require explicit route permission plus a BudgetReservation. Local/proven hard-$0 capacity remains preferred by default.

## Dissent and human authority

A TeamPlan may have objections. `blocking` and `critical` objections prevent plan acceptance unless an authenticated human explicitly chooses `accept_open_risk`. That action resolves the relevant objections as `accepted_risk` and records a durable `team_decision`.

The Supervisor does not erase dissent or fabricate consensus. The record keeps:

- who raised an objection,
- its severity,
- the plan version it applied to,
- how it was resolved,
- which human accepted residual risk.

Human/agent provenance is bound at the service layer. A human cannot create a Team message, proposal or objection while claiming another agent member authored it. System/agent principals may author non-human members only through trusted control-plane paths.

## Reopening deliberation

A human may reopen the Team room after a plan is accepted. If the Task is `READY` or `RUNNING`, OnePane pauses it using the existing Task/TaskAttempt lifecycle and changes the TeamSession to `paused_for_deliberation`.

A revised plan can then be proposed and accepted. `Task.Resume` resumes the existing waiting attempt where applicable; verified checkpoints and other durable progress remain intact.

## API surface

```text
GET/POST /v1/teams
GET/POST /v1/teams/{teamID}/members

POST /v1/tasks/{taskID}/team-session
GET  /v1/tasks/{taskID}/team-session
GET  /v1/team-sessions/{sessionID}

GET/POST /v1/team-sessions/{sessionID}/messages
POST     /v1/team-sessions/{sessionID}/rounds
GET      /v1/team-sessions/{sessionID}/turns

GET/POST /v1/team-sessions/{sessionID}/plans
POST     /v1/team-sessions/{sessionID}/plans/{planID}/accept

GET/POST /v1/team-sessions/{sessionID}/objections
POST     /v1/team-objections/{objectionID}/resolve
GET      /v1/team-sessions/{sessionID}/decisions
POST     /v1/team-sessions/{sessionID}/reopen
```

`team.read` and `team.write` are workspace-scoped API capabilities.

## Gateway/UI integration

`team_sessions.gateway_target_id` reserves a first-class association with the M30 messaging layer. M31 does not yet treat Discord/Slack/etc. as authenticated conversational ingress; that remains a final WebUI/gateway-ingress task. The authoritative Team transcript and human identity remain in OnePane, so adding a gateway conversation surface later does not require redesigning Team state or weakening the authority boundary.
