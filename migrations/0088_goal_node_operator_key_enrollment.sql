-- P0-AG: the approving authenticated human explicitly selects one Ed25519
-- operator public key; the immutable one-shot grant binds its hash.
-- Legacy exported grants lack the key and deliberately fail closed.
ALTER TABLE project_goal_node_probe_execution_permits
 ADD COLUMN operator_public_key_sha256 TEXT NOT NULL DEFAULT ''
 CHECK(operator_public_key_sha256='' OR
  (length(operator_public_key_sha256)=71 AND substr(operator_public_key_sha256,1,7)='sha256:'));
CREATE TRIGGER goal_node_probe_operator_key_immutable
 BEFORE UPDATE OF operator_public_key_sha256
 ON project_goal_node_probe_execution_permits
 BEGIN SELECT RAISE(ABORT,'Node operator identity immutable'); END;
