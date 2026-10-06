# Managed component network policy

OnePane managed components run with explicit capability profiles. Component health is subordinate to harness health; failure or quarantine of a component must not make harnessd unhealthy.

## Colibri

- outbound network: allowed
- trusted OnePane node connectivity: allowed
- local GPU: allowed when qualified
- OnePane model pool: read access
- unsolicited inbound access: denied by default
- arbitrary host filesystem: denied
- remote execution targets must be enrolled, authenticated, healthy and qualified by OnePane before the scheduler may use them

Colibri network access supports large-model execution on qualified OnePane nodes. Network reachability alone never establishes node trust.

## OmniRoute

- outbound network: allowed
- provider HTTPS/DNS: allowed by policy
- credentials: brokered by OnePane
- trusted-node capability: not required by default
- local GPU/model pool: not required by default
- unsolicited inbound access: denied by default
- arbitrary host filesystem: denied

## Hot Swap

Hot Swap is a OnePane orchestration capability, not a runtime. Active task state is preserved independently of the selected inference target. A task may move among compatible local/native, local/Colibri, qualified remote-node/Colibri, and policy-permitted provider targets. Runtime transitions may increase latency but do not by themselves fail the task. If a target becomes unhealthy, OnePane should quarantine it and select another qualified target; the task becomes blocked only when no policy-permitted qualified path remains.
