# Trusted Project preview ingress

OnePane never exposes a Project container directly to the LAN/WAN. The sandbox
runner publishes declared ports only to dynamic `127.0.0.1` host ports. M22
adds a trusted ingress layer above those mappings.

## Evidence chain

A usable preview route must be derived from the exact V2+ application
Observation that proves all of the following:

- the expected Project application is running;
- sandbox isolation verification passed;
- the container spec hash is known;
- the declared internal port is mapped to `127.0.0.1:<dynamic-port>`.

`project_endpoint_routes` records the Observation, Verification, application and
endpoint revisions, and container spec hash. A later application observation
marks existing routes stale before new routes are published. The ingress server
never calls Podman/Docker directly and never accepts an upstream URL from an HTTP
request.

## Browser-origin isolation

Untrusted Project HTML must not execute under the OnePane GUI/API origin. A
same-origin `/proxy/...` design would allow sandbox JavaScript to inherit
control-plane browser privileges.

Each endpoint therefore receives a distinct origin:

```text
local:
  http://ep-<hash>.localhost:8081/

remote example:
  https://ep-<hash>.preview.onepane.example/
```

The remote form requires wildcard DNS/TLS at the trusted reverse proxy. The
OnePane installer will eventually configure this where supported.

## Preview session

The control-plane API authenticates the user and authorizes `project.read`, then
mints a one-minute, one-time bootstrap URL on the endpoint's preview origin.
The preview listener exchanges it for an HttpOnly, SameSite=Strict, host-only
session cookie and redirects to `/`, removing the bootstrap token from the
steady-state URL.

Preview access is not merely trusted for the lifetime of that cookie. On each
request, OnePane rechecks the principal, API credential revocation/expiry,
Workspace status/membership, Workspace scope and `project.read` capability
scope. Session state is memory-only, so daemon restart invalidates all preview
sessions.

## Proxy boundary

The proxy:

- supports verified HTTP endpoints first;
- dials only the exact loopback port in the durable verified route;
- never uses environment proxy settings or DNS for the upstream;
- strips `Authorization` and `Proxy-Authorization`;
- removes the reserved OnePane preview cookie before forwarding while allowing
  ordinary app cookies;
- drops any upstream attempt to overwrite the reserved preview cookie;
- applies request/known-response size bounds and connection/header timeouts.

HTTPS-inside-sandbox and raw TCP ingress are declared in the domain model but
are not yet exposed through the browser preview listener. They require explicit
transport policy rather than disabling TLS verification.

## Public is not published

An endpoint whose declaration says `exposure=public` does **not** become
anonymous or internet-accessible in M22. It remains an authenticated preview.
Actual publication will be a separate high-risk Operation with approval,
hostname/TLS policy, observation and rollback/compensation semantics.
