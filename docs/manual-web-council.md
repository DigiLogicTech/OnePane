# Manual Web Chat and Research Council handoffs

## Purpose

The new **Web Chat** sidebar button opens a dedicated, full-width page. It is
separate from **OnePane Chat** (Assistant / Project Orchestrator) and available
from the mobile More menu.

The page has provider tabs for ChatGPT, Claude, Gemini, Perplexity, and Grok,
plus any additional provider identifiers encountered in Council seats. It uses
the normal external websites in a new browser tab/window rather than embedding
login pages or scraping browser output. This is an operator-mediated workflow,
not an API, proxy, OAuth or unofficial browser bridge dependency.

No provider browser cookies, access tokens or messages are extracted by OnePane.

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
