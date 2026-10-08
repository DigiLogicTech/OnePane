# Colibri tiered inference (Alpha 3.2 feature branch)

OnePane uses Colibri's existing expert residency engine rather than implementing a second tensor pager. Colibri is a **separate managed inference runtime**; it is not a llama.cpp GGUF backend. Only models explicitly registered as Colibri model directories have these controls.

## Model-specific controls

Open **Models → Local → Installed Models → Tiering** on a Colibri model.

- **Automatic**: When the installed package contains the `coli` launcher, OnePane runs `coli serve --auto-tier` so Colibri applies its RAM/VRAM placement plan. The legacy Python server remains the compatibility fallback for archives without `coli`.
- **Balanced**: Adds Colibri's `balanced` adaptive expert-cache policy and its default re-pin interval, with the same automatic planning behaviour where available.
- **Manual**: May set the Colibri expert RAM budget (`--ram`/`RAM_GB`), per-layer expert-cache cap (`--cap`), re-pin interval, GPU backend, GPU index and Vulkan expert residency count.

The default backend is **auto**. Explicit CPU disables both GPU paths. Explicit Vulkan requires the Vulkan-capable Colibri build and compiled shaders; explicit CUDA requires a supported CUDA backend. A failed backend must be surfaced rather than silently replaced.

**Important**: Colibri's expert weight files live on storage and can stream from SSD, but latency depends on hit rate, expert selection, disk throughput, and model family. A tiered model is not guaranteed to perform better than a smaller llama.cpp model.

## Whole-model hot swapping

OnePane adds **Hot swap** next to **Tiering** for each registered Colibri model.
The former selects the resident Colibri deployment; the latter configures how
that single model uses hot GPU VRAM, warm system RAM and cold model files.

- **Automatic:** Ordinary inference acquisition for a different Colibri model
  uses the same node-scoped hot-swap admission path as the button.
- **Explicit:** Hot swap activates a chosen model ahead of a chat request;
  it does **not** silently change the OnePane Chat assistant's routed model.
- **Memory:** At most one Colibri model remains resident per node in this
  initial release. The model files remain on SSD after eviction. Launching a
  new model still incurs initialization and cache warmup latency.
- **Busy protection:** No currently serving Colibri model is interrupted.
  Active requests, startup, shutdown and orphaned process states block a
  conflicting activation before any other model is evicted.
- **Rollback:** If the new model cannot become healthy, OnePane first attempts
  to drain the failed replacement and then restore the most-recently-used
  previously healthy resident. If safe teardown cannot be confirmed, rollback
  is withheld to prevent overcommitting VRAM.
- **Windows shutdown:** Managed Python launchers may own heavyweight child
  inference engines. The supervisor terminates the verified process tree
  rather than just the parent executable; acceptance testing still needs to
  prove GPU and RAM are actually released after each switch.

The one-active-Colibri policy is deliberately conservative for systems such
as the Alienware GTX 1060 / 16 GB RAM. It does not evict other inference
engines, such as llama.cpp, or manage arbitrary third-party GPU processes.
Cross-runtime GPU admission and warm preloading of more than one large model
require separate resource accounting and validation.

**API:**
```text
GET  /v1/local-ai/deployments/{id}/colibri-swap?workspace_id=...
POST /v1/local-ai/deployments/{id}/colibri-swap
```
POST body: `{"workspace_id":"..."}`. Requests require the deployment's own
workspace authorization and model-write capability.

## Lifecycle, ownership and security

Tier settings are persisted under `model_deployments.runtime_config_json.colibri_tier`; no database migration is required. Existing deployments without that key retain their previous process-launch behaviour until the operator saves a tier profile. New registered Colibri models default to automatic mode.

- Per-deployment, not global; workspace authorization checks the deployment's actual workspace.
- Settings never modify other model deployments or llama.cpp controls.
- A change can stop an **idle** supervised Colibri process so the next inference loads new settings; changes are refused if a request is running.
- The tier configuration never takes untrusted shell commands or paths. Existing approved managed model and runtime paths remain authoritative.
- Plan execution is read-only, time-bounded, and only uses an installed, in-root `coli` launcher.
- **No model downloads, tuning benchmarks or Colibri installation are automatically triggered by the settings dialog.**

### Internal APIs

```text
GET   /v1/local-ai/deployments/{id}/colibri-tier?workspace_id=...
PATCH /v1/local-ai/deployments/{id}/colibri-tier
GET   /v1/local-ai/deployments/{id}/colibri-plan?workspace_id=...
```

PATCH body: `{"workspace_id":"...", "settings":{"mode":"automatic","backend":"auto","ram_gb":0,"expert_cap":0,"vulkan_experts":96,"gpu_index":0,"repin_tokens":0}}`.

The read-only plan endpoint requires the `coli` Python launcher to be present. When it is absent, the UI identifies the limitation and the legacy serving integration remains usable.

## Windows / Alienware validation before release

1. Preserve the existing known-good Windows build and test on a separate branch.
2. Install Colibri through OnePane and register a supported Colibri MoE model directory within the selected managed model pool.
3. Detect GTX 1060 GPU and confirm the installed backend supports the selected Vulkan/CUDA mode **before** requesting GPU allocation; a Pascal GTX 1060 does not imply compatibility with every CUDA binary.
4. Start with Automatic, 4K–8K context, and do not override system RAM unless Colibri's plan indicates there is adequate headroom.
5. Run `coli plan --json` where present, then Agent Check; verify launch identity, health, and release of idle memory.
6. Record first-token latency, decode tokens/s, peak RSS/VRAM, disk reads, cache hit rate and failure logs; compare with the current llama.cpp model.
7. Exercise busy-update refusal, unsupported-backend startup error, restart and settings persistence.
8. Activate two Colibri models consecutively and confirm the first process **and its child engine** release RAM/VRAM. Attempt a hot swap during active inference and verify a conflict without eviction; test rollback after a failed replacement. Keep Linux package CI green.

Colibri's `coli tune` profiling and a model download/format-conversion wizard are **separate future integration tasks**, not implied by this feature. Do not report the tier as tuned until a measured profile exists.

Upstream references: https://github.com/JustVugg/colibri/blob/v1.12.1/docs/SETTINGS.md and https://github.com/JustVugg/colibri/blob/v1.12.1/docs/ENVIRONMENT.md.
