# Routine Engine

The Routine Engine is a deterministic control-plane scheduler. It does **not** install cron entries inside Project containers and it does not execute application commands directly.

A due Routine occurrence becomes a normal Task:

```text
Routine definition
  -> deterministic due occurrence
  -> routine_occurrence (unique occurrence_key)
  -> Task (background_routine)
  -> ordinary admission / scheduling / authority / execution / verification
```

Occurrence creation and Task creation commit in the same SQLite transaction. This prevents the scheduler from losing the durable relationship between scheduled responsibility and execution after a crash.

## Initial trigger types

- `interval`: `every_seconds`, optionally anchored by `start_at_utc`.
- `daily`: local `HH:MM`, optional ISO weekdays (`1=Monday ... 7=Sunday`), evaluated in the Routine's IANA timezone.

Go timezone data is embedded so routine behavior does not depend on the host having a complete system tzdata package.

## Catch-up

`policy_json.catch_up` supports:

- `latest` (default): instantiate only the latest due occurrence after downtime.
- `all`: instantiate a bounded batch (`max_catch_up`, default 10, max 100) and continue on later scheduler ticks if a backlog remains.
- `skip`: record the latest due occurrence as `missed` without creating a Task.

`latest` and `skip` calculate the most recent slot directly; they do not iterate through an unbounded historical backlog.

## Project applications

If a Routine has one active `project_routine_binding`, its Task is bound to that Project. The existing Project Routine executor can then execute the bound `app_command` through `project.app.exec` inside an independently verified running sandbox.

The binding itself grants no authority. Execution still requires the normal Task/Attempt, CapabilityLease, ToolGateway and verification path.

The current v0.1 engine deliberately rejects more than one active Project binding for the same Routine during materialization. Multi-action/workflow routines should later be represented explicitly rather than ambiguously overloading one occurrence/task row.
