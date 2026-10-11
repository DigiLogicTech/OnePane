# RC11 Agent Worker assurance branch task manifest

Branch: `feature/rc11-batch-worker-assurance-v2-20261011`
PR: [#225](https://github.com/DigiLogicTech/OnePane/pull/225)
Base: `feature/rc11-featurepolicy-fail-closed-20261011`

## Completed implementation
- [x] Workspace activity check occurs before reusable system membership admission.
- [x] Repeat Workspace-state verification in the same transaction as Task/Attempt/Worker writes.
- [x] Inactive-state diagnostic reports actual Workspace status.
- [x] Regression: suspended Workspace with all three active system memberships cannot admit a Worker.
- [x] Regression: deterministic status change between preflight and admission transaction is rejected.
- [x] Regression: late Worker audit event insertion failure rolls back Task state, Attempt and Worker rows.
- [x] Regression: restart cannot reuse stale active system memberships after Workspace suspension.
- [x] Regression: nonexistent Workspace cannot create new system memberships.
- [x] Reviewed PR diff and kept side effects transaction-bound.

## Verified CI
- Code head `55e41b329743723178753a4f8b4f4747d49cd583`:
  Project Workspace Development Architecture run
  [#38115963038](https://github.com/DigiLogicTech/OnePane/actions/runs/38115963038)
  **completed successfully**, including Agent Worker SQLite integration tests and
  full Go regression, cross-platform compilation and existing frontend checks.
- Prior Worker branch commits also passed CI. This manifest-only update does not
  change executable code.

## Remaining human and release gates
- [ ] Human review and architecture alignment.
- [ ] Verify together with the upstream feature-policy runtime integration and
      user-directed QA before any release merge.
- [ ] Final product smoke test against Windows and Ubuntu installed builds.

## Continuation
Worker assurance code and branch CI are complete. Keep PR #225 draft until
upstream release review requirements are satisfied. No packaging or merge.
