# Autonomous agent worker (M26)

M26 is OnePane's first general-purpose autonomous Task runner. It does not turn a
model into the control plane: the durable Task, Attempt, authority, route policy,
ToolGateway, OperationCoordinator, evidence, and completion gate remain owned by
OnePane.

## Durable execution model

Each started general Task gets one `agent_worker_run` bound to its active
`TaskAttempt`. The run records protocol/role/capability, bounded counters, route
policy, continuation state, last candidate, revision, and terminal state.
`agent_worker_steps` is an ordered journal of routing, model/runtime dispatch,
tool/operation work, delegation, replan/escalation, human waits, and assurance.

Reasoning step consumption is persisted before dispatch. If `harnessd` disappears
while a step is active, startup marks the run `interrupted` and uses Task recovery
to interrupt/block the attempt. OnePane does not assume an external model request
or mediated action was never executed merely because the process restarted.

## Route policy

The default worker asks for capability `inference.general`, role `general`, and
protocol L1. The scheduler may select either a qualified ModelDeployment or an
eligible proposal-only/gateway-mediated AgentRuntime.

The default autonomous route policy is deliberately conservative:

- prefer local/proven zero-incremental-cost capacity;
- protect finite subscription allowance;
- deny routes that may incur monetary spend;
- permit mediated/degraded candidates where policy still allows them;
- exclude candidates explicitly after dispatch failure or escalation.

Subscription or paid API usage therefore requires a later trusted control-plane
change to the run policy; a model cannot opt itself into spend.

## Context and verified progress

The Context Compiler always includes current Task state. It may also include the
current Project, bounded worker continuation, and the latest valid Checkpoint.
Checkpoint material is labelled verified derived state and is preferred over old
continuation/history when context must be bounded. Repository/web/document content
retains its trust labels and never becomes control instructions merely because it
appears in model context.

## Agent protocol

Models and external runtimes receive canonical `AgentRequest` objects and must
return one canonical `AgentResponse` proposal: `tool`, `delegate`, `replan`,
`complete`, `human`, `escalate`, `wait`, or `fail`. The worker validates protocol
version, request ID, and proposal shape before handling it.

## Authority boundary

The general worker has system identities for authority bookkeeping, execution,
and independent verification. They receive explicit Workspace memberships so
trusted administrators/services can issue scoped authority and assurance can be
recorded in the Workspace.

Membership is **not** a capability grant. M26 contains no path for the worker to
issue itself a `CapabilityLease`. A tool proposal must find an existing active,
unexpired, unexhausted lease bound to:

- the current Workspace;
- `system:agent-worker`;
- the current Task;
- the trusted tool capability;
- the requested action/resource scope.

Without that lease the run waits for authority instead of executing.

## Tool and mutation proposals

OBSERVE, READ, and allow-listed EXECUTE_SANDBOXED proposals use ToolGateway.
Results are persisted as immutable Observation/evidence before they enter the next
reasoning context.

MUTATE and EXTERNAL_SEND proposals enter OperationCoordinator. They use the same
intent/authorization/resource-lock/ExecutionPermit boundary as every other
external mutation. Adapter return reaches only `OBSERVING`; the agent worker does
not self-verify or commit its own side effect. The run waits until an independent
assurance path commits the Operation, and unknown outcomes remain blocked for
reconciliation.

## Delegation, replan, and escalation

Delegation creates a normal child Task and hard parent dependency. Child work is
therefore independently durable and auditable.

Replans and escalations are bounded by counters stored in the run. Escalation (or
dispatch failure within the bound) adds the failed candidate ID to the next route
request, preventing an immediate loop back onto the same deployment/runtime.

## Completion

A model/runtime `complete` response is only a proposal. OnePane moves the Task
through `COMPLETION_REQUESTED` and `VERIFYING`.

For a completion contract requiring only V0, a separate system verifier may
record declaration-level V0 evidence, produce a Checkpoint, and invoke
`CompleteVerified`. If the Task requires V1-V5, M26 does not manufacture stronger
evidence from model agreement. M28 now supplies the independent Assurance service:
the worker blocks at verification, and Assurance earns the requested level from
immutable deterministic/artifact/observation evidence (plus bound human acceptance
for V5) before the normal Checkpoint + CompleteVerified path may finish the Task.

## Routine worker interaction

Project Routine `app_command` Tasks remain owned by the dedicated Routine worker.
M26 explicitly excludes those Tasks during CREATED/READY admission so the two
workers cannot race for one TaskAttempt. Other Routine Tasks can later use the
general worker when their action semantics are added.
