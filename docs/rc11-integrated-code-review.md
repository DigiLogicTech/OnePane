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
- [ ] **Exact-source/workflow CI verification:** [run #39553844948](https://github.com/DigiLogicTech/OnePane/actions/runs/39553844948)
  started for `cf6c8614`; verify both hosted jobs before marking this
  checkpoint green. Preceding source/test run
  [#39553490507](https://github.com/DigiLogicTech/OnePane/actions/runs/39553490507)
  passed but does not validate the later Colibri/script-position changes.

The change does not claim OS-kernel-held process identity, ownership of a
recycled PID on Windows after a service crash, a proven bound GPU child/process
tree, or actual Colibri VRAM release. The existing deny/reconcile path for
untracked Windows PIDs is intentionally unchanged. Whole-branch security
review, real constrained-hardware acceptance, non-C: Windows upgrade/reboot/
rollback, paired Nodes, opt-in Level 5 effects, mandatory LAST UI/UX review,
operator installer approval and PR merge remain outstanding.
