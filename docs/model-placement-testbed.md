# M35 Hardware-portable model placement and qualification Testbed

M35 makes model deployment an explicit **model + quantization + runtime + hardware profile + placement** decision. Accelerator memory is not treated as one anonymous pool.

## Placement model

Every managed recommendation persists a `PlacementPlan` with a backend, a placement mode, and the concrete devices/reservations used by that plan.

Supported placement modes are:

- `single_device` — keep the model on one compatible accelerator.
- `layer_sharded` — split layers across compatible accelerator devices.
- `row_sharded` — use the runtime's row-split mode across compatible devices.
- `tensor_sharded` — opt-in/experimental tensor split where the managed runtime supports it.
- `cpu_offload` — accelerator placement with system-RAM/CPU spill.
- `cpu_only` — no accelerator dependency.

The planner never adds heterogeneous devices merely because their capacities sum to a large enough number. Multi-device placement requires a shared supported backend. Each placement device records a runtime device selector, capacity, allocation and share so residency accounting and runtime launch use the same plan.

The signed runtime catalogue may provide managed builds for `cpu`, `cuda`, `rocm`/`hip`, `vulkan`, `sycl`, `metal`, `opencl`, `musa`, and `ascend`/`cann`. The Linux appliance detector natively probes CPU features, NVIDIA/CUDA, AMD/ROCm, and generic PCI accelerators with SYCL/Vulkan/OpenCL availability where those tools are installed. The domain/runtime contract also represents Metal and Ascend/CANN without treating CUDA as the definition of an accelerator. Hardware that cannot be safely sized by a native trusted probe is not admitted to a placement merely because an advisory source says it exists.

## llmfit advisory input

A node may configure a loopback llmfit server with `local_ai.llmfit_url`. OnePane consumes llmfit model-fit information as **advisory metadata only**. It can contribute model/quant fit, context estimates, memory/disk estimates, capability identifiers, TP support and estimated or measured performance.

These values retain `llmfit` provenance. They do not replace:

- OnePane's signed model/runtime catalogue,
- the target node's native hardware/storage checks,
- OnePane's empirical deployment qualification, or
- human Testbed admission.

This distinction is deliberate because llmfit itself supports hardware overrides/simulated target profiles. Those are useful for planning but are not proof of the physical node OnePane is about to schedule.

## Model spec sheet

After successful automated qualification, OnePane creates or refreshes a durable `model_spec_sheets` row. The API representation separates:

- catalogue/model claims,
- llmfit advisory data,
- exact quantization and managed runtime/backend,
- exact hardware profile and placement plan,
- requested versus empirically verified context,
- measured TPS/TTFT from qualification,
- manual restrictions and admission state.

A changed hardware profile or placement invalidates the previous manual admission and returns the deployment to `pending`.

## Manual Testbed

The Testbed is an operator-facing pre-production trial of the **exact installed deployment**. Testbed sessions and turns are durable. A turn can use a normal prompt or bounded raw request JSON.

A synthetic tool probe exposes only a fake `onepane_test_probe` definition to inspect tool-call formatting. It never invokes ToolGateway, receives no CapabilityLease, and cannot mutate or send anything externally.

Automated qualification proves that a runtime/deployment is technically usable. It is intentionally insufficient for production admission. `accepted` and `restricted` admission require at least one completed manual Testbed session bound to the **current hardware profile and placement plan**. A trial from an older placement cannot admit a reconfigured deployment.

Admission states:

- `pending` — technically qualified but not eligible for ordinary managed scheduling/federation.
- `accepted` — eligible subject to normal scheduler/policy rules.
- `restricted` — eligible with durable operator restrictions.
- `rejected` — unavailable for normal managed use.

Restrictions currently enforced generically by the scheduler include a maximum context cap, denied capability IDs, and disabling model tool callbacks. `deny_use_cases` and free-form notes are retained in the profile for operator/UI policy; they are not treated as a universal scheduler primitive until the routing request carries a canonical use-case dimension.

## Remote-node operation

A paired peer does not gain model-management authority merely by being trusted for inference. The M34 per-peer model-management grant remains required.

With that grant, the master can use the same spec/Testbed/admission workflow against a remote deployment. Federation authenticates the peer with pinned mTLS, while the target node:

1. owns and executes the actual managed runtime,
2. runs Testbed turns locally,
3. persists the target's spec/admission state locally, and
4. gates federation export/inference on that local admission state.

The origin/master is therefore an operator surface and coordinator, not a relay for model bytes or runtime execution.

## API

Local deployment:

```text
GET  /v1/model-deployments/{deploymentID}/spec-sheet
POST /v1/model-deployments/{deploymentID}/testbed/sessions
GET  /v1/model-testbed/{sessionID}
GET  /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/complete
POST /v1/model-deployments/{deploymentID}/admission
```

Remote deployment through the paired-node management channel:

```text
GET  /v1/nodes/{nodeID}/models/{deploymentID}/spec-sheet
POST /v1/nodes/{nodeID}/models/{deploymentID}/testbed/sessions
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/complete
POST /v1/nodes/{nodeID}/models/{deploymentID}/admission
```
