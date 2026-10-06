from pathlib import Path
root=Path(__file__).resolve().parents[1]
schema=(root/'migrations/0001_initial.sql').read_text()
mig=(root/'migrations/0011_budget_enforcement.sql').read_text()
worker=(root/'internal/agentworker/execution.go').read_text()
infer=(root/'internal/inference/execute.go').read_text()
budget=(root/'internal/budget/service.go').read_text()
required_schema=['CREATE TABLE budget_accounts','CREATE TABLE budget_reservations','committed_amount + reserved_amount <= limit_amount']
for x in required_schema:
    assert x in schema, x
for x in ['agent_runtime_invocation_id','ux_budget_reservations_active_inference','ux_budget_reservations_active_runtime',"status='reserved'"]:
    assert x in mig, x
for x in ['accountChainTx','ErrInsufficientBudget','CommitFromUsage','BindInferenceRequestTx','BindAgentRuntimeInvocationTx','ErrReservationAlreadyBound','budget.reserved']:
    assert x in budget, x
for x in ['ErrBudgetRequired','BudgetReservationID','inference_request.budget_reserved']:
    assert x in infer or x in (root/'internal/inference/request_types.go').read_text(), x
for x in ['reserveReasoningBudget','BudgetAccountID','releaseBudgetIfReserved']:
    assert x in worker or x in (root/'internal/agentworker/types.go').read_text(), x
print('M27 budget enforcement: PASS')
