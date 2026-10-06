# OnePane Node Federation (M33)

OnePane nodes form a peer mesh for **inference capacity**, not a shared-authority cluster.
Any node may originate a Task. For that Task the origin node is the coordinator/master and
retains Task state, policy, budgets, Vault secrets, tool authority and verification.

## LAN discovery

By default each node advertises a small JSON beacon on the configured IPv4 multicast group
(`239.255.77.77:47777`). The receiver derives the source address from the packet and records:

- node ID and display name
- stable bootstrap identity fingerprint
- federation TLS certificate fingerprint
- federation listener port and protocol version

A discovered node is always `discovered`. Discovery cannot make it `paired` and discovered
model capacity never enters the scheduler.

## Pairing

1. Select a discovered node and call `POST /v1/nodes/{nodeID}/pair`.
2. OnePane opens the peer's HTTPS federation endpoint only after pinning the certificate
   fingerprint learned during discovery.
3. Both nodes show the same six-digit pairing code.
4. An Admin confirms the code independently on each node.
5. Only after both confirmations do both records become `paired`.

Post-pairing requests use TLS 1.3 with client certificates. The receiving node accepts a
client certificate only when its SHA-256 fingerprint matches an active paired record.
Revoking the peer invalidates scheduler-visible remote deployments immediately.

## Capability manifests

Paired nodes send a heartbeat plus a short-lived capability manifest. M33 exports only
model deployments that are:

- physically local to the peer (`node_id == local node`)
- non-provider-backed
- `ready` or `degraded`
- backed by a trusted/user-trusted Model

Cloud-provider deployments are intentionally not exported. This prevents a peer from
using another node's cloud API credentials or subscription allowance.

Remote model deployments are projected into the origin catalog with `runtime_name=remote-node`
and compatibility profiles copied from the peer manifest. Stale peers become unavailable.

## Remote inference

The scheduler treats a healthy paired remote deployment as `trusted_node` capacity. The
origin performs normal information-flow evaluation before dispatch. The remote request
contains only the bounded inference request plus identifiers; it does not contain origin
CapabilityLeases, ToolGateway credentials or Vault secrets.

The receiving node refuses provider-backed or non-local deployments. This enforces
`max_hops = 1`: a federated request cannot be federated again.

Each inbound request has a durable receipt keyed by `(peer_node_id, remote_request_id)`.
A repeated successful request returns the stored response. An `executing`/`unknown` request
is never blindly replayed because the original inference may have already consumed resources.

## Configuration

```yaml
node_federation:
  enabled: true
  listen: "0.0.0.0:18443"
  # advertise_url: "https://192.168.1.20:18443"
  discovery_enabled: true
  discovery_multicast: "239.255.77.77:47777"
  heartbeat_seconds: 10
  stale_seconds: 35
```

`advertise_url` is normally auto-detected from the first active non-loopback IPv4 interface.
Set it explicitly on multi-homed/VLAN systems.
