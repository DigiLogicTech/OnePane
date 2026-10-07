-- Alpha 3.2 Models remediation: first-class llama.cpp runtime lifecycle.

INSERT OR IGNORE INTO managed_component_states(
    component_id,available_version,desired_state,observed_state,metadata_json,revision,updated_at
)
VALUES(
    'llamacpp','b11430','disabled','not_installed','{}',1,
    CAST(strftime('%s','now') AS INTEGER)*1000
);
