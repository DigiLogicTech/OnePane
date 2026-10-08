# OnePane Alpha 3.3 RC1 — combined operator QA

**Candidate branch:** `release/alpha3.3-consolidated-qa`  
**Candidate version:** `0.1.0-alpha.3.3-rc.1`  
**Scope:** Alpha 3.2 remedial fixes + Colibri tiered memory and hot swap + readable model Spec Sheet + Web Chat multi-provider chaired Research Councils + updated tour.

This is a **QA prerelease**. Do not replace `alpha3.2-windows-known-good` or promote a stable release until operator QA is completed and the exact candidate commit is approved.

## 1. Installer and data preservation (Windows x64)

- [ ] Record current OnePane executable version and model pool / project locations before install.
- [ ] Install Alpha 3.3 RC1 over the previous build. Verify installer reports its RC version and service starts.
- [ ] Confirm Project data, models, credentials and previous layouts are intact; install does not fill shared SYSTEM/global temp locations.
- [ ] Check app reload, OnePane Chat, all left menu routes, Command palette, inspector and observability drawer.
- [ ] Confirm every page, especially Nodes, Settings, Web Chat, Operations and Workspaces, adapts to opening/closing and resizing the log drawer.
- [ ] Run Help / Tour from sidebar, including skip/back/replay. Verify spotlight alignment, panel position and new feature descriptions.
- [ ] Check theme-aware buttons, selectors and focus in Light, Dark, Midnight and Graphite.
- [ ] **Do not uninstall production models or delete projects** just to test preservation. Use disposable data for destructive tests.

## 2. Models, Colibri and spec sheet

- [ ] Local Models lists installed and recommended models correctly; duplicate installations are blocked.
- [ ] Open a known llama.cpp model Spec Sheet: backend, verified context, qualification, measured speed/TTFT and Agent Check remain readable and unchanged.
- [ ] Open a Colibri model Spec Sheet: the new **Configured placement** section shows Automatic/Balanced/Manual, backend, GPU index, warm RAM GB, expert cap, re-pin and Vulkan VRAM experts.
- [ ] Confirm the **Runtime residency** section reports *whole-model* status and does **not** invent observed VRAM/RAM/SSD expert-cache memory.
- [ ] View read-only Colibri plan when available. A plan is an *estimate*, does not launch the model, and is not labelled a benchmark.
- [ ] Save Colibri tier settings, reopen Spec Sheet and verify they persist after navigation and app restart.
- [ ] Validate Auto/CPU/CUDA/Vulkan appropriately for the installed runtime. Unsupported backend choices must fail visibly instead of silently falling back.
- [ ] Run Agent Check with CPU and (when supported) GPU placement; verify model is drained/unloaded after check and results update the Spec Sheet.
- [ ] If two compatible Colibri models are available: hot swap sequentially; confirm only one remains resident on the node and previous child processes release RAM/VRAM.
- [ ] Attempt a swap during an active request (on disposable test models); confirm busy protection, then failed-swap rollback where safely reproducible.
- [ ] Verify Colibri Hot Swap never changes the model selected in OnePane Chat.
- [ ] Verify Tiering and Hot Swap controls work for **every installed Colibri model**, not only the first entry.
- [ ] Confirm installed llama.cpp models and Colibri registrations are retained when managed runtime components are removed or disabled.
- [ ] Run representative benchmarks on the actual Windows GPU; generic CI success does not prove Colibri performance or backend compatibility.

## 3. Web Chat and manual Research Council

- [ ] Web Chat sidebar button opens its own page; OnePane Chat Assistant/Project Orchestrator remain unchanged.
- [ ] Create two ChatGPT tabs plus another provider; switching preserves separate prompt/response drafts and Council seat assignment.
- [ ] Start Web-only Council with 2–3 manual research seats, one **manual AI Chair**, a selected synthesis seat, 1–2 critique rounds and **human approval enabled**.
- [ ] Confirm **Chair agenda** is queued before participant Round 1. Paste a real Chair-model response; Council must remain blocked pending approval.
- [ ] Edit and approve Chair agenda; verify Round 1 research prompts include only approved scoped agenda plus objective, not independent peer answers.
- [ ] Submit each independent provider response. Confirm only the exact matching member/session's turn completes.
- [ ] Confirm Chair review questions are prepared after the round finishes and next round stays blocked pending review/approval.
- [ ] Approve follow-up questions; verify they are propagated to the *next* round without modifying prior prompts.
- [ ] Confirm final synthesis goes to the **explicitly selected participant**, not the Chair unless intentionally configured as a research seat elsewhere.
- [ ] Run Council **without Chair** and confirm original deterministic workflow still functions.
- [ ] Confirm partial setup resume, New Conversation generation, Chair approval/provenance hashes, queue refresh and closing/reopening Web Chat tabs.
- [ ] Verify unsupported local/API hybrid Chair options are disabled in the Web-only wizard, not silently substituted.
- [ ] Confirm a Web-only Council backing Task cannot be executed through the normal Task lifecycle.
- [ ] Review every outgoing prompt for project-sensitive data before manually pasting into an external cloud-provider website.
- [ ] Confirm provider websites open in separate browser tabs; they are not embedded or scraped.

## 4. Node, navigation and workload regression

- [ ] Nodes report hostname, health, metrics and hardware accurately and remain drawer-aware.
- [ ] Verify Agents, Skills, Settings, Model Routing and Discovery actions remain usable after changing tabs.
- [ ] Confirm Settings stays central, theme-aware and retains existing saved values.
- [ ] Verify Model Routing actions remain right-aligned and installed models are not offered for reinstallation.
- [ ] Run an ordinary local model (non-Colibri) and cloud-API model without Web Council; confirm scheduling and Agent Check are unaffected.
- [ ] Confirm Windows setup/uninstall/reinstall and Ubuntu package/upgrade preservation tests from CI are green.

## Release decision

Mark this RC **approved** only after the specific build SHA, manual screenshots/findings, installer identity and any failed QA items are recorded. Fix failures on a subsequent candidate tag rather than replacing the immutable RC1 assets. Promotion of the known-good branch is a separate, reviewed step.
