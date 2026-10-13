-- Historic signer approvals cannot become effective when a human
-- edits a monitoring plan then restores byte-identical JSON.
ALTER TABLE project_goal_observation_signers
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 0 CHECK(plan_generation>=0);

-- Earlier signers receive generation zero and intentionally fail closed
-- until newly enrolled by a human for the current Goal generation.
CREATE TRIGGER goal_observation_signer_generation_immutable
 BEFORE UPDATE OF plan_generation ON project_goal_observation_signers
 BEGIN SELECT RAISE(ABORT,'signed source plan generation immutable'); END;
