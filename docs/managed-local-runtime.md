# Managed Local Runtime and Qualification

OnePane owns the lifecycle of managed local inference. Users do not need to install or operate Ollama/llama.cpp separately for the managed path.

## Runtime ownership

A provisioned local model creates durable inventory records in:

- `managed_local_runtimes`
- `managed_local_models`
- `model_deployments`

Starting a deployment creates or reuses one `local_runtime_instances` row. The current managed backend launches `llama-server` on an automatically allocated **loopback-only** port.

The supervisor supplies a deliberately minimal environment and does not inherit arbitrary harness process secrets. The executable and model paths must remain under the harness-owned runtime/model roots.

## Restart identity

Persisted PID is never sufficient proof that a surviving process is the runtime OnePane launched. Startup recovery checks:

- the PID still exists;
- `/proc/<pid>/exe` matches the managed executable;
- command-line model path matches the managed model;
- host remains `127.0.0.1`;
- port matches the durable instance record;
- an independent `/health` probe succeeds.

If process identity cannot be re-established, the instance becomes `orphaned`. OnePane does not signal or trust a PID merely because the integer was persisted before restart.

Graceful daemon shutdown stops attached managed runtimes.

## Empirical qualification

A recommendation predicts fit. Qualification establishes observed compatibility for one concrete `ModelDeployment` on one machine/runtime.

Qualification exercises:

1. baseline chat completion (`L0`);
2. JSON-object behavior (`L1`);
3. JSON-schema constrained response (`L2`);
4. native tool/function call proposal (`L3`);
5. progressively larger context probes, using returned `usage.prompt_tokens` as the authoritative exercised size;
6. baseline request latency and approximate observed completion throughput.

Non-streaming request latency is **not** labeled TTFT. `ttft_ms` remains unset until a streaming first-token probe is implemented.

A passing/limited run creates a `compatibility_profiles` record and writes only the empirically exercised value to `model_deployments.context_max_verified`.

A user-approved managed model may move from `quarantined` to `user_trusted` after successful empirical qualification. This does not self-promote the model into the system-reserved `trusted` class.

## Normal inference path

Qualified managed deployments use the same durable `InferenceRequest` path as cloud models. The `llamacpp` transport resolves the healthy loopback endpoint through the supervisor, pins the registered model identity, disables streaming for the current request protocol, and rejects non-loopback endpoints.

There is no unaudited local-model shortcut around inference state, information-flow checks, Artifact persistence, or scheduler qualification.
