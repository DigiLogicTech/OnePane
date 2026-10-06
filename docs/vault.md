# Built-in Vault and Secret Broker

M18 replaces the bootstrap environment-variable workaround with a built-in encrypted secret store. No external Vault service is required for the normal single-node product.

## Encryption model

Each secret version receives a random 256-bit data-encryption key (DEK). The secret value is encrypted with AES-256-GCM and authenticated metadata derived from the immutable SecretRecord identity. The DEK is separately encrypted by the local 256-bit key-encryption key (KEK). The database therefore stores ciphertext, a wrapped DEK, and nonces rather than plaintext.

The local KEK is generated at first bootstrap under `<data_dir>/vault/master.key`. The file must remain mode 0600 or stricter; startup fails closed on unsafe permissions or invalid key length. A root-compromised host remains outside the v0.1 security claim.

## Versioning

Creating a new value for an active logical secret creates a new SecretRecord version and retires the previous active version atomically. Revocation is explicit and auditable.

ProviderConnection and trusted adapter configuration should refer to a secret as `vault:<secret_record_id>`. The Secret Broker resolves that value only inside trusted transports/adapters. The raw value is not placed in Model input, Task context, tool JSON, repository content, or general logs.

## Future hardening

The envelope format leaves room for KEK rotation and alternative OS/TPM-backed key protectors without changing SecretRecord semantics. Multi-node secret distribution remains out of scope until the federated trust model explicitly supports it.

## Provider credential namespaces

Provider credentials have an explicit consumption namespace so users and services can distinguish credentials intended for a direct provider connection from credentials managed for OmniRoute.

Canonical logical names are:

```text
provider/<upstream-provider>/api-key
omniroute/<upstream-provider>/api-key
omniroute/gateway/access-token
```

Examples:

```text
provider/xai/api-key
omniroute/xai/api-key
provider/openai/api-key
omniroute/openai/api-key
```

The secret metadata also records `credential_scope`, `upstream_provider`, `credential_kind`, and a display label. Creating a new value with the same logical name rotates that logical credential to a new SecretRecord version.

The names are not cosmetic: `ValidateProviderCredentialRef` can enforce that an OmniRoute-scoped secret is not silently reused as a direct-provider credential, and vice versa. The WebGUI/API creates provider credentials through this typed path rather than arbitrary logical-name input.
