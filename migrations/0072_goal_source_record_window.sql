-- RC11 Level 5: a durable conservative quota on unauthenticated-yet
-- content-labelled source submissions. Reservation is made before storing
-- an Observation and is not automatically refunded on a subsequent storage
-- failure (fail closed). Source records remain UNVERIFIED until separately
-- Ed25519 signed and accepted.
ALTER TABLE project_goal_observation_signers ADD COLUMN
 source_record_window_start INTEGER NOT NULL DEFAULT 0
 CHECK(source_record_window_start>=0);
ALTER TABLE project_goal_observation_signers ADD COLUMN
 source_record_window_count INTEGER NOT NULL DEFAULT 0
 CHECK(source_record_window_count BETWEEN 0 AND 30);
