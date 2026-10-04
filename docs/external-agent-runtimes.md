# External agent runtimes / bring-your-own harness

The harness treats an **external agent runtime** as a different kind of resource
from a `ModelDeployment`.

- `ModelDeployment` means: run inference and return model output.
- `AgentRuntimeConnection` means: hand a bounded reasoning/orchestration job to
  another agent system and receive a canonical proposal/result.

The origin harness remains authoritative for Task state, Policy,
CapabilityLeases, Verification, secrets, and ToolGateway execution.

## Operating modes

### `proposal_only`

The external runtime may reason, delegate internally, or produce a plan, but it
may only return one of the canonical `AgentResponse` proposal types allowed by
the request. Tool proposals are not permitted.

This mode is eligible for autonomous scheduling only after the connection has
been explicitly marked `user_trusted` (or a future system-only
`trusted_adapter` qualification path establishes trust).

### `gateway_mediated`

The external runtime may return a `tool` proposal. The proposal is **not** an
execution permit. A future scheduler/agent loop must submit that proposal back
to the origin ToolGateway, where the normal Policy + CapabilityLease checks are
performed.

The external runtime never receives the origin lease or raw secret.

M11 implements the protocol permission for these proposals. It does not yet
implement the M12 scheduler loop that consumes the proposal and resumes the
runtime.

### `unmanaged`

Use this classification for an opaque harness that can execute its own tools or
mutate systems without going through the origin ToolGateway.

An unmanaged runtime is deliberately excluded from autonomous dispatch. Its
registration is useful for inventory, visibility, and a future explicit human
launch path, but the v0.1 scheduler must not use it as an autonomous worker.
Outputs from an unmanaged runtime are not authoritative state.

## Canonical Agent Protocol v1

The built-in adapter is:

```text
builtin.agent_protocol_http@1
```

Default endpoint:

```text
POST /v1/agent/run
```

`endpoint_json` may override the path and timeout:

```json
{
  "base_url": "http://127.0.0.1:9000",
  "invoke_path": "/v1/agent/run",
  "timeout_ms": 120000
}
```

Credentials are referenced using `secret_ref`; raw API keys/tokens are rejected
from `endpoint_json`.

### Request

```json
{
  "protocol_version": "v1",
  "request_id": "agentinv_...",
  "workspace_id": "ws_...",
  "task_id": "task_...",
  "attempt_id": "attempt_...",
  "principal_id": "principal_...",
  "role": "researcher",
  "objective": "Investigate the failing deployment and propose the next action",
  "constraints": {},
  "context": [
    {
      "id": "task-current",
      "kind": "task",
      "trust": "AUTHORITATIVE_DATA",
      "authoritative": true,
      "content": {"state": "running"}
    }
  ],
  "context_manifest": {
    "context_hash": "sha256:..."
  },
  "permitted_proposal_types": [
    "delegate",
    "replan",
    "complete",
    "human",
    "escalate",
    "wait",
    "fail"
  ],
  "tool_callback": false
}
```

### Response

```json
{
  "protocol_version": "v1",
  "request_id": "agentinv_...",
  "proposal_type": "complete",
  "message": "Investigation complete",
  "proposal": {
    "summary": "The service is healthy after configuration correction."
  },
  "usage": {
    "input_tokens": 1200,
    "output_tokens": 180
  }
}
```

A `complete` response means **propose completion**. It cannot set the Task to
`COMPLETE`. The origin harness must still establish Verification, create a valid
Checkpoint, and call the verification-gated completion path.

For `gateway_mediated`, a tool proposal is allowed only when `tool_callback` is
true and `tool` appears in `permitted_proposal_types`.

## Data-flow defaults

External runtimes fail closed to this default:

```json
{
  "max_confidentiality": "public",
  "allowed_residency": ["any"],
  "destination_kind": "untrusted",
  "allow_raw_secrets": false
}
```

Increasing a connection's declared clearance does not bypass the platform
InformationFlow rules. `ORIGIN_NODE` and `TRUSTED_NODES` residency remain
separate from confidentiality.

External runtime output is stored as an immutable Artifact and remains
`UNVERIFIED_DERIVED`. Adapter registration does not make model/harness output
trusted evidence.

## Hermes

A direct Hermes Agent API connection must be treated as **unmanaged** when
Hermes retains its own tool execution. In that configuration, Hermes can perform
tool calls on the Hermes host before returning its response, so the origin
harness cannot claim that those side effects passed through its ToolGateway.

For autonomous use under this harness, put a bridge in front of Hermes that
implements Agent Protocol v1 and enforces one of these boundaries:

1. **Proposal-only bridge:** Hermes is run in a configuration where the bridge
   can guarantee that no side-effecting tools execute, then the bridge converts
   the final reasoning result into a canonical proposal.
2. **Gateway-mediated bridge:** external tool intentions are surfaced as
   canonical `tool` proposals rather than executed remotely; the origin harness
   authorizes and executes them.

Do not rely on prompt text such as "do not use tools" as a security boundary.
The restriction must be enforced by configuration/adapter code.

## Other harnesses

Any external harness can integrate without becoming a first-class dependency if
it can implement the same small HTTP contract. Adapters can later be added for
native protocols (ACP, JSON-RPC, vendor APIs, etc.) while preserving the same
`AgentRequest` / `AgentResponse` semantics and authority boundary.
