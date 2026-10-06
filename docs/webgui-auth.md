# M23 WebGUI and first-run authentication

OnePane now serves an embedded first-party WebGUI from the control-plane origin. It does not depend on a CDN, Node.js runtime, or external web service at runtime.

## First run

`GET /v1/setup/status` is intentionally unauthenticated and reveals only whether first-run administrator setup is required plus non-sensitive counts. The first successful `POST /v1/setup/admin` transaction creates:

- the first human Principal;
- the default Workspace;
- active WorkspaceMembership;
- the human Admin Role/assignment;
- a `local_password` AuthIdentity;
- an opaque AuthSession; and
- the bootstrap-to-commissioning SystemMode transition.

The transaction fails closed once any active human/auth identity exists, so the bootstrap endpoint cannot be reused to mint another administrator.

## Password storage

Local passwords are not stored directly. v0.1 uses PBKDF2-HMAC-SHA256 with a random 256-bit salt, 600,000 iterations, and a 256-bit derived key. The KDF parameters, salt, and derived hash are stored in `auth_identities.credential_json` so the format can be upgraded later. Comparisons are constant-time.

## Browser sessions

The browser receives an opaque random `onepane_session` cookie. Only its SHA-256 hash is stored in `auth_sessions`. The cookie is:

- HttpOnly;
- SameSite=Strict;
- scoped to `/`; and
- Secure whenever the configured control-plane origin is HTTPS.

Sessions have both absolute and idle expiry and are revocable server-side.

Unsafe browser requests also require `X-OnePane-CSRF`, matched against a separate per-session CSRF secret. The CSRF value is exposed through a SameSite=Strict non-HttpOnly cookie because browser JavaScript must echo it; it is not an authentication credential.

Bearer API credentials remain supported for CLI/service automation and are not converted into browser sessions implicitly.

## Origin isolation

The WebGUI stays on the control-plane origin. Project application previews continue to use the separate M22 per-endpoint preview origins. Untrusted app HTML/JavaScript therefore never shares the OnePane origin or its session cookie.
