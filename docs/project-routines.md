# Project routines

Project routines connect durable `Routine` responsibilities to applications without putting cron inside containers.

A `ProjectRoutineBinding` identifies the Project Runtime, optional application, Routine, action kind, and action specification. A binding does **not** grant a capability and does not execute by itself.

The initial executable action is `app_command`. When a Routine occurrence has been materialized as a normal Task, `projectroutine.Executor` verifies that:

- the binding is active;
- the Task belongs to the bound Project;
- the Project Runtime and application have independently observed `running` state;
- a worker principal and CapabilityLease were supplied.

It then invokes the trusted `project.app.exec` sandbox tool. The invocation result does not complete the Task. Normal Verification/Checkpoint/Task completion rules still apply.

The later Routine Engine remains responsible for trigger calculation, occurrence materialization, retry/catch-up policy, and issuing/obtaining the per-occurrence authority required by the action. This design keeps schedules self-hosted while preserving the same control-plane policy and audit trail as interactive work.
