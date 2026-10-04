# Provider onboarding, zero-cost routing, and Browser Workspace

This document defines the v0.1 first-run provider experience and the boundary
between autonomous inference and user-operated web subscriptions.

## First-run priority

The installer should make a useful harness available without requiring the user
to buy API credits.

Recommended order:

1. Discover usable local ModelDeployments.
2. Offer OmniRoute as the recommended cloud-gateway preset.
3. Configure OmniRoute in strict zero-cost mode for the default first-run path.
4. Probe `/v1/models` and OmniRoute settings before marking the route ready.
5. Offer paid/API-key providers later as explicit opt-ins.
6. Offer ChatGPT-plan sharing only as an optional protected fallback.
7. Expose Browser Workspace for human-operated use of existing web subscriptions.

OmniRoute is registered through the normal ProviderConnection + Model +
ModelDeployment path. It is not trusted merely because it runs locally: the
models behind it are remote providers and remain subject to information-flow
policy.

## Cost and allowance semantics

Scheduler cost classes are deliberately not collapsed into a single "free"
boolean:

- `local`: local compute. No external inference charge.
- `free`: externally hosted capacity identified as free. It is autonomous by
  default only when `hard_zero_incremental_cost=true`.
- `included_subscription`: no per-request API bill, but consumes a finite user
  subscription allowance. Protected by default.
- `provider_managed`: the upstream gateway decides cost/provider. Treated as
  potentially billable unless a stronger guarantee is established.
- `paid`: known billable capacity.
- `unknown`: cost cannot be established. Treated as potentially billable.

Two independent RouteRequest permissions exist:

- `AllowSubscriptionUsage`: permits consumption of an included subscription
  allowance.
- `AllowPotentialMonetarySpend`: permits routes that may incur money cost.

Both default to false. Granting one never grants the other.

Even after `AllowSubscriptionUsage=true`, subscription-backed routes remain a
fallback tier. Any eligible local or proven-hard-$0 route sorts ahead of them.

## OmniRoute

The built-in OmniRoute preset targets the local OpenAI-compatible endpoint:

```text
http://127.0.0.1:20128/v1
```

The first-run preset defaults to zero-cost-only behavior. The harness probes:

- `GET /v1/models`
- OmniRoute's exported settings when available

A route may be classified `free + hard_zero_incremental_cost=true` only when the
harness can verify OmniRoute's `freeAccessPolicy` is `strict` for that setup.
Otherwise it remains `provider_managed` and autonomous scheduling requires
explicit potential-spend permission.

This is intentionally conservative. OmniRoute's ordinary `auto` route may
include free, subscription, or paid providers depending on the user's
configuration. The harness must not infer a cost guarantee from the `auto` name.

## ChatGPT plan sharing

The harness may support OpenAI's public Sign in with ChatGPT flow for eligible
open-source/local applications. This is an automated inference provider, but it
is **not a free provider**. It uses the user's included ChatGPT plan allowance.

Rules:

- Provider cost class is `included_subscription`.
- It is never scheduler-eligible merely because the account is connected.
- A route must explicitly set `AllowSubscriptionUsage=true`.
- Local/proven-$0 capacity remains preferred.
- UI must identify when the ChatGPT plan is being used and expose usage-management
  controls when the OpenAI integration is implemented fully.
- Tokens belong in the future Vault/Secret Broker, not browser storage, logs, or
  model context.

OpenAI currently documents this OSS flow at:

- https://developers.openai.com/siwc/token-sharing-open-source/sign-in
- https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions

## Browser Workspace

Browser Workspace is a different product surface from an automated inference
provider. It exists so a user can use web applications they already subscribe
to inside the harness experience without giving the autonomous scheduler those
sessions.

Default policy:

```text
automation_mode       = human_only
credential_boundary   = browser_profile_only
credential_extraction = forbidden
subscription_use      = user_initiated_only
import_mode            = explicit_user_import
schedulable            = false
```

The user logs into ChatGPT, Claude, Gemini, or another service in the normal web
experience. Cookies, session tokens, local storage, and other browser credentials
remain inside that browser profile. Harness workers may not scrape/extract them
and Browser Workspace is never a scheduler candidate.

A user may explicitly import selected output back into the harness. Imported AI
output is labelled `UNVERIFIED_DERIVED`; ordinary web content is
`UNTRUSTED_CONTENT`. Neither becomes authoritative simply because the user viewed
it in a logged-in browser.

### Why this is separate

A browser session can make existing subscriptions more useful, but web-service
usage is still governed by each service's plan limits and terms. The harness
therefore does not call it "free" and does not automate it by default.

For OpenAI specifically, current product documentation separates ordinary Chat
usage limits from the Work/Codex allowance used by app token-sharing. That makes
manual ChatGPT web use a useful separate option, but it still consumes whatever
Chat allowance applies to the user's plan.

## Future WebGUI/Desktop implementation

M20 should expose Browser Workspace as an isolated browser/webview or launchable
system-browser surface rather than an iframe assumption. Cross-origin framing,
service CSP, authentication flows, and passkeys make a generic iframe unreliable.

The important invariant is independent of rendering technology:

**browser credentials stay in the browser boundary; autonomous control-plane
credentials stay in Vault; neither is silently converted into the other.**

### OmniRoute credential naming

Credentials stored on behalf of OmniRoute are deliberately separated from credentials intended for direct OnePane provider connections:

```text
omniroute/xai/api-key       # xAI credential intended for OmniRoute
provider/xai/api-key        # xAI credential intended for direct OnePane use
```

The same convention applies to OpenAI, Anthropic, Groq, Google, or another upstream. An optional OmniRoute gateway bearer token uses `omniroute/gateway/access-token`.

This separation is preserved in Vault metadata and the WebGUI so users can opt into OmniRoute without accidentally changing or reusing direct-provider credentials.
