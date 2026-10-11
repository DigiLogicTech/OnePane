# RC11 — Inbound federation model admission fail-closed

An mTLS-paired Node may issue remote inference only to an explicitly
admitted local deployment. The federation HTTP handler previously
skipped `FederatedInferenceAllowed` when the model manager was nil.
This implicitly trusted any requested local deployment if the
service was constructed without a model manager, regardless of whether
the deployment was admitted or safe for remote use.

The inbound handler now rejects every request if the model-admission
manager is absent or returns false. No execution receipt, remote
model action, raw prompt forwarding or silent model fallback occurs.
The existing verified mTLS integration suite now uses an explicitly
allowlisted test model manager and separately asserts that disabling
this authority refuses a new inference request before receipt insertion.

Production harnessd supplies the configured LocalAI manager; this
hardening ensures that misconfigured or future server call sites do
not accidentally bypass the admission gate. It does not substitute
for remote physical acceptance, and RC11 installer review gates
remain unchanged.
