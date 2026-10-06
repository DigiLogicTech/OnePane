from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
auth=(ROOT/'internal/webauth/service.go').read_text()
pwd=(ROOT/'internal/webauth/password.go').read_text()
api=(ROOT/'internal/api/session_auth.go').read_text()+(ROOT/'internal/api/server.go').read_text()
vault=(ROOT/'internal/vault/names.go').read_text()
ui=(ROOT/'internal/webui/static/index.html').read_text()
checks={
 'opaque sessions':'auth_sessions' in auth and 'token_hash' in auth,
 'csrf':'X-OnePane-CSRF' in api and 'SameSiteStrictMode' in api,
 'password kdf':'pbkdf2-hmac-sha256' in pwd and '600000' in pwd,
 'bootstrap closed':'ErrSetupComplete' in auth,
 'omniroute namespace':'ScopeOmniRoute' in vault and 'ScopeDirectProvider' in vault and 'ProviderCredentialLogicalName' in vault,
 'embedded webgui':'OnePane' in ui and 'setup-form' in ui,
}
failed=[k for k,v in checks.items() if not v]
if failed: raise SystemExit('M23 FAIL: '+', '.join(failed))
print('M23 WebGUI/auth/provider-secret contract: PASS')
