# OnePane Messaging Gateway

M30 adds durable notification delivery on top of the OnePane control plane. The gateway is intended for task-completion notices, Routine results, approvals/escalations, operational alerts, and other event-driven messages.

## Security and execution model

A notification is not sent directly from a model or Routine. The path is:

`Event Ledger -> NotificationRule -> Delivery -> CapabilityLease -> OperationCoordinator -> gateway.send -> receipt Verification -> committed delivery`

`gateway.send` is an `EXTERNAL_SEND` ToolGateway capability. Each delivery gets a one-use lease scoped to its exact `gateway-target:<id>`. A successful HTTP/API response establishes only V1 acceptance-receipt evidence; it does not claim that a human read the message. Unknown network outcomes are never automatically replayed because doing so could duplicate an externally delivered message. If the daemon restarts while a delivery is in flight, that delivery becomes `unknown` and requires reconciliation.

Gateway credentials live under an independent Vault namespace such as:

`gateway/discord/bot-token`
`gateway/telegram/bot-token`
`gateway/slack/bot-token`
`gateway/google-chat/webhook-url`

Fixed SaaS gateways pin credential-bearing requests to canonical provider hosts. Self-hosted gateways require HTTPS unless the endpoint is loopback. Generic webhooks are intentionally an explicit high-trust integration choice. Relay-mode credentials can only be sent to the configured Relay endpoint; plaintext Relay is accepted only on loopback.

## Gateway catalog

The built-in catalog mirrors the current Hermes messaging-gateway surface and includes Telegram, Discord, Slack, Google Chat, WhatsApp, WhatsApp Cloud API, Signal, SMS/Twilio, Email/SMTP, Home Assistant, Mattermost, Matrix, DingTalk, Feishu/Lark, WeCom, WeCom Callback, Weixin/WeChat, BlueBubbles, Photon, QQ Bot, Yuanbao, Microsoft Teams, Teams Meetings, Microsoft Graph Webhook, LINE, ntfy, SimpleX, Open WebUI, generic Webhooks, Raft, IRC, and Buzz.

Three delivery modes are supported:

- `native_http` for stable bot/API or webhook surfaces that OnePane can call directly.
- `smtp` for direct email delivery.
- `relay` for local-protocol or connector-owned gateways such as Signal, WhatsApp/Baileys, SimpleX, iMessage bridges, IRC, and similar surfaces. Relay accepts a normalized `/v1/messages/send` envelope and keeps platform-specific sockets/credentials outside OnePane when desired.

The catalog also records inbound, outbound, threads, images, files, reactions, typing, streaming, and voice capability metadata. M30 consumes the outbound/thread subset. Conversational inbound ingress can be added later without changing target or connection identity.

## Targets and threads

A `GatewayConnection` describes one configured platform account/bot. A `GatewayTarget` is a stable named destination under that connection. Targets hold an address plus optional `thread_ref`.

Examples:

- Discord: address = channel or thread channel ID; optional thread_ref = message ID for a reply reference.
- Telegram: address = chat ID; thread_ref = forum topic/message-thread ID.
- Slack: address = channel ID; thread_ref = thread timestamp.
- Mattermost: address = channel ID; thread_ref = root post ID.
- Matrix: address = room ID.

This keeps routing out of prompts. A Routine can simply reference a target such as `Ops / nightly-builds`.

## Rules and Routine notifications

`NotificationRule` subscribes to an Event Ledger event type and optionally narrows by aggregate type, aggregate ID, or Routine ID. Every matched event creates a durable idempotent `NotificationDelivery`.

A convenience API binds a Routine directly to a target. It creates a rule for verified `task.completed` events associated with that Routine's occurrences, rather than triggering from a model's completion declaration.

Templates support `{{event_type}}`, `{{aggregate_id}}`, and `{{payload}}`; verified task-completion notifications additionally expose authoritative `{{task_objective}}`, `{{task_result}}`, and `{{routine_name}}`. Richer structured templates can be added without changing the delivery state machine.

## API surface

- `GET /v1/gateway-presets`
- `POST /v1/vault/gateway-credentials`
- `GET|POST /v1/gateways`
- `GET|POST /v1/gateway-targets`
- `GET|POST /v1/notification-rules`
- `POST /v1/notifications`
- `POST /v1/routines/{routineID}/notification-targets`

The WebUI will later wrap these APIs with platform-specific setup forms, connection tests, target pickers, and Routine notification controls.
