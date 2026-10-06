# Deterministic Watchdog

The control-plane watchdog is a deterministic runtime-health primitive. It is not model-backed.

`harnessd` writes a heartbeat to `watchdog_states` every five seconds. Policy considers the watchdog healthy only while that heartbeat remains inside its configured freshness window (15 seconds in the current bootstrap).

The Policy Engine fails closed when the watchdog is stale or unavailable:

- `OBSERVE` and `READ` remain permitted when other authority checks pass;
- `EXECUTE_SANDBOXED`, `MUTATE`, and `EXTERNAL_SEND` are denied.

This gate is inside Policy rather than the Routine or Project subsystems, so autonomous side effects cannot bypass it by entering through another client or integration.

If the watchdog goroutine terminates unexpectedly, no special emergency callback is required: the heartbeat naturally expires and subsequent mutation authorization fails closed.
