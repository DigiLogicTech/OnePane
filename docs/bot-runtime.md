# M32 Bot Runtime

M32 adds a first-class conversational Bot surface that is deliberately separate from autonomous Task execution.

The goal is to let a OnePane user open and use the **provider or harness's own Bot mode** rather than flattening every Bot into a stateless model call. When the underlying runtime exposes a supported session API, OnePane preserves its remote Bot identity and session. When the provider exposes only its own hosted UI/app, OnePane records the Bot and launches that supported surface instead of pretending an external Bot API exists.

## Trust boundary

A Bot chat is human-interactive and uses the Bot runtime's **native authority** unless the remote system itself provides a more restrictive mode. If a Hermes Bot, Dify app, Grok Bot, etc. can use its own browser, terminal, plugins or connectors, those actions do **not** pass through OnePane ToolGateway merely because the conversation is visible in OnePane.

Therefore:

- Bot Runtime connections are never autonomous Scheduler candidates.
- Autonomous OnePane Tasks still use ModelDeployments or proposal-only AgentRuntime connections.
- Bot session metadata is marked `authority=native_runtime` and `onepane_autonomous=false`.
- OnePane records the user instruction, mirrored assistant response and transport receipt without claiming independent verification of native Bot side effects.
- A transport timeout after dispatch becomes `unknown`; OnePane does not blindly replay the message because the remote Bot may already have executed tools.

## Access modes

### `harness_api`

A harness exposes a documented persistent-session API. M32 includes native Hermes support.

Hermes Bot Mode is backed by Hermes profiles. A OnePane Bot maps `remote_bot_id` to the Hermes profile name, and its canonical OnePane Bot chat attaches to or creates that profile's `Bot Chat` session through the Hermes API server.

Hermes credentials use an independent Vault scope:

```text
bot/hermes/api-key
```

This is intentionally separate from:

```text
agent-runtime/hermes/access-token
```

The former gives a human access to Hermes' full Bot/session surface and native toolset. The latter is reserved for the separately governed external-runtime/proposal bridge used by autonomous OnePane execution.

The default Hermes Bot endpoint is loopback-only:

```text
http://127.0.0.1:8642
```

Remote endpoints must use HTTPS. Redirects are not followed for credential-bearing requests.

### `relay_api`

Harnesses whose native chat/session APIs vary by deployment can implement **OnePane Bot Protocol v1** behind a small adapter. M32 ships relay presets for Dify, Flowise, n8n, Open WebUI and a custom bridge.

OnePane sends:

```http
POST /v1/bot/chat
Authorization: Bearer <bot-scoped-secret>
Idempotency-Key: <turn-id>
Content-Type: application/json

{
  "bot_id": "remote-bot-id",
  "session_id": "stable-session-id",
  "input": "hello",
  "idempotency_key": "..."
}
```

The bridge returns a normalized assistant message while remaining responsible for the harness's own persistent memory, tools and session semantics.

Relay credentials use:

```text
bot/<preset>/api-key
bot/<preset>/access-token
```

They cannot be substituted with provider, plugin, gateway or AgentRuntime secrets.

### `hosted_surface`

Some products provide durable Bots but no supported external Bot-session API. These are registered in OnePane as hosted Bots with a provider-owned launch URL. M32 includes hosted presets for:

- ChatGPT GPTs
- Grok Bot
- Gemini Gems
- Microsoft Copilot agents
- Poe Bots
- custom hosted Bot surfaces

Fixed presets pin launch URLs to the expected provider domains. OnePane does not collect provider browser cookies, proxy account credentials, scrape the provider UI, or claim that a hosted Bot is available through an external API when it is not.

## Durable state

M32 adds:

- `bot_connections` — configured native/relay/hosted access surface.
- `bot_profiles` — the remote provider/harness Bot identity shown in OnePane.
- `bot_sessions` — local session identity plus optional remote session identity.
- `bot_messages` — mirrored conversation messages.
- `bot_turns` — durable send lifecycle and idempotency record.

A canonical session is one stable long-lived chat per user+Bot. Scratch sessions can also be created.

## Web chat

The embedded OnePane shell now has a **Bots** view.

- API/relay-backed Bots create or attach to their canonical session and chat directly in OnePane.
- Hosted-only Bots show an explicit provider-launch action.
- The current view is deliberately minimal; richer streaming, attachments, tool-progress rails, voice, reactions, bot/group discovery and OAuth belong to the final WebUI phase.

## API

```text
GET  /v1/bot-presets
POST /v1/vault/bot-credentials

GET  /v1/bot-connections
POST /v1/bot-connections

GET  /v1/bots
POST /v1/bots

GET  /v1/bots/{botID}/sessions
POST /v1/bots/{botID}/sessions

GET  /v1/bot-sessions/{sessionID}
GET  /v1/bot-sessions/{sessionID}/messages
POST /v1/bot-sessions/{sessionID}/messages
```

Workspace capabilities are separated into `bot.read`, `bot.write`, and `bot.chat`.

## Deliberate non-goals in M32

M32 does not:

- convert a native Bot into a trusted OnePane autonomous worker;
- claim provider-hosted Bots have APIs that their vendors do not expose;
- copy a provider's private memory into OnePane unless the remote API explicitly returns it;
- automatically replay `unknown` Bot turns;
- implement cross-provider Bot group chats yet.

Those boundaries let OnePane offer one Bot roster and web-chat entry point while preserving the runtime that actually owns each Bot.
