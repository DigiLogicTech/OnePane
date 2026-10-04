from pathlib import Path

root = Path(__file__).resolve().parents[1]
coord = (root/'internal/operation/coordinator.go').read_text()
types = (root/'internal/operation/types.go').read_text()
recon = (root/'internal/projectruntime/reconciler.go').read_text()
boot = (root/'internal/bootstrap/bootstrap.go').read_text()

checks = {
    'restart marks executing unknown': 'StateExecuting, StateUnknownOutcome' in coord and 'process_restart' in coord,
    'unknown blocks before retry': 'StateUnknownOutcome, StateBlockedUnknownOutcome' in coord,
    'direct unknown to prepared forbidden': 'case StateUnknownOutcome:\n\t\treturn to == StateBlockedUnknownOutcome' in types,
    'verified safe retry explicit': 'RetryAfterReconciliation' in coord and 'verified_safe_retry' in coord,
    'resource lease renewed': 'RenewResourceLease' in coord,
    'accept reconciled desired state': 'AcceptReconciledOutcome' in coord and 'reconciled_desired_state_present' in coord,
    'bootstrap runs recovery before workers': 'RecoverInterrupted(ctx, nil, nil, nil)' in boot,
    'project runtime uses independent observer': 'ErrIndependentObserverRequired' in recon and 'MutationPrincipalID == cmd.ObserverPrincipalID' in recon,
    'project runtime records V2 verification': 'policy.VerificationV2' in recon and 'observation_id' in recon,
}
failed = [k for k,v in checks.items() if not v]
if failed:
    raise SystemExit('M16 recovery validation failed: ' + ', '.join(failed))
print('M16 recovery/reconciliation contract: PASS')
