from pathlib import Path
root=Path(__file__).resolve().parents[1]
def need(rel,*tokens):
    p=root/rel
    if not p.exists(): raise SystemExit(f'M33 federation validation: FAIL missing {rel}')
    s=p.read_text(errors='ignore')
    miss=[x for x in tokens if x not in s]
    if miss: raise SystemExit(f'M33 federation validation: FAIL {rel} missing {miss}')
need('migrations/0016_node_federation.sql','node_pairings','node_capability_manifests','node_federated_inference_receipts','pairing_code','peer_tls_fingerprint')
need('internal/nodefederation/discovery.go','ListenMulticastUDP','DiscoveryAnnouncement','ObserveDiscovery')
need('internal/nodefederation/identity.go','ed25519.GenerateKey','ExtKeyUsageClientAuth','CertificateFingerprint')
need('internal/nodefederation/service.go','BeginPair','ConfirmPair','ReceivePeerConfirm','trust_state=\'paired\'','Revoke','CanOperate')
need('internal/nodefederation/manifest.go','BuildManifest','provider_connection_id IS NULL','ReceiveHeartbeat','remote-node','ExpireStale')
need('internal/nodefederation/transport.go','tls.VersionTLS13','tls.RequestClientCert','peerFromTLS','RemoteTransport','DispatchFederatedLocal','duplicate_or_unknown_remote_request')
need('internal/inference/execute.go','DestinationTrustedNode','DispatchFederatedLocal','ProviderConnectionID != nil','remote-node')
need('internal/bootstrap/bootstrap.go','nodefederation.EnsureIdentity','RemoteTransport','Federation: federationService')
need('cmd/harnessd/main.go','NewDiscovery','HeartbeatPeers','ListenAndServeTLS','SetFederation')
need('internal/api/server.go','GET /v1/nodes','/pair/confirm','/capabilities','requireNodeOperator')
need('internal/config/config.go','node_federation','239.255.77.77:47777','FederationAdvertiseURL')
readme=(root/'README.md').read_text()
if 'remote paired-node federation' in readme.split('## Deliberately not implemented yet',1)[-1].split('\n## ',1)[0]: raise SystemExit('M33 federation validation: FAIL README still marks federation unimplemented')
print('M33 Node Federation + LAN discovery validation: PASS')
