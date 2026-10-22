# RC11 — Historical TCP return-to-baseline evidence

This checkpoint closes a functional gap in **operator incident review**, not the security/permission model.

- The existing per-Goal TCP reachability alert API now projects the first later immutable TCP attempt that returned to the **same human-approved baseline** after a drift notice. Correlation requires the same Project, Goal, Workspace, source, permit, revision, target IP/port, timeout and baseline ID/revision.
- The new `return_to_baseline_evidence_state` is `not_observed` or `historical_tcp_return_observed`, with an attempt ID and timestamp when present. This is historical TCP socket reachability evidence **only**.
- An alert remains `open` or `acknowledged` until a human acknowledges it; evidence of a past return does **not** assert currently healthy service, secure physical device, corroborated incident resolution or remediation permission. The Goal is not activated and no model, probe or Task is triggered by review.
- Correlation derives from already immutable, admitted probe attempts: no new credential, mutable issue flag, unsafe retry or unreviewed outbound network connection is introduced. The lookup is capped by the existing 100-alert response and supported by a history index.
- The integration regression uses two real domain-admitted attempts with an advancing test clock: unexpected connected result followed by baseline-matching not-connected result, then verifies foreign Project isolation, unchanged Goal health, no Tasks and no automatic alert closure.

**Next functional gate:** design a canonical, human-reviewed incident case model with explicit device/target correlation and independent provenance before any incident-confidence or autonomous recovery claims. Then address bounded reversible playbook postconditions, historical-data endurance and physical Node acceptance. Comprehensive user-centred UI/UX review remains the final development step before the next QA build. PR #227 stays draft; this source increment does not authorise packaging.
