# Operations and recovery

External mutation is never represented as a direct tool call. `OperationCoordinator` owns the durable intent and follows:

`PROPOSED -> AUTHORIZED -> PREPARED -> EXECUTING -> OBSERVING -> VERIFIED -> COMMITTED`

The coordinator snapshots the CapabilityLease revision, policy revision, required verification, and approval requirement before acquiring an exclusive `ResourceLease`. Immediately before a mutating adapter call it mints a short-lived, single-use **ExecutionPermit**. The permit exists only in process memory and is never a durable domain object.

A successful adapter response moves the Operation only to `OBSERVING`. A direct resulting-state probe must independently establish the postcondition before `CommitVerified` can release the exclusive ResourceLease.

## Crash and unknown-outcome recovery

If harnessd restarts while an Operation is `EXECUTING`, bootstrap changes it to `UNKNOWN_OUTCOME` and then `BLOCKED_UNKNOWN_OUTCOME`. It is never automatically retried.

Recovery has two explicit paths after a direct V2+ observation:

1. **Desired state already exists.** `AcceptReconciledOutcome` moves the Operation back to `OBSERVING`; the normal verification/commit path finishes it.
2. **A retry is independently proven safe.** `RetryAfterReconciliation` moves the blocked Operation back to `PREPARED` and renews its existing exclusive ResourceLease. It does not allocate a second writer lock.

`UNKNOWN_OUTCOME -> PREPARED` is intentionally impossible. The reconciliation block is mandatory.

Project Runtime reconciliation uses the same mechanism. Mutation and observation must be performed by different principals, and GUI-facing observed state is advanced only from a V2+ Verification that names the exact immutable Observation used as evidence.
