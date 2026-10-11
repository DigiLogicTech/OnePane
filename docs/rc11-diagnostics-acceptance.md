# RC11 diagnostics acceptance and security model

## Implemented in diagnostics branch

- `featurepolicy.DecisionDiagnostic` produces typed, bounded JSON fields:
  `at_utc`, `component`, `correlation_id`, `action`,
  `allowed`, `category`, and `remediation`.
- Allowed actions are a fixed execution-capability vocabulary. Unknown actions
  become `redacted`; only `run:`, `task:`, `attempt:`, `request:` and
  `trace:` correlation IDs with bounded alphanumeric suffixes are retained.
- The existing **Settings > Debug & Diagnostics** surface includes an explicit,
  offline policy evidence reviewer. The operator must paste an authorised
  diagnostic export; the viewer never fetches evidence or grants permissions.
- The reviewer caps input at 32 KiB and 64 records, validates category/decision
  consistency, discards unexpected fields, and clears pasted raw JSON after
  review. Only an allowlisted local JSON projection can be downloaded.
- The frontend uses `textContent` instead of interpolating diagnostic content
  as HTML. The viewer does not persist browser or server state or upload data.

## Not implemented by this feature alone

- **No live policy telemetry claim.** A pasted diagnostic is not proof that a
  gateway enforced a policy. The policy runtime branch must integrate effective
  Project/Workspace/Agent permissions with the actual side-effect boundaries.
- The viewer does not automatically discover or retrieve Worker failures,
  unavailable runtimes, stale Nodes, inactive Workspaces or installer failures.
  Existing authorised QA panels handle their separate backend evidence.
- Neither this offline viewer nor a successful simulated decision is permission
  to read another Workspace. Do not copy raw logs, passwords, API keys or model
  prompts into the input box.
- Release readiness still requires full regression CI, security/code review,
  integration with upstream policy enforcement and human QA.

## Acceptance checks

1. Go tests verify stable JSON keys, denial precedence, secret redaction,
   non-escalation, malformed/unknown actions and bounded remediation hints.
2. Node tests validate privacy allowlists, malicious/unexpected fields,
   malformed timestamps and IDs, count/byte ceilings and raw-input eviction.
3. Debug Centre script contract checks verify load order and no hidden data
   transport or browser storage.
4. Workflow `Project Workspace Development Architecture` must pass on the
   actual branch head, not an earlier green commit.
5. Manual QA must confirm responsive layout, keyboard operability, current
   theme styling and that a downloaded file contains no supplied secret canary.
