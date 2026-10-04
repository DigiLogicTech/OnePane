# Local AI Bootstrap

The finished product should be useful immediately after installation even when the user has no existing Ollama, llama.cpp, LM Studio, or vLLM setup.

The Local AI bootstrap path is therefore owned by the harness:

1. detect the local HarnessNode hardware and software environment;
2. persist a hardware fingerprint/profile;
3. score local models by use case, fit, quantization, context envelope, estimated speed, and disk headroom;
4. let the user choose a recommendation or accept a role-oriented default;
5. install a harness-managed local runtime (llama.cpp is the default managed backend);
6. download the selected model artifact;
7. verify runtime/model integrity where trusted digests are available;
8. register Model + ModelDeployment objects;
9. empirically qualify the deployment before making it schedulable;
10. route suitable workloads locally before consuming cloud or subscription allowance.

## Hardware detection

The Linux detector records CPU, logical cores, architecture, total/available RAM, storage, kernel/OS information, GPU vendor/name/VRAM, driver information where available, acceleration backend, and existing local runtime probes.

The first detector supports NVIDIA (`nvidia-smi`), AMD (`rocm-smi`) and Intel discovery paths. A hardware fingerprint excludes volatile free-memory/free-disk values so ordinary utilization changes do not create a new machine identity, while material hardware/driver changes can trigger requalification.

## Recommendation semantics

Recommendations are planning estimates, not proof that a ModelDeployment is operational. Fit is quantization-aware and considers model weights plus a conservative runtime/context allowance.

Fit bands are deliberately conservative:

- `perfect`: <= 60% of the selected memory pool;
- `good`: <= 85%;
- `marginal`: <= 98%;
- `too_tight`: rejected.

The planner distinguishes full GPU, MoE, CPU/GPU offload, and CPU-only execution. CPU/offload paths are capped below `perfect` because fitting in RAM is not equivalent to high-quality GPU residency.

The planner also reserves disk headroom and download scratch space. A model is not recommended merely because VRAM is sufficient if installing it would overfill the model volume.

Role/use-case categories currently include general, coding, reasoning, chat, embeddings, and routing. The shipped catalog is intentionally small in the scaffold; finished releases should consume a signed, updateable model catalog rather than hard-coding a stale internet model list into control-plane code.

## Managed downloads

Managed downloads are HTTPS-only, size bounded, written atomically, and SHA-256 checked when the trusted catalog supplies a digest. Runtime archives reject absolute paths, traversal, symlinks, and unsupported archive entry types.

A managed runtime requires a trusted SHA-256 before installation. A model artifact without a trusted digest may be downloaded, but the resulting `Model` is registered `quarantined` and the deployment remains `qualifying`.

## No install-equals-trust shortcut

`ProvisionApprovedPlan` intentionally stops at `DeploymentQualifying`. The later qualification subsystem must start the runtime and empirically test protocol behavior, context limits, tool/schema behavior, stability, and observed performance before the scheduler may rely on it.

This preserves the project rule that compatibility is deployment-specific and empirical.

## Relationship to LLMFIT

The user experience borrows the useful concepts from LLMFIT—hardware detection, fit bands, quantization selection, speed estimates, use-case recommendations, runtime awareness, and storage planning—but the harness does not require users to install LLMFIT. LLMFIT can later be supported as an optional catalog/benchmark import source.

## Managed runtime supervision and qualification

The bootstrap path is now connected to a managed `llama-server` supervisor and empirical qualification. See [`managed-local-runtime.md`](managed-local-runtime.md).

A provisioned model remains `qualifying` until the harness starts the concrete deployment, probes protocol behavior and exercised context, creates a compatibility profile, and moves the deployment to `ready`. Qualified managed deployments then execute through the ordinary durable `InferenceRequest` transport path.
