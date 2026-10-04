-- M15 OperationCoordinator audit snapshot additions.
-- ExecutionPermit remains ephemeral and is intentionally not persisted.

ALTER TABLE operations ADD COLUMN capability_lease_id TEXT REFERENCES capability_leases(id);
ALTER TABLE operations ADD COLUMN capability_lease_revision INTEGER CHECK (capability_lease_revision IS NULL OR capability_lease_revision >= 1);
ALTER TABLE operations ADD COLUMN resource_lease_id TEXT REFERENCES resource_leases(id);
ALTER TABLE operations ADD COLUMN required_verification TEXT CHECK (required_verification IS NULL OR required_verification IN ('V0','V1','V2','V3','V4','V5'));
ALTER TABLE operations ADD COLUMN required_approval TEXT CHECK (required_approval IS NULL OR required_approval IN ('none','approver','admin'));

CREATE INDEX idx_operations_resource_state
ON operations(workspace_id, resource_ref, state, updated_at);
