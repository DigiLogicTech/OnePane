# Manual Web Chat and Research Council handoffs

## Purpose

The new **Web Chat** sidebar button opens a dedicated, full-width page. It is
separate from **OnePane Chat** (Assistant / Project Orchestrator) and available
from the mobile More menu.

The page has **one tab per manual Web Chat conversation**, including several
simultaneous conversations with the same cloud provider (for example, ChatGPT 1
and ChatGPT 2), as well as separate Claude, Gemini, Perplexity and Grok tabs.
The **+ New Web Chat** action selects a provider and an optional tab label.
Each tab independently remembers its provider, selected Council turn,
conversation generation and tab label. Tab metadata is saved per Workspace,
while unsent prompt/response scratchpads remain memory-only for privacy;
closing or reloading the browser discards those drafts. It uses
the normal external websites in a new browser tab/window rather than embedding
login pages or scraping browser output. This is an operator-mediated workflow,
not an API, proxy, OAuth or unofficial browser bridge dependency.

No provider browser cookies, access tokens or messages are extracted by OnePane.

## Start a Web-only Council (guided flow)

The **Start Web-only Council** button on Web Chat opens a wizard; the user does
not have to build a Team manually beforehand. The wizard supports:

- A research objective and friendly Council name.
- Two to eight manual-only seats, each with an explicitly chosen provider,
  model label and research role. The same provider may appear multiple times.
- One to five configured independent cross-critique rounds and optional final
  synthesis, with Research integrity enabled automatically.

After confirmation, OnePane creates the canonical Team, registers L0
consultation-only members, saves the Research configuration, creates an
operator-review Task, and starts a Council session. It never requests an
inference deployment or tools for these manual seats.

One Web Chat conversation tab is provisioned for each seat when there is
capacity (16 tabs per Workspace). Tabs are pinned to their originating
Council session and member. Pending turns are **matched by both session and
member ID**, avoiding cross-talk between two different ChatGPT seats. Later
research rounds automatically appear in the same matching tabs. Existing
pending drafts are preserved until the operator acts.

The queue checks periodically while Web Chat is active and **updates the
display only after new or changed turns appear**. No provider pages are
loaded in the background. If automatic refresh is unavailable, the
operator can select **Refresh**.

If a network error occurs after creating a Team or adding seats, the wizard
retains acknowledged resource IDs in memory and supports *Resume Council
launch* without intentionally creating those resources again. If a request
succeeded on the server but its response was lost, the user should inspect
the partial Team/Task state before retrying. The draft process is not a
single all-or-nothing backend transaction.

An operator must still open each external web provider, paste the prompt,
and paste the complete response back. Subscriptions are subject to each
provider's separate account limits.

## Configure a Council seat

1. Open **Agents → Teams → Configure** on the Team to use for Council work.
2. Choose **Add Manual Web Chat seat**.
3. Choose provider, write an *operator-attested* model name, display name,
   and Council role. The seat is stored as an L0 agent with
   config.manual_web.enabled=true, provider_id and model_label.
4. Start a Council session on that Team (manual Web seats are not eligible
   for ordinary Team execution).

TeamWorker uses the same immutable Council member snapshot, round and
Research context filtering as a routed seat, but skips scheduler admission,
budget reservation, remote inference and tool calls entirely.

The Council turn enters blocked while its manual prompt is pending. Other
independent Council turns can run, but the round cannot advance until all
required turns succeed. No polling or provider usage occurs during the wait.

## Multi-chat tabs and side-by-side provider use

- Select a Web Chat tab to work on its Council handoff. Tab switching does not
  change another tab's Council selection or unsent response draft.
- **+ New Web Chat** can create multiple conversations for the same provider.
  A tab can be used independently without attaching a Council turn.
- **Close tab** hides that conversation workbench; its queued Council work
  remains in the server-side queue, and can be selected from another tab.
- **New Conversation** inside a tab starts a fresh provider-site conversation.
  For an attached pending Council turn, the server increments the attempt
  generation and keeps the immutable prompt and Council round unchanged.
- The provider website opens in a separate browser tab. Provider sites may
  reject iframe embedding, and this release does not include a native WebView
  runtime. The OnePane tabs manage handoffs, **not live embedded provider UI**.
- Operators can arrange actual cloud-provider browser windows side by side
  using the operating system. A future native WebView or draggable multi-panel
  canvas would need separate compatibility, authentication and security QA.

## Operator handoff

1. Navigate to **Web Chat** in the sidebar.
2. Select a provider tab and the pending turn; check the prompt for content
   that should not be supplied to a third-party service.
3. Click **Copy Prompt** then **Open Provider**, paste and submit it in the
   signed-in provider's own web page.
4. Paste the complete answer into Web Chat and click **Submit to Council**.
5. A single transaction stores the attested response, SHA-256 digest and
   model/provider/conversation metadata; marks the exact turn successful;
   and emits a Council audit event.
6. The Research Council coordinator sees the completed turn on its next tick
   and proceeds with the configured critique/synthesis workflow.

**New Conversation** opens a fresh provider entry point and increments the
local conversation-generation counter. It does *not* create another Council
round, change model bindings, erase previously submitted seat outputs or alter
the frozen prompt. Submitting a response from an outdated generation fails
closed with an HTTP conflict. A new provider conversation must still be
started by the human on the website.

## State and security model

- The pending and submitted handoffs persist in
  manual_web_council_turns (migration 0033).
- Council prompt and response digests are captured; external claimed model
  identity is marked operator_attested, not cryptographically verified.
- Prompt contents and the submitted response are stored inside OnePane, so
  the workspace's data-handling policy remains important.
- GET /v1/manual-web/turns?workspace_id=... requires team.read.
- POST /v1/manual-web/turns/{id}/submit and
  POST /v1/manual-web/turns/{id}/new-conversation require team.write
  and a real authenticated human principal.
- Both mutations verify workspace membership and the exact queued turn.
- Conversation generation, session round, turn status and response identity
  are checked to prevent stale submissions and duplicated completion.
- External websites are linked via an allowlisted provider catalog with
  noopener noreferrer; unknown providers have no default navigation URL.
- No tool grants, file reads, command execution, browser automation,
  subscription credential transfer or autonomous task creation.
- Browser provider chats remain in the provider's own account. OnePane
  stores only explicitly pasted outputs, not the entire provider chat history.

## Current limitations

- External providers can prohibit iframe embedding, so this release opens
  independent browser tabs. Native webview hosting is intentionally deferred.
- Model selection and subscription allowances are controlled by the provider;
  OnePane cannot certify that an operator selected the configured model.
- First implementation supports text copy/paste and explicit Council turns.
  File attachments, screenshot provenance, more comprehensive replay snapshots,
  custom provider website registration, and cross-provider browser tab lifecycle
  management are future work.
- The known-good Windows release is unchanged. Validate a real multi-round
  Council with two independent manual Web seats and a fresh-conversation
  handoff before merging.
