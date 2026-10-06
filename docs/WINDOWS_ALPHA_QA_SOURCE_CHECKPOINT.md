# Windows alpha QA source checkpoint

This checkpoint contains the source-side fixes accumulated after the first Windows alpha/QA2 run-through. It intentionally does not contain a newly compiled Windows release.

## Included QA fixes

- Guided product-tour spotlight and target-aware positioning; Windows `Ctrl+K` wording.
- Live notification centre with dynamic unread badges and control-plane-derived attention items.
- Configurable Operations dashboard and Workspaces with add/remove/reorder/reset and bounded snap sizes.
- Recoverable Inspector and observability drawer.
- New-tab picker rather than forcing Operations.
- Additional Ocean/Violet/Ember themes, revised Light theme, and system-aware login/bootstrap theming.
- Real New Task, Project configuration, node pairing, provider connection, integration credential and provider list API paths.
- Full-page filtered Vault credential catalogue using write-only provider credentials and Vault references.
- OmniRoute local-loopback or remote-HTTPS support, persisted gateway URL, Vault gateway credential selection, and isolated failure state.
- Installer and GUI model-pool selection.
- Full scrollable model catalogue sourced from the Local AI catalogue API.
- Windows OnePane icon, native title-bar theme integration, and double-click `Verify-OnePane.cmd` launcher.
- Windows Desktop proxies the authoritative backend WebUI instead of carrying a second drifting copy.
- Direct WebUI shell serving fixes the `/` <-> `/index.html` redirect loop, with regression tests.
- Operations, Tasks, Projects, Providers and notifications use real control-plane list/read APIs instead of canned dashboard counts.
- Observability drawer uses Event Ledger data rather than canned demo log rows.

## Validation

Run:

```bash
python3 scripts/validate_windows_qa_source.py
node --check internal/webui/static/app.js
```

The source validator covers the Windows QA requirements above. Targeted Go tests cover API, WebUI, provider onboarding, tasks, projects, inference and Event Ledger readers.
