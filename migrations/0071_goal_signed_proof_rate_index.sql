-- Bounded L5 metadata-only proof throughput. The signer/accepted-at index
-- makes per-source fixed-window quotas independent of historical ledger size.
-- The existing proof history remains immutable.
CREATE INDEX idx_goal_signed_proof_signer_recent
 ON project_goal_signed_observation_proofs(signer_id,accepted_at DESC);
