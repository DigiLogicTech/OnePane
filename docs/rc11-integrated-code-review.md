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
- [ ] Latest integrated commit passes Linux/Windows CI and all new integration tests.
- [ ] First code and security review on **combined** diff.
- [ ] User vision alignment interview and recorded functional decisions.
- [ ] Second audit against the updated vision, followed by any remediation.
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
