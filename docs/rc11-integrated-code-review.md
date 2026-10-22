# RC11 consolidated code-review and vision-alignment gate

Integration branch: `feature/rc11-policy-worker-diagnostics-integration-20261011`, based on policy head `891ff8daa2514e56bbd1ebbfcd048f691712d785`.
Combines PR #224 (policy runtime), #225 (Worker assurance) and #226 (diagnostics)
without merging any of them into main or packaging an installer.

## Integration deliverables
- Persisted Project/Workspace/Task overrides checked at model, file tool and
  autonomous Worker execution boundaries, including revocation rechecks.
- Active Workspace membership, Task/Attempt ownership, rollback and restart tests.
- Redacted policy diagnostics and operator-initiated offline Debug Centre viewer.
- Go unit/integration coverage, frontend security scripts and embedded WebUI tests.

## Acceptance gates
- [x] Integrated code commit `8c2fdc429118e84850d92f6ff81a590f3e55d14d` passed [CI run #38118216676](https://github.com/DigiLogicTech/OnePane/actions/runs/38118216676), including Go/SQLite inference, tool and Worker tests, WebUI privacy scripts and Windows checks.
- [x] First assistant code/security review on combined diff, including transactional Worker and diagnostic hardening. User sign-off remains a separate gate.
- [x] User vision alignment interview: all six decisions explicitly confirmed and recorded.
- [x] Post-vision source-level gap review: `docs/rc11-second-review-gap-matrix.md`.
- [ ] Implement all blocking gaps, then repeat the final security/code audit and remediate.
- [ ] User-approved build, install/upgrade regression and manual Windows/Ubuntu QA.
- [ ] Only then consider release merges and packaging.

## Known limitations for vision review
1. Task-less chat or direct inference uses legacy authority and data-flow
   controls, not the persisted canonical Project/Workspace feature-policy chain.
2. Explicit file Tool Gateway actions are feature-policy protected; generic
   shell, Git, and other sandbox tools use pre-existing authority and sandbox
   restrictions. Operation classification needs comprehensive review.
3. Agent-profile independent feature settings are not yet persisted in this
   chain; Task execution overrides restrict but cannot expand parent grants.
4. The Debug Centre policy viewer is offline/opt-in, not a live policy event tap.
5. Hosted CI cannot establish physical Node, packaged installer, model GPU or
   visual theme/drawer QA. This is a **draft code review**, not QA-ready release.

## Initial technical audit
The preliminary code audit and decision register are in `docs/rc11-integration-code-audit.md`. Product alignment, second audit and installed-build QA are not complete.

## Review checkpoint (2026-10-11)
Assistant implementation audit and corrective tests are complete. Latest application-code CI passed (#38118997403). See docs/rc11-integration-code-audit.md and docs/rc11-vision-decision-register.md for remaining design approvals; no release merge has been performed.

## Second-review checkpoint

All six product decisions are user-confirmed; this is an approved design baseline, **not** release approval. See `docs/rc11-second-review-gap-matrix.md` for P0/P1 implementation and safety gaps. Provider-backed deployments cannot claim free local scheduling based on a local Node record or unverified deployment cost metadata (green CI #38122612493). Trusted-node scheduler opt-in and pairing verification are being tested, but the Worker/API authorization pipeline is not yet wired to issue the required verified transfer grants. No installers or release merge are approved.

## Post-vision implementation checkpoint (2026-10-11)

The six design decisions are all approved. Canonical paired-Node transfer policy, human-only privilege expansions, default L4/explicit L5 project modes, a nonactivating continuous-goal registry and configuration UI/API, plus typed five-capability tool gateway checks have been committed with negative-path tests. Latest independently confirmed green runs include Worker admission [#38125542945](https://github.com/DigiLogicTech/OnePane/actions/runs/38125542945) and continuous-goal API [#38125753168](https://github.com/DigiLogicTech/OnePane/actions/runs/38125753168). Newer UI-head CI must finish before being marked green. The second source-level gap review is maintained in `docs/rc11-second-review-gap-matrix.md`. Final implementation-complete audit, approved installers and real hardware QA remain outstanding.


## Interim security code-review evidence — managed model startup (2026-10-21)

This is an **incremental source audit**, not the mandated final whole-branch review or operator sign-off. The code reviewed was `internal/localai/supervisor.go`, its managed-startup SQLite transitions, explicit Colibri compensation, and their native/cross-platform CI paths. PR #227 remains draft.

**Finding 1 — stale health result could write `healthy` after stop or replacement (remediated).** An in-flight `/health` request formerly updated the runtime instance by ID alone. An independent SQLite stop, orphan reconciliation or changed PID/revision could be overwritten by a later response. `confirmStartingHealthy` now requires the exact starting status, revision, PID and command fingerprint; it rejects zero-row CAS. The symmetric `failStartingInstance` prevents a stale timeout from demoting another process. New SQLite tests cover status, revision, PID and fingerprint replacement and both orders of conflicting healthy/failed transitions. Native Windows plus Linux/integration CI for the initial fix: [#39548410301](https://github.com/DigiLogicTech/OnePane/actions/runs/39548410301), success.

**Finding 2 — cancelled request could still spawn a new model after its startup row was persisted (remediated).** `startLocked` used a background context for process startup because a resident model must live beyond a single chat; this property is preserved. A cancellation preflight now fences the new process-creation boundary. If cancellation or `runtime-home` creation prevents launch, a detached but bounded transaction CAS marks only that NULL-PID original starting generation failed, preventing a phantom “loading model” or accidental update of a newer launch. The existing post-spawn PID persistence/orphan and Colibri verified compensation remain in force if cancellation races with the actual OS creation. This is not an atomic, cross-process cancellation lease.

**Finding 3 — process could change during an HTTP readiness probe (partially remediated).** `waitHealthy` verifies managed process identity before **and after** the HTTP request, and then uses the exact-generation CAS to commit readiness. This reduces the PID-reuse gap but does not hold an OS kernel PID handle across the entire probe, attest GPU resources, or substitute for physical process-tree acceptance. A source-order regression enforces both checks.

**Review outcome:** no new model stop/kill authority, cloud fallback or remote Node privilege was added. Remaining review must inspect storage/ACL/reparse TOCTOU, cross-process scheduler/Worker boundaries, tool capabilities and default L4/opt-in L5, integrated UX, real Windows SCM+data-root rollback and genuine CPU/Colibri process/memory isolation. Do not mark this preliminary review complete as a release gate, package an installer, or merge based on hosted tests alone. The final code/safety audit and mandatory LAST operator-focused UI/UX review remain independently required.

## Follow-up source-security checkpoint — managed-model process identity (2026-10-21)

The current head contains a narrow, fail-closed hardening of the managed
llama.cpp/Colibri PID ownership predicate. Earlier checks accepted an
otherwise valid `-m`/`--model`, `--host` and `--port` argument pair
even when the inspected command carried unexamined `--flag=value` or
fused-short overrides. Host and port strings could also pass through
filesystem-path normalization. Colibri only required its script path
*somewhere* in the Python argv, leaving a `python -c`/module/program
impersonation ambiguity.

- [x] `0fbb1ece`: deny alternate inline/fused argument spellings and match
  TCP host/port literally, while preserving model-path normalization.
- [x] `ca797689`: negative tests for host/model/port override and
  noncanonical scalar arguments.
- [x] `42b2a580`: bind a Colibri Python process to OnePane's actual
  script operand position (including the approved `py.exe -3` form);
  reject option terminators.
- [x] `aa34b0f1`: regressions for Python inline-code/module impersonation,
  script misplacement and post-terminator option smuggling.
- [x] `cf6c8614`: explicitly run managed-process identity tests in
  native Windows CI and Linux `-race` lane.
- [x] **Exact-source/workflow CI verified:** [run #39553844948](https://github.com/DigiLogicTech/OnePane/actions/runs/39553844948)
  completed successfully for `cf6c8614`, including native Windows managed
  PID identity tests, Linux `-race` identity tests, full Go regression,
  and cross-platform source compilation. Physical Node acceptance remains
  intentionally skipped: none of this verifies GPU/VRAM reclamation, actual
  installed process identity after Windows crash, or a real Colibri model.

The change does not claim OS-kernel-held process identity, ownership of a
recycled PID on Windows after a service crash, a proven bound GPU child/process
tree, or actual Colibri VRAM release. The existing deny/reconcile path for
untracked Windows PIDs is intentionally unchanged. Whole-branch security
review, real constrained-hardware acceptance, non-C: Windows upgrade/reboot/
rollback, paired Nodes, opt-in Level 5 effects, mandatory LAST UI/UX review,
operator installer approval and PR merge remain outstanding.

## Managed recovery fencing — interrupted inference, drain and unknown PID (2026-10-21)

This incremental review follows the process-identity hardening checkpoint. It
does **not** replace the final whole-branch audit or physical installation QA.

**Code and regression work:**
- `f4ddfbe6` + `c3ebbd66`: after restart, repeat live model PID/CLI identity
  verification *after* the loopback health response and before committing
  readiness; promote only an original active generation using status, revision,
  PID and process-fingerprint CAS. The initial intermediate commit could not
  compile independently until the newly introduced CAS helper was added.
- `6f146332`: SQLite mutation regressions for stop, orphan, changed PID,
  revision and command fingerprint; source-order revalidation assertion.
- `ec5ed67a` + `60acb740`: recovery snapshot now carries exact status;
  first orphan transition is conditional on that status and the original
  NULL/non-NULL PID. A crash-interrupted `draining` runtime is orphaned without
  probing or signalling instead of quietly resurrected on HTTP readiness.
- `785a38be`: explicit native Windows and Linux race-lane recovery regressions.
- `02beb33b` + `7db8f14d` + `b2277a57`: persisted `busy` after crash
  represents an unknown inference outcome. Never mark that model reusable on a
  healthy socket; retain the PID in an orphaned record for reconciliation,
  without automatically repeating the request or sending a signal.
- `5948756b` + `b42f6daf`: when examining *other* model reservations on
  the same Node, a corrupt non-NULL PID (including zero, negative and PID 1)
  is not evidence that a failed/stopped process released memory. Reject the
  admission and require reconciliation; a confirmed pre-spawn failure with
  SQL NULL PID remains eligible for retry.
- `b533ea9f` + `dba77b89`: a target model left `orphaned`/`draining` with
  NULL/invalid PID cannot be assumed unlaunched or memory-free. Block its
  restart rather than overwrite the evidence with a second engine. A verified
  pre-spawn cancellation that records `failed` with NULL PID remains
  independently retryable.

- `cd315fe6` + `5b55054b` + `de5c439d`: repeat the managed model
  process identity check after the durable `draining` update but before
  SIGTERM, and enforce ordering in Linux/native Windows tests. A recycled or
  otherwise unverified PID is orphaned for reconciliation rather than
  signalled. This does not create kernel-held PID references or prove that a
  Colibri GPU child has also exited.

**Verification:** the combined source/workflow head `60dc7755` passed
[hosted CI #39578352208](https://github.com/DigiLogicTech/OnePane/actions/runs/39578352208),
including native Windows managed-process/SQLite checks, Linux race tests,
full Go regression and cross-compilation. This is not physical acceptance.
These tests do not prove real process-tree/VRAM release, handle-based Windows
PID ownership after crash, service restart with populated non-C data, or
safe unattended reconciliation. The system deliberately fails closed where
actual external effects remain unknown. Final review, explicit L4/L5 authority
checks, physical Node/model acceptance, mandatory LAST UI/UX review and
operator-approved installer are still separate gates.

## RC11 P0 — no-child provenance and conservative Stop/memory admission (2026-10-22)

This is another **incremental software code-review checkpoint**, not the
mandatory final source/security audit or installed hardware QA.

- [x] **Unknown PID stop fence** (`a8a1e521`): manual Stop and daemon
  shutdown no longer rewrite a `starting`, `draining` or `orphaned`
  model with no trustworthy PID as successfully `stopped`. A missing
  child PID after a crash or SQLite failure cannot prove the process
  and its potential GPU/Colibri children exited. SQL status, revision,
  residency and operator-reconcilable evidence remain intact. A
  confirmed prelaunch `failed` model remains explicitly stoppable.
  [Windows/Linux CI #39699667254 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39699667254).
- [x] **Failed/NULL-PID memory provenance** (`886737eb`): failed
  instance without a PID is eligible for new local memory capacity,
  restart or cleanup **only** if its stored failure reason is the exact
  bounded pre-spawn cancellation/launcher-creation failure and it
  has no child start timestamp. Generic failed records (including
  legacy/unknown outcomes) remain blocked pending operator
  reconciliation. The canonical reason is bound by the guarded
  prelaunch SQL write. Tests cover absent, wrong and inconsistent
  provenance plus known-safe retries.
  [Windows/Linux CI #39700172492 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39700172492).
- [x] **SQL NULL versus recorded zero** (`14b3dab1`): a private
  `RuntimeInstance.pidRecorded` flag preserves actual SQL NULL
  provenance across instance reads; a corrupt persisted `pid=0`
  cannot impersonate a no-child failure. This flag is not serialized
  as a new operator API field. SQLite tests verify denial without
  mutating the original failed row. Source CI
  [#39700685628 passed](https://github.com/DigiLogicTech/OnePane/actions/runs/39700685628).
- [x] **Native Windows exact proof regression** (`c22d2dce`,
  `7bbbe466`): explicitly exercise unknown-PID Stop, cancelled
  prelaunch, recovery and recorded-zero versus NULL-PID proof on
  hosted Windows; Linux runs the local-model tests and race suite.
  A preceding workflow-only change `fdcec636` was rejected as
  malformed configuration and was replaced with the clean workflow
  before the verified runs. [Exact source/workflow CI #39700884306
  PASSED](https://github.com/DigiLogicTech/OnePane/actions/runs/39700884306)
  for `7bbbe46672ed460ba27b28b2436e3ab1e43d272c`, including
  both Windows and Linux; the physical Node job was intentionally
  **SKIPPED**, not marked accepted.

**Remaining unverified:** real model parent/child process-tree and RAM/
VRAM/Colibri-cache release under pressure and cancellation; physical
Windows service crash/restart, populated operator-selected non-C:
upgrade/reboot/rollback, installed Ubuntu service, paired physical
Nodes and reversible operator-authorised Level 5 postconditions.
Source tests cannot establish these external observations. A
human-facing reconciliation path for intentionally blocked uncertain
models needs installed UI/Debug Centre QA.

**Mandatory release gates remain:** final integrated code/security audit,
the explicitly LAST human-focused UI/UX customisation review, both
operator review approvals, physical acceptance and separately approved
Windows/Ubuntu package/installer QA. PR #227 is draft and unmerged.
No user models, real GPUs, existing Project data, credentials or live
network devices were touched in this checkpoint.

## Colibri replacement unknown-outcome and tracked process Wait (2026-10-22)

The earlier no-child/PID provenance work exposed two additional code paths
which could misinterpret an interrupted model as safely recyclable. This is
a scoped implementation and audit checkpoint, **not** real process-tree,
CPU/RAM/VRAM, installer, or paired-Node acceptance.

- [x] **Colibri rollback denied for unresolved replacement states**
  (`cdf091388d70e3ab2e7b4611190ec4e270fc4f14`).
  `reconcileFailedColibriReplacement` can no longer allow the old
  resident to restart merely because the failed target's Python PID
  appears absent while its persisted status is still starting,
  draining, orphaned, busy or healthy. An unknown `failed`/NULL-PID
  record and corrupt recorded PID are also denied. A proven
  no-child prelaunch cancellation and a completed `stopped` record
  retain their intended rollback path. Negative SQLite regressions
  assert that no old Colibri engine is restarted or foreign process
  signalled under unresolved ownership.
- [x] **Existing cancellation fixture reconciled to truthful state**
  (`1679169bc0fa93f5253c2856d745d826cab276d3`).
  The original request-cancelled-after-eviction test represented
  pre-spawn cancellation but omitted the required guarded no-child
  failure record. The fixture now records the same durable no-child
  marker that production writes, preserving safe rollback. Earlier
  intermediate code-only CI at `cdf09138` failed this legacy
  fixture; this was investigated and fixed rather than weakening
  the new production ownership check.
- [x] **Owned process exit signalling is race-safe**
  (`6f612ad09e203c05ec62266e2c3473b52044429c`).
  The managed `OSProcessBackend` no longer reads
  `exec.Cmd.ProcessState` concurrently with `cmd.Wait`.
  A per-launch, close-only completion channel records the completed
  Wait generation; once closed, cached executable/argv identity is
  rejected instead of reused as live ownership. Bounded tests launch
  only the self-owned Go test executable and concurrently inspect
  its short-lived lifecycle. This is **not** a kernel-held PID handle,
  a universal race-free OS PID reuse guarantee, or a Colibri child
  process release claim.
- [x] **Explicit platform regression**
  (`1abd72659de785f3c5bad272ec9a21d1a602b9f7`):
  native Windows and Linux race lanes run the subprocess-exit
  fixture in addition to the Colibri rollback regressions.
  [Exact combined source/workflow CI #39704135818 PASSED](https://github.com/DigiLogicTech/OnePane/actions/runs/39704135818)
  with native Windows checks, Linux model/Colibri race checks,
  full Go regression and cross-platform compilation. The physical
  Node job was **SKIPPED** intentionally.
- [x] **Physical acceptance protocol authored, not executed**
  (`45137212b3f6d1fb4408ae8a85b81e7c38e7c499`):
  `docs/rc11-physical-model-lifecycle-acceptance.md` specifies
  operator-authorised host/model preconditions, read-only inspection,
  observed process *and child* exit, per-process memory/VRAM
  attribution, state/restart failure matrix and explicit stop/pass
  criteria. No real model, remote Node, GPU, service or installer was
  started or modified in this work.

**Remaining blockers:** physical Windows/Ubuntu Colibri/llama.cpp parent
and child lifecycle + GPU memory reclamation; safe service crash and
populated operator-selected data-root rollback; paired Nodes and
authorised reversible L5 acceptance; final integrated security review;
the mandatory LAST human-focused UI/UX review; explicit approval
before Windows/Ubuntu packaging and installed QA. Do not infer
physical success from GitHub CI or silently convert an orphaned
engine to a stopped one.

## RC11 managed process generation / Colibri success admission (2026-10-22)

**Software-only checkpoint, not release or physical memory-reclamation
acceptance.** From prior audited head `b73e2aff`, 11 commits changed
seven scoped files. Intermediate workflow `a4fc1040` was rejected by GitHub
before running any jobs because a test selector was truncated; the original
clean workflow was restored and the selector simplified in `7a3a79c1`.

- [x] **Tracked subprocess generation fence — `7983360f`.**
  An old `exec.Cmd.Wait` goroutine may finish after a numeric PID has been
  recycled. Cleanup now removes the tracked map entry only if it still
  refers to the exact original `trackedManagedCommand` pointer.
  Cross-platform deterministic tests simulate delayed cleanup, PID reuse
  and idempotence; native Windows and Linux race lanes exercise them.
- [x] **Windows tree-signal ownership — `b4d1257d`,
  `1544911b`.** The Windows `taskkill /T /F` path denies missing or
  completed launch generations rather than signalling a bare restored PID.
  A native-Windows test starts its own untracked subprocess, confirms
  liveness, proves the model backend refuses to terminate it, and tears
  it down only through the test's own process handle. This guard does
  **not** close the final PID-recycle window of external taskkill, create
  a kernel Job Object, or certify that Colibri child GPU memory is freed.
- [x] **Colibri returned-owner and competing-residency contracts —
  `397856a1`, `313176d6`, `6ba9b728`, `49fe0bff`,
  `3846f67b`.** A nil-error starter must return an actual, matching
  Node/Deployment/Instance/PID/loopback/fingerprint with healthy or
  legitimately busy status. Otherwise the coordinator rejects claimed
  readiness without automatically rolling back an unknown process.
  Following a valid target return, any separate resident Colibri peer
  still observed on the same Node also blocks a false success.
  Positive and adversarial SQLite tests distinguish a truly stopped
  predecessor from overlapping or incomplete replacement state.
- [x] **Native Windows Colibri contract lane — `7a3a79c1`.**
  The exact native Windows test selection is included in the workflow.
  Source and full workflow at `7a3a79c1998e10b416e63ac448d92a3df06579d1`
  [passed CI #39718213444](https://github.com/DigiLogicTech/OnePane/actions/runs/39718213444).
- [x] **Unknown persisted busy inference admission — `55a8a914`.**
  Before process reuse, `Start` refuses a durable `busy` model
  without a corresponding, activity-lock-checked in-process lease.
  An old PID and healthy HTTP socket cannot establish completion of a
  crash-interrupted inference. Existing actively leased inference remains
  eligible under its separate model identity check. Windows and Linux
  regression evidence is
  [exact-head CI #39718524948](https://github.com/DigiLogicTech/OnePane/actions/runs/39718524948),
  **PASSED**: native Windows, Linux race/integration, full Go regression
  and cross-platform compilation. The physical Node job was **SKIPPED**.

**Limitations remain:** no approved real CPU/GPU/Colibri hardware test,
per-process child VRAM reclamation, kernel-held Windows process tree
ownership, installed Windows SCM/non-C selected data root rollback,
installed Ubuntu service test, paired Nodes, verified bounded Level 5
effects, final integrated code/security audit or the mandatory LAST
operator-focused UI/UX review. No production model process, credentials,
user Project data, home network or installed environment was touched.
No installer or release merge is approved.

## RC11 direct Colibri startup and conservative operator residency (2026-10-22)

This code checkpoint followed the verified `6add596b` model lifecycle
review. It covers code/tests and hosted CI **only**, not installation or
physical process-tree/GPU acceptance.

- [x] **Direct Models Start no longer bypasses Colibri rollback**
  (`6e775b6400a765897cb0c9a973c2867d36a65a90`).
  `RuntimeSupervisor.Start` resolves the intended managed backend under
  the existing residency mutex and routes a Colibri deployment through
  `colibriHotSwapLocked`. Ordinary chat Acquire and the explicit Hot Swap
  action already used this coordinator. A failed post-eviction direct
  launch now receives the same independently bounded, ownership-checked
  compensation path. Plain llama.cpp starts remain unchanged.
  Source-route regression prevents reintroducing bare `startLocked`
  for direct Colibri Start; the swap coordinator already has adversarial
  SQLite failure/rollback tests.
- [x] **Unresolved Colibri process stays visible to the operator**
  (`a7a4085ddca501c172d0aa292e8ff9f955a6c88e`).
  The read-only `ColibriHotSwapStatus` endpoint's
  `active_deployment_ids` conservatively includes `orphaned` and
  `draining` entries (possibly resident, not verified healthy),
  rather than hiding uncertain RAM/VRAM owners. The target's status and
  redacted message require reconciliation before swapping. This
  does not automatically signal, relaunch or reconcile any process.
  SQLite regressions verify both states, competing resident visibility,
  and absence of process signals.
- [x] **Native Windows and Linux race-lane regression**
  (`4f91668fdcf6b6a5735b87c6215baf2b7df6c19c`)
  explicitly selects the new direct-Start and status tests. The
  [exact combined source/workflow CI #39721549420](https://github.com/DigiLogicTech/OnePane/actions/runs/39721549420)
  **PASSED**, including native Windows, Linux race checks, full Go
  regression and cross-platform compilation. Physical Node approval
  status was deliberately **SKIPPED**.

**Unverified/release blockers are unchanged:** actual Windows/Ubuntu
Colibri/llama.cpp parent/child process-tree and independently attributed
RAM/VRAM release, physical Node and authorised Level 5 effects,
installed Windows SCM and chosen non-C storage upgrade/reboot/rollback,
Ubuntu installed service, full whole-branch code/security review,
mandatory LAST human-centred UI/UX review, and explicit operator
approval before packaging/merge. No installed model, service, user data,
credential or external device was touched.

## RC11 stopped/NULL-PID ownership and Colibri status — 2026-10-22

**Software-only verified checkpoint.** This follows `57766b75`
and fixes a previously unguarded ambiguity: `stopped` plus a SQL
NULL PID does **not** prove no engine was ever created. The daemon could
have crashed before process identity persistence. The code and tests
do not claim independently measured child-process or VRAM release.

- [x] **Durable no-child evidence for stopped models** (`86ca37a9`).
  Shared `confirmedNoChildWithoutPID` accepts `failed` or `stopped`
  with true SQL NULL PID, no child start timestamp, and the exact
  guarded pre-spawn failure reason. Unproven stopped/NULL-PID records
  now refuse local memory admission, runtime restart, manual Stop
  rewriting, and Colibri failed-swap rollback until explicit operator
  reconciliation. Recorded zero/invalid PIDs remain denied. Tests
  cover unknown legacy rows, mismatched reasons, contradictory
  `started_at`, idempotent Stop of a verified no-child model and
  positive safe rollback.
- [x] **Windows/Linux no-child test selection** (`a0e24b1d`).
  Dedicated native Windows and Linux race tests exercise unproven
  stopped/NULL-PID memory/restart/rollback paths.
  [CI #39728662960 PASSED](https://github.com/DigiLogicTech/OnePane/actions/runs/39728662960).
- [x] **Colibri resident-list truthfulness** (`443e9808`).
  `colibriResidents` now returns unproven failed/stopped owners with
  SQL NULL or invalid recorded PID as uncertain residency, rather than
  silently omitting them. `selectColibriEvictions` refuses to evict
  another model when ownership cannot be established; the status
  endpoint reports potentially occupied RAM/VRAM instead of an
  apparently empty Node. An exact verified never-launched child
  remains excluded and does not strand a legitimate swap.
- [x] **Test fixture correction without weakening safety**
  (`43e45f56`, `aa1e11d5`). Two older rollback fixtures simulated
  a formerly healthy Colibri engine as stopped with no PID and no
  provenance. Intermediate CI at `443e9808` and `fec93f32`
  correctly rejected this synthetic unknown-owner state. The fixture
  now models a genuinely launched predecessor with a recorded PID,
  later observed dead by a deterministic fake process backend (not
  an arbitrary live CI worker PID). Success and cancellation/recovery
  paths remain tested.
- [x] **Native exact-head source + workflow CI**:
  [#39729564503 PASSED](https://github.com/DigiLogicTech/OnePane/actions/runs/39729564503)
  at `aa1e11d53877add6a0930099e0d5c0c901c42a4e`, both hosted
  Windows and Linux with Linux race and full Go/cross-platform tests.
  Physical Node approval job was intentionally **SKIPPED**.

**Continuing release blockers:** no approved physical Windows/Ubuntu
Colibri/llama.cpp parent/child lifecycle, process-attributed GPU/RAM
reclaim, or installed service/SCM/non-C root upgrade/rollback evidence.
Paired real Nodes, irreversible/reversible Level 5 proof, mandatory
whole-branch security/source review, the explicitly LAST user-focused
UI/UX review and operator installer authorisation all remain open.
No runtime weights, installed service, actual GPU, user Project data or
external network target were touched.

## Colibri rollback/target persisted-generation admission — 2026-10-22

**Exact software verification:** source/workflow commit
`d8ef971e4ce0bde21f427a3c6480fad1a88f91c7`, hosted
[Windows/Linux CI #39741362521](https://github.com/DigiLogicTech/OnePane/actions/runs/39741362521)
**PASSED** (native Windows, Linux model/Colibri race, full Go regression,
cross-platform compilation). Physical Node job **SKIPPED**.

- [x] **Fail closed on false Colibri rollback reporting**
  (`a58026a1`). A replacement launch failure can initiate an
  independently bounded predecessor restoration, but a nil-error
  callback is not proof of residency. The restored instance must
  identify the expected Node/deployment/PID/loopback/fingerprint
  and healthy or legitimately busy state, match the saved SQLite
  runtime generation, and have no other possibly resident Colibri
  peer. Only then may the operator state claim the predecessor
  was restored. A conflicting/missing record leaves reconciliation
  required, does not return a usable target, and does not falsely
  clear `EvictedDeploymentIDs`.
- [x] **Deterministic positive/adversarial rollback fixtures**
  (`c8cc6f15`). A test predecessor is created with a fully
  populated persisted runtime record, not a bare `healthy`
  label. Regressions cover missing/wrong restored model identity,
  PID/fingerprint mismatch, uncommitted restoration, and a
  resurfaced competing replacement. Earlier intermediate
  source-only CI `a58026a1` failed because the fixture had
  NULL runtime identity columns; the fixture was repaired without
  weakening the production guard. The
  [corrected rollback workflow #39740881476](https://github.com/DigiLogicTech/OnePane/actions/runs/39740881476)
  **PASSED**.
- [x] **Target success must also match durable generation**
  (`f55f1c0b`). A nil-error, valid-looking requested model may
  not be returned as resident unless the persisted Instance ID,
  PID, port, fingerprint and expected model/Node match. Native
  negative tests cover forged returned identifiers and an
  uncommitted stopped SQLite record; a legitimate persisted
  successful Colibri swap remains usable.
- [x] **Explicit Windows/Linux test selectors** (`fce8335e`,
  `d8ef971e`). The same failure-path and target-owner regressions
  run in the named native Windows and Linux race suites; no real
  inference engines were started by the tests.

**Scope limitation:** durable ID/PID equality and the success of
`startLocked` are software-level ownership assertions, not a
kernel-held process handle or independent Colibri child/VRAM
reclamation proof. The actual host process tree, GPU allocation,
Windows/Ubuntu installed service and chosen non-C: root
upgrade/rollback, paired Nodes, authorised Level 5 external
postconditions, the complete source/security review and mandatory
LAST operator-focused UI/UX review remain release blockers.
No release merge, installer, live model, installed service or user
data was touched.
