# Approvals and Compensation

M17 turns policy approval requirements into durable, single-use authorization objects and treats rollback as another governed Operation.

## Approval binding

An Approval is bound to:

- the exact Operation intent;
- workspace/task/attempt/principal context;
- tool + adapter identity;
- resource reference;
- desired/precondition/reconciliation state;
- operation input hash;
- required approval level;
- Policy revision.

Changing any of those values invalidates the approval binding.

Approvals expire and are single-use. The requesting principal cannot approve its own Operation. Approval principals must be active human workspace members and hold a suitable human role (`Approver` or `Admin`; `Admin` is required for admin-level approval).

When an authorized Operation is waiting for approval, `ContinueApproved` consumes the approved record in the same transaction that acquires the exclusive ResourceLease and advances the Operation to `PREPARED`. A resource conflict therefore cannot consume an approval without also establishing the execution precondition it approved.

## Compensation

Compensation is not an unlogged callback or root cleanup hook. A committed Operation may contain a typed compensation specification. Invoking compensation creates a new Operation linked by `compensates_operation_id`.

The compensating Operation:

- targets the same logical resource in v0.1;
- requires a fresh CapabilityLease;
- passes Policy again;
- may require a new human approval;
- acquires the normal exclusive ResourceLease;
- executes through ToolGateway + MutationGate;
- produces its own receipt/Observation/Verification;
- can itself enter UNKNOWN_OUTCOME if the external state is ambiguous.

This means rollback remains auditable and recoverable rather than receiving implicit privilege.
