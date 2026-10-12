# RC11 — Product UI/UX alignment and design-system convergence

Status: **Open, pre-release gate**. Added 2026-10-12. This review is required before the next installed Windows/Ubuntu QA build; it does not authorize packaging or release.

## Design intent

OnePane is a **local-first execution harness**, not merely a monitoring dashboard or cloud chat client. The UI must make it straightforward to complete work despite small CPU/GPU/RAM capacity, with queuing, model pinning/hot swap, Colibri, OmniRoute and resource-aware routing shown as coherent choices rather than unrelated features.

1. **Project = goal and authority boundary.** A Project groups goals, Tasks, Workspaces, evidence and execution history. Workspaces are separately isolated build/run environments and may share only explicitly published artifacts. A legacy shared Project sandbox must remain clearly identified as compatibility, never promoted as isolated.
2. **One assistant, distinct responsibilities.** Show Assistant / Project Orchestrator as clear user-facing experiences, not contradictory parallel chat systems. Operators should understand which principal, model and Project a request belongs to, and when an action needs approval.
3. **Level 4 default, Level 5 opt-in.** Level 4 is bounded goal/task execution; Level 5 is ongoing operations requiring explicit source, network, device and remediation rights. A configured goal or TCP reachability result is never displayed as active protection, verified device security or authorised remediation.
4. **Local-first by default.** Distinguish local/paired Nodes from cloud providers. Never hide cloud costs, model offload, resource reservation, queue position or runtime draining. Preserve CPU/GPU selection and memory constraints.
5. **One consistent UI vocabulary.** Use the same component anatomy, hierarchy, CTA placement, status semantics and compact disclosures everywhere. Do not accumulate more one-off Alpha/RC overrides; where a control can use a shared pattern, replace overrides incrementally.
6. **Truthful diagnostics and reversible actions.** Explain pending/queued/unknown/denied/degraded/verified states accurately, show the source of truth and revision if relevant, and surface Debug/Diagnostics/QA capture in context. Destructive actions require explicit confirmation, a visible scope and a cancellation/recovery path if technically available.

## Audit observations from current source

- `app-foundation.js` is a large compatibility runtime (~328 KB), alongside `app.js` (~116 KB) and many late-loading feature overrides. Shared primitives exist (`.btn`, `.page-heading`, `.toolbar`, `.subtab`, `.panel-card`) but need one authoritative contract.
- `style.css` and `ui-consistency.css` jointly control scroll ownership and theme variables. Logs drawer/Inspector have previously caused page clipping. Future design changes must preserve the shell-owned scroll contract, not use viewport-height hacks.
- Nested Project → Workspace and Local/Cloud/Routing/Discover navigation already exist; a secondary legacy Workspaces entry is removed by a late override. Verify one visible selection and browser/tab lifecycle rather than reverting to duplicate parents.
- Level 5 controls are currently appended to the Project goal view, with independent receipt review, exact IPv4 network permits, manual TCP tests and baseline comparison. Split by permission class and use consistent scopes, confirmation and `unverified` semantics.
- Local/Cloud models, Nodes, Agents/Teams/Research, Skills, Secrets, Operations, Settings and command palette still need full page-level content/interaction review. This is not resolved by a global CSS patch.

## Incremental work manifest (do not release until reviewed)

| ID | Priority | Workstream | Acceptance / evidence |
| --- | --- | --- | --- |
| UX-01 | P0 | Design principles + page inventory | Capture canonical IA, design tokens, copy glossary, migration approach, current debt and QA matrix in this document. No silent scope changes. |
| UX-02 | P0 | Shared visual primitives | One theme-aware control/field/toolbar/panel/status contract, visible keyboard focus, reduced-motion and high contrast checks. Applied without breaking specialised layouts. |
| UX-03 | P0 | Navigation and context | Projects → Workspaces hierarchy, clear model subroutes, single active nav item, reliable tabs/back/reload, responsive shell and scope breadcrumbs. |
| UX-04 | P0 | Project and Workspace surfaces | Separate Project overview, Workspaces, Files/Library, isolated runtime, interconnected Tasks and Agent/Orchestrator experiences; no shared sandbox presented as isolated. |
| UX-05 | P0 | Local-first model lifecycle | Clear Local/Cloud routing, Colibri pinning, OmniRoute, Node CPU/GPU controls, model queue, VRAM pressure, standby/draining and task admission. |
| UX-06 | P0 | Autonomous operation affordances | Explicit L4 default / L5 opt-in, distinct permissions for read, network probes and remediation; consent scope, pause/revoke and proof of results. |
| UX-07 | P1 | Page-by-page visual audit | Operations, Tasks, Projects, Workspaces, Models, Nodes, Agents/Teams, Skills, Sandboxes, Providers, Integrations, Secrets, Evidence, Settings, Chat, Help/Tour, Debug and installers. |
| UX-08 | P0 | Cross-theme and responsive QA | Light, Dark, Midnight, Graphite + custom themes; desktop and compact; expanding/collapsing Logs and Inspector; keyboard, 200% zoom, focus, mobile and reduced motion. |
| UX-09 | P0 | Accessibility and state semantics | Functional controls only, labelled icons, correct tab/accordion ARIA, responsive table overflow, no hover-only operations, success/warning/unknown distinctions; automated contract tests. |
| UX-10 | P0 | Code and installed QA review | User reviews consolidated code and product vision, second code review, full CI, then Windows/Ubuntu installed manual QA with screenshot and debug report capture. |

