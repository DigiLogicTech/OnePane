# Physical CPU evidence — 2026-10-28 Brisbane

Status: **bounded OnePane-managed CPU smoke passed for Windows Test2; broad lifecycle, installed desktop and release acceptance remain open**. Earlier attempts below are retained as historical evidence.

## Windows Test2 managed CPU smoke

Exact final test-installer backend `649fffb25f99d79a0de97ba42d4118d8a7fc078f`, version `0.1.0-alpha.3.3-windows-test.2+649fffb2`, passed normal first-run authentication, managed Gemma3 1B Q4_K_M installation/Agent Check, actual testbed inference and normal abort. CPU-only placement was asserted and independently observed through the owned `b11430/cpu/llama-server.exe`; actual response `READY`,37prompt/3completion tokens,4558ms request including startup. Engine RSS971464704bytes/private270049280bytes is one point observation, not peak or endurance evidence. No production admission or resource override was granted.

The tested backend SHA-256 matches the MSI's extracted payload (`1780c298ab00f712f35d66b129e092f26d973460cf4ad876259aa13fd848c90f`). Model and runtime executable hashes match the previously pinned values. Normal abort left no inference descendant or runtime listener; the backend's separately identified pre-existing console host remained until backend teardown. An independent post-cleanup observation found none of the previously observed test descendants. This corrects the earlier harness's overbroad console-host assertion without relabelling its failed record.

Evidence: `D:\Codex\OnePane\.tools\physical-onepane-cpu-20261028-06\result.json`, with reviewed summary `D:\Codex\OnePane\builds\windows-test-20261028-02\physical-validation.json`. The MSI is a user-authorized current-product test build, not formal RC11. Installed SCM/desktop operation, populated upgrades, queue/swap, active leases, GPU/Colibri/OmniRoute and autonomy remain unverified.

Attempt04 on6ec82154 failed its tree-stop under sandbox restrictions; a disposable owned-process taskkill probe reproduced Access denied. Attempt05 outside that restriction returned real inference and stopped the engine, but exposed CPU-only placement falling through to automatic Vulkan sharding on dual-compatible GPUs. Its harness also misclassified the backend console host as a model descendant. Both raw results remain not passed. Source fix649fffb2 limits automatic sharding to automatic mode or explicit sharding, preserving CPU/hybrid intent and CPU memory limits. Test1 was superseded; Test2 contains the fix.

## Earlier standalone and developer attempts

The existing catalogue-verified Gemma 3 1B Q4_K_M model and an existing llama.cpp b11430 CPU runtime were exercised offline on loopback in a new scratch directory. No existing application, user database, model, runtime installation or service was modified. The source checkout was dirty; the standalone test is not a build from that checkout.

| Evidence | Value |
| --- | --- |
| Model SHA-256 | `12bf0fff8815d5f73a3c9b586bd8fee8e7b248c935de70dec367679873d0f29d` |
| Runtime archive SHA-256 | `b608455b0109793f774537d63d15d4cf2098ddbc5b200ebc9a648e1d85369666` |
| Runtime executable SHA-256 | `1b1614a8cb19f2d3bcb6662db95b9a4e47a665b2065a7274692d48e6fbc7ba36` |
| Placement | CPU, GPU layers 0, context 512, batch/ubatch 32, 2 threads, one slot |
| Readiness / request latency | 3399 ms / 773 ms nonstreaming end-to-end; first-token latency unmeasured |
| Actual response / usage | `READY` followed by newline; 17 prompt, 3 completion tokens |
| Process working set after inference | 965,652,480 bytes |
| Available memory before / during / after | 2910 / 1973 / 2895 MiB |
| Teardown | Exact owned engine identity force-stopped; engine, observed child and listener absent afterward |
| Integrity | Model and executable hashes unchanged afterward |

Independent process/parent and IPHelper loopback listener observations were collected, together with request, actual response, configuration and logs. Local evidence is retained at `D:\Codex\OnePane\.tools\physical-cpu-20261028-standalone-01\result.json`. Forced process termination does not validate OnePane's normal stop, unload or resource reclamation path.

Separately, source commit `6e30ef7ad7823e5b6001517e5db1c317e693377f` was exported with `git archive` into a new QA source root and compiled as `dev-physical-qa.6e30ef7a`, with Go 1.23.12. Archive SHA-256 `ef4ed47742ca1f70e4c9904d420c1a47102c5052fe07b9ef19bb39a4b8973e35`; backend SHA-256 `23412b594ab099f52f9211f0ef6f40b987d5ceac6049ca4be8e6b4993ccb5161`. A no-VCS-stamping archive-build harness issue was corrected with `-buildvcs=false`. Normal first-run HTTP admin setup and `/about` source identity passed in an isolated state root. The QA script then failed to persist a test credential with Windows DPAPI (`Cryptography_DpApi_ProfileMayNotBeLoaded`); this is a harness/environment failure and does not establish a product Vault defect. It stopped its owned daemon. A follow-up process check found no OnePane/llama processes. Evidence: `D:\Codex\OnePane\.tools\physical-onepane-cpu-20261028-01\attempt1-startup-result.json`.

Next physical test: use a fresh scratch root and normal first-run authentication, retaining test credentials only in test-process memory, then exercise production model registration/launch/inference/normal stop without bypassing admission controls. Current launcher controls lack direct batch/thread selection; honor production behavior and record hardware limits. CPU/GPU lifecycle, queue/swap, managed teardown, Colibri, OmniRoute, agents/L4/L5, Windows/Ubuntu installation/upgrades and final release-source acceptance remain outstanding.

A follow-up exact-source developer build from `5b5b2287427d25955ce7f944fbcf212412f94d59` was prepared in `.tools/physical-onepane-cpu-20261028-02`. Its normal-authentication, in-memory-only credential harness exited at the memory preflight:1170MiB available, below the unchanged1800MiB safety minimum. No daemon was launched, model copied or inference requested in that attempt. The result is **not passed**, retained in that directory's `result.json`; rerunning with less available memory or bypassing admission would not establish constrained-hardware acceptance. Build provenance is retained separately in `build-provenance.json` and is not installer provenance.

A fresh retry using the same exact binary in `.tools/physical-onepane-cpu-20261028-03` observed1763MiB and also stopped at the unchanged preflight before launching any daemon or touching model storage. It is another **not passed** result, not an inference/runtime failure. Do not keep retrying with marginal memory; resume the physical protocol when stable sufficient RAM is available.
