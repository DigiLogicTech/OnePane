# OnePane Alpha 3.3 RC2 — Focused model QA remediation

Based on initial Windows x64 Alpha 3.3 RC1 hands-on QA, the installer, layouts, themes, tour, and navigation passed. This RC focuses **only** on blockers found in Models and Agent Check. The known-good baseline and RC1 prerelease are unchanged.

## Fixes

1. **Q4_0 exact model installation:** Add Q4_0 to the managed planner's quantization/bytes-per-parameter table. The checkbox **Proceed despite estimated RAM/VRAM limits** continues to bypass resource-fit estimates only. The exact Q4_0 model artifact must still be present in the verified, digest-pinned catalogue and satisfy physical storage checks. It does **not** override a requested unsupported GPU-only placement or artifact trust.
2. **CUDA Agent Check:** When an older llama.cpp runtime generation was removed/repaired, an installed model could still refer to the obsolete `b11430/cuda/llama-server.exe` path. A missing configured executable now resolves only to the currently registered, existing executable for the **same backend**, inside the managed runtime root. No CUDA→CPU/Vulkan substitutions. Model weights and compute policy are unchanged. A missing/invalid installed binary is marked unavailable in runtime status and given an explicit **Repair runtime** message.
3. **Failed Agent Check cleanup:** A failed testbed launch now records the testbed lifecycle as `cancelled`, rather than `failed`, matching the SQLite CHECK constraint. Its failure explanation remains on the Spec Sheet and idle memory is released where possible; the testbed is not promoted into a successful qualification.
4. **Colibri register folder:** Replaced browser-native `prompt()`/manual model-name popup with a OnePane-styled form: browse/select a model folder, absolute path, model reference/name, display name and context. UI rejects selecting just the `Models` parent directory; server continues enforcing the configured pool boundary and a required `config.json`.

## Retest on Windows x64

- [ ] **Q4_0:** Reopen Qwen3.5-9B Download & Install with Q4_0, Auto compute and *resource override enabled*. Confirm exact quantization is honoured; download is allowed **only if** verified digest-pinned Q4_0 exists and physical disk space is sufficient. Do not silently change quantization.
- [ ] **CUDA:** On a known installed test model, Run Agent Check. If the selected managed CUDA runtime is damaged or its dependencies are missing, use **Models → Local → llama.cpp Settings → Repair runtime** and recheck. If neither the pinned nor registered CUDA executable exists, OnePane must show a clear error rather than quietly try a different backend.
- [ ] **Testbed:** Confirm the same failed launch no longer surfaces `CHECK constraint failed: status IN ('active','completed','cancelled')`. Spec Sheet must preserve the infrastructure failure and not claim a successful Agent Check. Ensure no orphan CUDA/CPU process remains.
- [ ] **Colibri UI:** Open **Local → Colibri → Settings → Register model folder**; confirm themed form, folder picker and helper text. Selecting the bare `Models` directory is rejected. Selecting a valid model subdirectory with `config.json` should register it; then use its tiering, plan, residency and Agent Check controls.
- [ ] Confirm previous Alpha 3.3 RC1 checks that passed—install/upgrade data retention, navigation, drawer-aware pages, tour, all themes and OnePane Chat—still pass.

**RC2 code is a draft on `fix/alpha3.3-qa-models-rc2`; Windows and Ubuntu installed-product CI gates must both pass before publishing the prerelease.** Real CUDA launches and Colibri models on your Alienware remain manual QA requirements.
