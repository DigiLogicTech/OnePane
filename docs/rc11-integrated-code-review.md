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