## UX design contract

- **Layout:** content must constrain to `minmax(0,1fr)` under the desktop shell; Logs and Inspector reserve their own space; no page assumes full-window height. Toolbar actions group visually on the trailing edge; narrow layouts wrap at available width rather than clipping controls.
- **Components:** standard buttons 34–38px desktop with at least 40px compact hit targets where space permits; form fields share height/radius, border and focus state; each field has an accessible name; do not change checkboxes/radios to 100%-width text inputs.
- **Typography:** page title → short explanatory sentence → section label → control label → metadata. Avoid decorative headers and duplicate headings. Indicate `unknown` rather than green health when evidence is missing.
- **Theme:** use existing `--bg`, `--panel`, `--panel-2`, `--panel-3`, `--text`, `--muted`, `--border`, `--accent`, `--on-accent`, `--focus-ring` tokens. No hard-coded dark surfaces or pale text on Graphite.
- **Motion:** honour `prefers-reduced-motion`; no layout shifts on drag/resize; no auto-scrolling away from typed work.
- **Safety:** approval and execution are separate, particularly for L5, with an explicit exact target/scope and visible evidence of what did and did not occur.

## Completion criterion

UI/UX alignment is complete only after shared-component regression checks, a reviewed page-by-page task matrix, themed/resized visual QA, and the already-agreed code/vision review gate. A single stylesheet, a green Go CI run, or an uninstalled preview is **not** completion.


## First implementation checkpoint — 2026-10-12

**Completed implementation subsets (not full UX review):**

- **UX-02:** Added `internal/webui/static/ux-alignment.css`, loaded after view styles. Shared theme-token controls, page-heading hierarchy, toolbar wrapping, reduced-motion support, keyboard focus, dense L5 authority rows and narrow-viewport action sizing. Preserved Logs/Inspector shell space and persisted widget resize geometry.
- **UX-03 subset:** Patched the final Project/Workspace navigation renderer in `project-workspace-page.js` to avoid double-active Project/Workspace leaves, show the collapsed parent when a selected child is hidden, and expose `aria-current="page"` for the active desktop and mobile route.
- **UX-08 subset:** Added automated WCAG AA (4.5:1) static palette-contrast checks for the seven explicit dark themes (Dark, Graphite, Midnight, Ocean, Forest, Violet and Ember). These pass from existing palette values. Light/System, custom themes and actual screen-level accessibility still require inspection.
- **UX-09 subset:** Added a shared-source contract at `scripts/validate_product_ux_alignment.js` and wired it into `.github/workflows/project-workspace-development-checks.yml`. This asserts final stylesheet precedence, canonical theme tokens, keyboard focus, no shell viewport overrides, nav selection semantics and L4/L5 design intent.

**Validation:** completed hosted regression `9205b60e` at [CI run #38195487992](https://github.com/DigiLogicTech/OnePane/actions/runs/38195487992) with successful frontend contract, Go/SQLite, integration and cross-platform compilation jobs. Earlier red runs came from test harness formatting/regex mistakes corrected in the final head. This is **source and unit/regression validation, not installed visual QA**.

**Still open:** UX-03 full navigation/back/reload and state recovery; UX-04 through UX-07 page-by-page redesign; UX-08 true cross-theme/shell-resize/small-screen visual QA; UX-09 full keyboard/assistive-tech audit; UX-10 code/vision review and Windows/Ubuntu installed QA. No new release is authorised by the shared-layer work.
