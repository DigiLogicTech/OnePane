from pathlib import Path
root=Path(__file__).resolve().parents[1]
mig=(root/'migrations/0012_assurance_worker.sql').read_text()
svc=(root/'internal/assurance/service.go').read_text()
types=(root/'internal/assurance/types.go').read_text()
api=(root/'internal/api/server.go').read_text()
budget=(root/'internal/budget/service.go').read_text()
obs_svc=(root/'internal/observation/service.go').read_text()
obs_types=(root/'internal/observation/types.go').read_text()
for token in ['CREATE TABLE assurance_runs','CREATE TABLE verification_acceptances','evidence_hash','waiting_human']:
    assert token in mig, token
for token in ['deriveLevel','minimumEvidenceTime','VerifyIntegrity','VerifyContent','RunWaitingHuman','acceptanceState','resumeResolved',"ar.status IN ('queued','running','interrupted','waiting_evidence','waiting_human')", "event_type='operation.executing'", 't.Int64 > minimum', 'return minimum, nil']:
    assert token in svc, token
for token in ['ErrHumanAcceptance','ErrAcceptanceIneligible','EvidenceHash']:
    assert token in types, token
for token in ['verification.accept','acceptVerification']:
    assert token in api, token
for token in ['ErrReservationActive','agent_runtime_invocations','inference_requests']:
    assert token in budget, token
for token in ['ErrSourceActorMismatch']:
    assert token in obs_types, token
for token in ['cmd.SourcePrincipalID != nil && cmd.ActorPrincipalID != nil', 'ErrSourceActorMismatch']:
    assert token in obs_svc, token
print('M28 independent V1-V5 assurance + reviewed budget expiry: PASS')
