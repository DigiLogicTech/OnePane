# RC11 CI completion register — 23 October 2026

Snapshot taken before the changes in this follow-up branch. Baseline: `fb7f5df7ada0f85517ad2b4dd49916133e600656`. Source: authenticated GitHub open-PR listing, commit-bound PR workflow runs, repository queued/in-progress queries and current workflow source.

## Current finding

There are **26 open PRs**. The six recent RC11 heads (#222–#227) all have successful PR-triggered runs; the current integration head passed Linux regression/race/source and native Windows checks. Repository-wide queries returned **zero queued and zero in-progress runs** at inspection time. The remaining CI work is acceptance, coverage and branch reconciliation, not a currently running backlog.

Of the 26 heads, nine have returned successful runs, one has an older failure, and sixteen have no PR-triggered run returned. The commit-workflow connector filters to PR-triggered runs and returns its first page only: **no returned run is not evidence that a branch was never tested**. Manual/push-only packaging workflows need separate inspection. This register does not certify required branch checks or branch-protection configuration.

| Open PR | Inspected head | Returned PR workflow result |
| --- | --- | --- |
| [#227](https://github.com/DigiLogicTech/OnePane/pull/227) | `fb7f5df7` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/39749071306) |
| [#226](https://github.com/DigiLogicTech/OnePane/pull/226) | `93549579` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38119073198) |
| [#225](https://github.com/DigiLogicTech/OnePane/pull/225) | `e69146a0` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38116121361) |
| [#224](https://github.com/DigiLogicTech/OnePane/pull/224) | `c7c8cba5` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38118341835) |
| [#223](https://github.com/DigiLogicTech/OnePane/pull/223) | `475bf8f6` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38114205521) |
| [#222](https://github.com/DigiLogicTech/OnePane/pull/222) | `83120e35` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38110599084) |
| [#109](https://github.com/DigiLogicTech/OnePane/pull/109) | `529ecac7` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38024257882), [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38024257854) |
| [#107](https://github.com/DigiLogicTech/OnePane/pull/107) | `8c39f17c` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38023832551) |
| [#18](https://github.com/DigiLogicTech/OnePane/pull/18) | `bc3e05f2` | [success](https://github.com/DigiLogicTech/OnePane/actions/runs/38110521562) |
| [#17](https://github.com/DigiLogicTech/OnePane/pull/17) | `33f1d8d6` | No PR-triggered run returned |
| [#16](https://github.com/DigiLogicTech/OnePane/pull/16) | `85e5f36f` | No PR-triggered run returned |
| [#15](https://github.com/DigiLogicTech/OnePane/pull/15) | `d2f4ae59` | No PR-triggered run returned |
| [#14](https://github.com/DigiLogicTech/OnePane/pull/14) | `53ca5bdb` | No PR-triggered run returned |
| [#13](https://github.com/DigiLogicTech/OnePane/pull/13) | `1b54eac6` | No PR-triggered run returned |
| [#12](https://github.com/DigiLogicTech/OnePane/pull/12) | `8ae69049` | No PR-triggered run returned |
| [#11](https://github.com/DigiLogicTech/OnePane/pull/11) | `fc9c1189` | No PR-triggered run returned |
| [#10](https://github.com/DigiLogicTech/OnePane/pull/10) | `9c5ac2d7` | No PR-triggered run returned |
| [#9](https://github.com/DigiLogicTech/OnePane/pull/9) | `be5adab2` | No PR-triggered run returned |
| [#8](https://github.com/DigiLogicTech/OnePane/pull/8) | `874960f9` | No PR-triggered run returned |
| [#7](https://github.com/DigiLogicTech/OnePane/pull/7) | `bd154586` | No PR-triggered run returned |
| [#6](https://github.com/DigiLogicTech/OnePane/pull/6) | `a8d1de33` | No PR-triggered run returned |
| [#5](https://github.com/DigiLogicTech/OnePane/pull/5) | `a9f7cf59` | No PR-triggered run returned |
| [#4](https://github.com/DigiLogicTech/OnePane/pull/4) | `5dda4480` | No PR-triggered run returned |
| [#3](https://github.com/DigiLogicTech/OnePane/pull/3) | `b8ba7efa` | No PR-triggered run returned |
| [#2](https://github.com/DigiLogicTech/OnePane/pull/2) | `dfc5a990` | No PR-triggered run returned |
| [#1](https://github.com/DigiLogicTech/OnePane/pull/1) | `f6e224d8` | [failure](https://github.com/DigiLogicTech/OnePane/actions/runs/37173631341) |

The old #1 failure is in the step **Restore Alpha 3 and apply Alpha 3.1 stabilization**. It is not a failure of the current RC11 head; reconcile its relevance and missing external build inputs before rerunning or closing it. Historical red intermediate runs are retained as history, not erased or relabelled passed.

## Work to close

Implementation has progressed since the inventory snapshot. See [execution status](rc11-qa-execution-status.md) for the live Team room, shared compute admission/cancellation tests, Windows key protection, current-source QA builder and the remaining acceptance gates. The historical inventory below is not a claim that this newer source head has passed CI.

| ID | Action | Completion evidence | Status |
| --- | --- | --- | --- |
| CI-01 | Carry existing Web Council/session-isolation and Colibri UI checks into current integration CI | Existing five behavioural validator scripts and six syntax checks run in the current workflow; their source/validator changes trigger it | Implemented in this follow-up; all 23 repository validator scripts pass locally, including 20 now directly invoked by integration CI plus three already invoked through its development-surface validator; all 25 directly referenced syntax checks pass; final hosted head must pass separately |
| CI-02 | Reconcile stacked/legacy PRs before release | Requirement/patch comparison against integration, explicit retained/absorbed/deferred decision, then deliberate branch cleanup | Open; several heads are not Git ancestors despite the integration manifest describing consolidated work; ancestry alone cannot determine missing functionality |
| CI-03 | Establish fresh physical rootless evidence | Explicitly dispatched approved-head tests with trusted runner, protected reviewer environment and administrator-approved pre-pulled digest images | Unverified; five physical jobs exist, not executed by this assessment |
| CI-04 | Qualify real model process/resource lifecycle | CPU/GPU/Colibri stop, swap, cancellation/crash and child/RAM/VRAM observations on selected machines | Unverified; hosted process/SQLite tests are insufficient |
| CI-05 | Installed Windows and Ubuntu acceptance | Populated RC10 upgrade, chosen non-C: root, service restart/reboot, rollback and preserved data/models/credentials | Open; current native Windows source tests are not an installed MSI upgrade |
| CI-06 | Add live Team execution and browser tests as functionality lands | Two-agent collaboration, switchable views, long-room pagination, restart/reconnect, isolation/revocation and constrained-resource queueing | Required; see live Team contract; existing Council tests do not prove live teamwork |
| CI-07 | Final whole-product/security and LAST UI review | Reviewed requirement→code→test/evidence matrix, remediation, current hosted CI and operator sign-offs | Open; no installer or release merge is approved by this change |

## Coverage change in this follow-up

The older `manual-web-council-check.yml` runs on its historical feature branch or manual dispatch. Its three behaviour validators were absent from the current integrated workflow. Preserve that regression investment by also running:

- `validate_web_chair.js`: Chair configuration/approval and session-isolated handoffs.
- `validate_web_only_council.js`: independent manual web seats, staged rounds and policy.
- `validate_web_chat_sessions.js`: session isolation, per-provider concurrency and conversation/handoff behaviour.

Two more passing validators were found only in the historical Alpha 3.1 stabilisation workflow: `validate_colibri_spec_tour.js` and `validate_models_qa_rc2.js`. They exercise current Colibri spec/tour distinctions, model registration, duplicate prevention and Agent Check messaging. Carry both into integration rather than depending on an old build-restoration job. Their source modules and validator changes now trigger integration CI.

The integrated workflow also syntax-checks the four corresponding web-chat scripts and both Colibri modules. Three other QA validators (incident capture, report summary and consolidated support ZIP) are already invoked by `validate_project_development_surface.js`; they were verified rather than duplicated. No test is removed, physical dispatch gate changed or model/host privilege widened.

## Physical workflow boundaries

`rc11-rootless-physical.yml` defines rootless Workspace isolation/recovery, Godot build, large-artifact publication, full Library publication and Workspace service-broker acceptance. These jobs require manual dispatch, matching full reviewed integration SHA, typed operator confirmation, an approval variable, protected environment and pinned image variables. They must remain separate from automatic PR source checks. Do not turn a skipped job into a green physical acceptance result.

Release order remains functional completion → final code/security review → LAST product UI review → approved QA packaging → installed/operator acceptance → release decision.

