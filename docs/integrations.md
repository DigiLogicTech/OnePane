# OnePane M29 integrations

M29 adds three explicit integration registries: cloud inference providers, external agent/harness runtimes, and SaaS tool connectors. They are convenience/onboarding layers over existing OnePane security primitives; none of them bypasses Policy, CapabilityLease, BudgetReservation, ToolGateway, OperationCoordinator, Observation, Verification or Vault.

## Cloud inference providers

The built-in provider catalog contains 27 presets:

- OmniRoute and protected ChatGPT-plan fallback.
- OpenAI, Anthropic, Google Gemini, Google Vertex AI, Azure OpenAI and Amazon Bedrock.
- xAI, Mistral, Groq, DeepSeek, OpenRouter, Together AI, Fireworks AI, Cerebras, Cohere and Perplexity.
- Cloudflare Workers AI, NVIDIA NIM, SambaNova, Hugging Face Inference, Alibaba Model Studio, Nebius AI, Moonshot/Kimi and MiniMax.
- Custom OpenAI-compatible endpoint.

Most providers use the hardened `openai-compatible` transport. Anthropic uses the native Messages adapter and is normalized into OnePane's canonical inference response envelope. Provider registrations do not become schedulable merely because a credential exists: discovery/probe/qualification and the normal Scheduler rules still apply.

Fixed provider presets pin credential-bearing requests to their canonical host or host suffix. HTTPS is mandatory except explicit loopback development endpoints. A custom OpenAI-compatible endpoint is intentionally operator-supplied and should use a credential created only for that endpoint.

Provider credentials are direct-provider scoped, for example:

```text
provider/openai/api-key
provider/anthropic/api-key
provider/xai/api-key
```

They are different records from OmniRoute credentials such as:

```text
omniroute/xai/api-key
omniroute/gateway/access-token
```

Paid, subscription and provider-managed candidates remain subject to M27 BudgetReservation enforcement.

### Cloud identity notes

Vertex AI currently accepts a supplied Google Cloud access token through the provider preset; automated ADC/service-account token refresh is a later Secret Broker extension. Bedrock supports the OpenAI-compatible runtime with a Bedrock API key. Azure's `api-key` header is sent raw rather than being rewritten as a Bearer credential.

## External harness and agent runtimes

The built-in catalog contains 33 presets spanning:

- Hermes Agent.
- Microsoft Agent Framework, Google ADK, AWS Strands, OpenAI Agents SDK, LangGraph, LangChain Deep Agents, CrewAI, Pydantic AI and LlamaIndex agents.
- AutoGen and Semantic Kernel for existing deployments.
- OpenHands, goose, OpenCode, OpenClaw, Claude Code, OpenAI Codex CLI, Gemini CLI, Aider, Qwen Code, Kilo Code and Pi.
- Dify, Flowise, Langflow and n8n AI/workflow agents.
- Cursor, Windsurf, Cline and Roo Code as opaque human-only registrations.
- Custom OnePane Agent Protocol and custom A2A bridge presets.

A preset is not a grant of authority. Autonomous runtimes must communicate through the canonical proposal-only OnePane Agent Protocol bridge. OnePane remains the owner of tools, budgets, credentials, policy and verification. Runtimes that execute arbitrary native tools outside that boundary are registered as `unmanaged` and remain excluded from autonomous scheduling.

Runtime credentials use a separate Vault namespace:

```text
agent-runtime/claude-code/access-token
agent-runtime/hermes/access-token
```

Credential-bearing remote runtime endpoints require HTTPS; loopback HTTP remains available for local bridges.

## SaaS plugins / connectors

The built-in connector registry contains 20 services:

- Gmail, Google Calendar and Google Drive.
- Outlook / Microsoft 365 Mail and Calendar, OneDrive, SharePoint and Teams.
- GitHub and GitLab.com.
- Slack and Discord.
- Notion and Linear.
- Dropbox, Asana, Jira Cloud, Confluence Cloud, HubSpot and Todoist.

Each plugin is registered as ToolGateway capabilities rather than handing its API client directly to a model. A typical plugin exposes:

```text
plugin.github.read
plugin.github.mutate

plugin.gmail.read
plugin.gmail.mutate
plugin.gmail.send
```

Read operations use `READ` and minimum V1 verification. Mutations use `MUTATE` and minimum V2 verification. Email/message delivery uses `EXTERNAL_SEND` and minimum V3 verification. The ordinary OperationCoordinator rules still apply to mutation and send actions.

The generic connector adapter accepts only a relative API path, method, query map, JSON body and Vault `secret_ref`. The service origin comes from the trusted preset. It does not accept an arbitrary URL. Authentication/cookie/host headers cannot be supplied by the caller, redirects are not followed, and credential references are validated against the exact plugin namespace before plaintext resolution.

Special handling prevents privilege-class confusion:

- Gmail/Outlook/Teams/Slack/Discord send paths can only be invoked through the `*.send` tool.
- POST-based read APIs are explicitly allow-listed rather than treating every POST as a read.
- Linear GraphQL bodies containing mutations/subscriptions are rejected by the read tool.
- Slack logical `{ "ok": false }` responses are failures even when HTTP returns 200.

Plugin credentials use their own Vault namespace:

```text
plugin/gmail/access-token
plugin/outlook-mail/access-token
plugin/github/access-token
plugin/slack/access-token
```

## REST onboarding surface

Authenticated operators can use:

```text
GET  /v1/provider-presets
POST /v1/providers/probe
POST /v1/providers

GET  /v1/agent-runtime-presets
POST /v1/agent-runtimes
POST /v1/agent-runtimes/{connectionID}/status

GET  /v1/plugin-presets
POST /v1/vault/plugin-credentials
POST /v1/vault/agent-runtime-credentials
```

The existing dedicated OmniRoute endpoints remain available.

## Current credential UX limitation

M29 is the backend integration layer. OAuth-heavy products such as Google Workspace and Microsoft 365 currently expect an operator-supplied access token/API credential stored in OnePane Vault. Browser authorization, refresh-token lifecycle, delegated consent screens and account-picker UX will be added during the final WebUI/onboarding phase. This limitation is intentionally explicit rather than hiding browser sessions or long-lived refresh credentials inside a generic adapter.
