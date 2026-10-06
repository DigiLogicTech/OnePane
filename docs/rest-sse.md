# M13 REST + SSE surface

`harnessd` now exposes a small versioned API intended for the future native WebGUI and trusted automation clients.

## Authentication

Only `GET /v1/health` is anonymous. Protected routes currently accept scoped API credentials. The bearer token itself is never stored; the database contains its SHA-256 hash.

Each API credential is constrained by both:

- `workspace_scope_json`;
- `capability_scope_json`.

Mutation handlers derive the actor principal from the authenticated credential. JSON clients cannot impersonate another actor by supplying a principal ID.

Useful M13 capability scopes are:

- `project.read`;
- `project.write`;
- `project.run`;
- `project.review`;
- `events.read`.

`project.*` or `*` may be used when an administrator intentionally issues a broader credential. Active Workspace membership is still required for normal human/agent/service principals.

The later WebGUI authentication milestone will adapt opaque server-side sessions into the same authorization boundary rather than weakening these checks.

## Project endpoints

Current Project Workspace routes include:

- `POST /v1/projects`
- `GET /v1/projects/{projectID}`
- `POST|GET /v1/projects/{projectID}/runtime`
- `POST /v1/project-runtimes/{runtimeID}/desired-state`
- `POST|GET /v1/project-runtimes/{runtimeID}/applications`
- `POST|GET /v1/project-runtimes/{runtimeID}/endpoints`
- `POST|GET /v1/projects/{projectID}/changes`
- `POST /v1/project-changes/{proposalID}/review`
- `POST|GET /v1/projects/{projectID}/routine-bindings`

JSON request bodies are limited to 1 MiB and reject unknown fields.

## SSE

`GET /v1/events/stream?workspace_id=<id>&after=<sequence>` streams only events belonging to the authorized Workspace.

The SSE `id` is the durable Event Ledger sequence. Clients should reconnect using the last successfully applied sequence. `once=1` is available for bounded polling/tests.

The server sends heartbeat comments on otherwise idle long-lived streams. SSE is not used to bypass authorization and does not expose global events through a Workspace stream.

## Network exposure

The default listener remains `127.0.0.1:8080`. Configuring a non-loopback listener requires an HTTPS `server.public_origin`, making the intended deployment model explicit: terminate TLS at a trusted reverse proxy rather than sending bearer credentials over plain LAN HTTP.

## M29 integration catalog/onboarding APIs

Authenticated clients can enumerate and register the M29 integration surfaces:

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

Catalog reads require an authenticated principal. Provider/runtime registration and Vault credential creation remain capability-scoped control-plane operations; catalog membership itself never grants execution authority. Plugin execution is exposed only through the registered ToolGateway capabilities (`plugin.<id>.read`, `.mutate`, and where supported `.send`).

## M30 messaging gateway APIs

Authenticated clients can enumerate gateway presets and manage durable outbound routing:

- `GET /v1/gateway-presets`
- `POST /v1/vault/gateway-credentials`
- `GET|POST /v1/gateways`
- `GET|POST /v1/gateway-targets`
- `GET|POST /v1/notification-rules`
- `POST /v1/notifications`
- `POST /v1/routines/{routineID}/notification-targets`

Gateway write operations are workspace-authorized. The Routine convenience endpoint derives the authoritative workspace from the Routine before authorization; it does not trust a caller-supplied workspace ID.

## M32 Bot Runtime

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

Bot resources use workspace capabilities `bot.read`, `bot.write`, and `bot.chat`. Native Bot chat is human-interactive and is not an autonomous Task execution path.


## M35 model profile and Testbed APIs

Managed local deployments expose their provenance-separated spec sheet and manual admission workflow:

```text
GET  /v1/model-deployments/{deploymentID}/spec-sheet
POST /v1/model-deployments/{deploymentID}/testbed/sessions
GET  /v1/model-testbed/{sessionID}
GET  /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/turns
POST /v1/model-testbed/{sessionID}/complete
POST /v1/model-deployments/{deploymentID}/admission
```

An Admin on the origin/master can perform the equivalent workflow on a paired node after that target has enabled remote model management:

```text
GET  /v1/nodes/{nodeID}/models/{deploymentID}/spec-sheet
POST /v1/nodes/{nodeID}/models/{deploymentID}/testbed/sessions
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}
GET  /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns
POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/complete
POST /v1/nodes/{nodeID}/models/{deploymentID}/admission
```

`accepted` and `restricted` admission require a completed Testbed session. Synthetic tool probes test output/tool-call formatting only and never carry ToolGateway authority.
