-- Alpha 3.1 refinement: research teams, skills/tools, OAuth foundations, managed OmniRoute and deployment compute policy.

ALTER TABLE teams ADD COLUMN configuration_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(configuration_json));

CREATE TABLE deployment_compute_policies (
    deployment_id             TEXT PRIMARY KEY REFERENCES model_deployments(id) ON DELETE CASCADE,
    preference                TEXT NOT NULL DEFAULT 'auto' CHECK (preference IN ('auto','prefer_gpu','prefer_cpu','require_gpu','require_cpu','hybrid')),
    placement_mode            TEXT NOT NULL DEFAULT 'auto' CHECK (placement_mode IN ('auto','single_device','cpu_offload','layer_sharded','row_sharded','tensor_sharded','cpu_only')),
    preferred_device_ids_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(preferred_device_ids_json)),
    required_device_ids_json  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(required_device_ids_json)),
    metadata_json             TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    revision                  INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_by                TEXT REFERENCES principals(id),
    created_at                INTEGER NOT NULL,
    updated_at                INTEGER NOT NULL
) STRICT;

CREATE TABLE tool_bundles (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source_kind     TEXT NOT NULL DEFAULT 'builtin' CHECK (source_kind IN ('builtin','custom')),
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','archived')),
    metadata_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE tool_bundle_tools (
    bundle_id       TEXT NOT NULL REFERENCES tool_bundles(id) ON DELETE CASCADE,
    tool_id         TEXT NOT NULL,
    access_class    TEXT NOT NULL DEFAULT 'requested' CHECK (access_class IN ('requested','read','mutating','administrative')),
    required        INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0,1)),
    PRIMARY KEY(bundle_id,tool_id)
) STRICT;

CREATE TABLE agent_profile_tool_bundles (
    profile_id      TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    bundle_id       TEXT NOT NULL REFERENCES tool_bundles(id) ON DELETE CASCADE,
    PRIMARY KEY(profile_id,bundle_id)
) STRICT;

CREATE TABLE skill_packages (
    id              TEXT PRIMARY KEY,
    skill_id        TEXT NOT NULL,
    version         TEXT NOT NULL,
    name            TEXT NOT NULL,
    publisher       TEXT NOT NULL DEFAULT '',
    package_sha256  TEXT NOT NULL CHECK (length(package_sha256)=64),
    package_path    TEXT NOT NULL,
    source_kind     TEXT NOT NULL DEFAULT 'uploaded' CHECK (source_kind IN ('builtin','uploaded')),
    trust_state     TEXT NOT NULL DEFAULT 'unverified' CHECK (trust_state IN ('builtin','verified','unverified','rejected')),
    status          TEXT NOT NULL DEFAULT 'quarantined' CHECK (status IN ('quarantined','installed','disabled','removed','failed')),
    manifest_json   TEXT NOT NULL CHECK (json_valid(manifest_json)),
    requested_by    TEXT REFERENCES principals(id),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(skill_id,version,package_sha256)
) STRICT;
CREATE INDEX idx_skill_packages_status ON skill_packages(status,name,version);

CREATE TABLE skill_assignments (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
    skill_package_id TEXT NOT NULL REFERENCES skill_packages(id) ON DELETE CASCADE,
    subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('agent_profile','team','team_member','workspace')),
    subject_id      TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    configuration_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(configuration_json)),
    assigned_by     TEXT REFERENCES principals(id),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(workspace_id,skill_package_id,subject_kind,subject_id)
) STRICT;

CREATE TABLE team_presets (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source_kind     TEXT NOT NULL DEFAULT 'builtin' CHECK (source_kind IN ('builtin','custom')),
    configuration_json TEXT NOT NULL CHECK (json_valid(configuration_json)),
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    revision        INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE provider_oauth_configs (
    preset_id          TEXT PRIMARY KEY,
    authorization_url  TEXT NOT NULL,
    token_url          TEXT NOT NULL,
    client_id          TEXT NOT NULL,
    scopes_json        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(scopes_json)),
    extra_auth_json    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(extra_auth_json)),
    enabled            INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    revision           INTEGER NOT NULL DEFAULT 1,
    updated_at         INTEGER NOT NULL
) STRICT;

CREATE TABLE provider_oauth_flows (
    id                 TEXT PRIMARY KEY,
    workspace_id       TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    preset_id          TEXT NOT NULL,
    state              TEXT NOT NULL UNIQUE,
    code_verifier      TEXT NOT NULL,
    redirect_uri       TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('pending','completed','failed','expired','cancelled')),
    requested_by       TEXT NOT NULL REFERENCES principals(id),
    expires_at         INTEGER NOT NULL,
    failure_reason     TEXT,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_provider_oauth_flows_state ON provider_oauth_flows(state,status,expires_at);

CREATE TABLE provider_oauth_connections (
    id                 TEXT PRIMARY KEY,
    workspace_id       TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    preset_id          TEXT NOT NULL,
    access_secret_ref  TEXT NOT NULL,
    refresh_secret_ref TEXT,
    scopes_json        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(scopes_json)),
    account_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(account_json)),
    expires_at         INTEGER,
    status             TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected','expired','revoked','failed')),
    revision           INTEGER NOT NULL DEFAULT 1,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_provider_oauth_connections ON provider_oauth_connections(workspace_id,preset_id,status);

INSERT OR IGNORE INTO managed_component_states(component_id,available_version,desired_state,observed_state,metadata_json,revision,updated_at)
VALUES('omniroute','3.8.51','disabled','not_installed','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000);

INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('core','Core','DigiLogic Core Core capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','context.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','project.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','workspace.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','task.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','notes.write','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('core','structured_output','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('research','Research','DigiLogic Core Research capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','web.search','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','browser.fetch','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','file.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','evidence.capture','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','evidence.cite','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('research','provenance.record','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('code','Code','DigiLogic Core Code capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','repo.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','repo.search','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','file.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','file.write','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','diff.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','terminal.run','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','code.execute','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('code','test.run','mutating',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('operations','Operations','DigiLogic Core Operations capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','logs.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','events.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','service.status','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','process.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','runtime.inspect','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('operations','health.check','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('infrastructure','Infrastructure','DigiLogic Core Infrastructure capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','node.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','node.telemetry','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','storage.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','network.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','container.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('infrastructure','runtime.inventory','mutating',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('security','Security','DigiLogic Core Security capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('security','policy.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('security','permissions.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('security','audit.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('security','secret.reference.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('security','configuration.inspect','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('data','Data','DigiLogic Core Data capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('data','table.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('data','json.transform','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('data','calculate','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('data','chart.generate','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('data','spreadsheet.analyze','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('planning','Planning','DigiLogic Core Planning capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('planning','task.create','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('planning','task.update','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('planning','dependency.plan','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('planning','checkpoint.create','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('planning','project.read','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('verification','Verification','DigiLogic Core Verification capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('verification','test.run','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('verification','artifact.inspect','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('verification','assert.evaluate','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('verification','evidence.compare','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('verification','acceptance.verify','read',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('documentation','Documentation','DigiLogic Core Documentation capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('documentation','file.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('documentation','document.write','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('documentation','diagram.describe','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('documentation','spec.write','mutating',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('model-evaluation','Model Evaluation','DigiLogic Core Model Evaluation capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('model-evaluation','model.read','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('model-evaluation','model.testbed','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('model-evaluation','model.agent_check','read',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('model-evaluation','benchmark.run','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('model-evaluation','runtime.telemetry','mutating',1);
INSERT OR IGNORE INTO tool_bundles(id,name,description,source_kind,status,metadata_json,revision,created_at,updated_at) VALUES('tool-operator','Tool Operator','DigiLogic Core Tool Operator capability bundle.','builtin','active','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('tool-operator','tool.invoke','mutating',1);
INSERT OR IGNORE INTO tool_bundle_tools(bundle_id,tool_id,access_class,required) VALUES('tool-operator','tool.result.inspect','read',1);
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.general',NULL,'General Assistant','Balanced general-purpose reasoning, planning and governed execution.','Act as a balanced general assistant. Clarify objectives from available context, plan proportionally, use only permitted capabilities, surface uncertainty, and verify material claims before completion.','general','inference.general','L1','builtin','active','{"category":"general","recommended_tool_bundles":["core","planning"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.general','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.general','planning');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.researcher',NULL,'Researcher','Evidence-driven investigation and source analysis.','Investigate questions using scoped evidence. Separate observation from inference, assess source quality, preserve citations and uncertainty, and do not treat prior conclusions as evidence.','research','inference.research','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.researcher','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.researcher','research');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.research-lead',NULL,'Lead Researcher','Research decomposition, evidence planning and synthesis coordination.','Lead a research effort by decomposing the question, defining evidence needs, assigning independent lines of inquiry, tracking uncertainty and ensuring synthesis is supported by provenance.','research','inference.research','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research","planning"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-lead','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-lead','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-lead','planning');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.research-critic',NULL,'Research Critic','Skeptical challenge of claims, methods and evidence.','Critique research claims adversarially but constructively. Seek contradictions, unsupported assumptions, missing alternatives, weak evidence and methodological errors.','research','inference.research','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-critic','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-critic','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-critic','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.evidence-auditor',NULL,'Evidence Auditor','Evidence provenance, citation and support auditing.','Audit whether each material claim is supported by allowed evidence, whether citations resolve to the claimed source, and whether provenance is complete and reproducible.','research','inference.research','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.evidence-auditor','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.evidence-auditor','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.evidence-auditor','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.research-synthesizer',NULL,'Research Synthesizer','Synthesis of independent findings into a traceable conclusion.','Synthesize independent research outputs without erasing disagreement. Weight evidence quality, preserve uncertainty, cite supporting evidence and clearly identify unresolved questions.','research','inference.research','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research","documentation"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-synthesizer','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-synthesizer','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.research-synthesizer','documentation');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.technical-researcher',NULL,'Technical Researcher','Technical standards, APIs, implementation evidence and documentation analysis.','Research technical questions from primary documentation, source code and empirical evidence. Distinguish version-specific facts from general principles and record reproducible references.','research','inference.technical','L2','builtin','active','{"category":"research","recommended_tool_bundles":["core","research","code"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.technical-researcher','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.technical-researcher','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.technical-researcher','code');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.software-engineer',NULL,'Software Engineer','Implementation, debugging, refactoring and testing.','Implement software changes narrowly and safely. Read surrounding code first, preserve established contracts, run relevant tests, and report assumptions and regressions.','engineering','inference.coding','L2','builtin','active','{"category":"engineering","recommended_tool_bundles":["core","code"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.software-engineer','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.software-engineer','code');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.code-reviewer',NULL,'Code Reviewer','Correctness, security, maintainability and regression review.','Review code for correctness, security, maintainability, compatibility and test coverage. Prefer concrete findings tied to code paths over stylistic preference.','engineering','inference.coding','L2','builtin','active','{"category":"engineering","recommended_tool_bundles":["core","code","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.code-reviewer','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.code-reviewer','code');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.code-reviewer','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.systems-architect',NULL,'Systems Architect','Architecture, interfaces, trade-offs and system decomposition.','Design systems by making requirements, boundaries, interfaces, failure modes and trade-offs explicit. Prefer simple composable contracts and identify migration risks.','engineering','inference.reasoning','L2','builtin','active','{"category":"engineering","recommended_tool_bundles":["core","planning","documentation"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.systems-architect','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.systems-architect','planning');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.systems-architect','documentation');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.devops',NULL,'DevOps Engineer','Build, deployment, containers and CI/CD automation.','Operate build and deployment systems with reversible changes, explicit state checks, least privilege and verification after mutation.','operations','inference.technical','L2','builtin','active','{"category":"operations","recommended_tool_bundles":["core","code","operations","infrastructure"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.devops','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.devops','code');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.devops','operations');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.devops','infrastructure');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.infrastructure',NULL,'Infrastructure Engineer','Nodes, networking, storage, operating systems and runtime infrastructure.','Diagnose and operate infrastructure from observed state. Preserve service availability, validate capacity and dependencies, and prefer reversible changes.','operations','inference.technical','L2','builtin','active','{"category":"operations","recommended_tool_bundles":["core","operations","infrastructure"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.infrastructure','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.infrastructure','operations');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.infrastructure','infrastructure');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.security',NULL,'Security Analyst','Defensive configuration, threat and policy analysis.','Assess security posture defensively. Identify attack surface, trust boundaries, misconfiguration and policy gaps without expanding authority beyond the active scope.','security','inference.security','L2','builtin','active','{"category":"security","recommended_tool_bundles":["core","security","research","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.security','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.security','security');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.security','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.security','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.data-analyst',NULL,'Data Analyst','Structured data, metrics, calculations and interpretation.','Analyze data reproducibly. Validate schemas and units, distinguish descriptive from causal conclusions, quantify uncertainty and expose transformations.','analysis','inference.analysis','L2','builtin','active','{"category":"analysis","recommended_tool_bundles":["core","data","research"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.data-analyst','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.data-analyst','data');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.data-analyst','research');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.planner',NULL,'Planner','Objectives, tasks, dependencies, milestones and checkpoints.','Convert objectives into minimal actionable plans with dependencies, owners or execution roles, checkpoints, acceptance criteria and explicit blockers.','general','inference.reasoning','L1','builtin','active','{"category":"general","recommended_tool_bundles":["core","planning"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.planner','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.planner','planning');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.verifier',NULL,'Verifier','Acceptance criteria, tests, evidence and completion verification.','Verify outputs against explicit requirements using observable evidence. Do not infer completion from intent; identify unverified criteria and failed checks.','review','inference.verification','L2','builtin','active','{"category":"review","recommended_tool_bundles":["core","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.verifier','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.verifier','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.troubleshooter',NULL,'Troubleshooter','Systematic fault isolation and recovery planning.','Diagnose faults by reproducing symptoms, narrowing hypotheses with evidence, identifying the first failing boundary and preferring low-risk recovery steps.','operations','inference.technical','L2','builtin','active','{"category":"operations","recommended_tool_bundles":["core","operations","infrastructure","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.troubleshooter','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.troubleshooter','operations');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.troubleshooter','infrastructure');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.troubleshooter','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.documentation',NULL,'Documentation Writer','Technical and user documentation.','Produce accurate documentation from authoritative system context. Keep procedures reproducible, distinguish prerequisites from actions, and avoid inventing unsupported behavior.','documentation','inference.general','L1','builtin','active','{"category":"documentation","recommended_tool_bundles":["core","documentation"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.documentation','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.documentation','documentation');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.project-analyst',NULL,'Project Analyst','Requirements, risks, status, decisions and project evidence.','Analyze project state, requirements, risks, decisions and dependencies using project-scoped evidence. Surface inconsistencies and decision points clearly.','analysis','inference.analysis','L2','builtin','active','{"category":"analysis","recommended_tool_bundles":["core","planning","research","documentation"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.project-analyst','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.project-analyst','planning');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.project-analyst','research');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.project-analyst','documentation');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.operations-analyst',NULL,'Operations Analyst','System activity, incidents, capacity and workload analysis.','Interpret operational telemetry, queues, events and capacity. Correlate symptoms across services and identify actionable anomalies without bypassing policy.','operations','inference.analysis','L2','builtin','active','{"category":"operations","recommended_tool_bundles":["core","operations","data"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.operations-analyst','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.operations-analyst','operations');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.operations-analyst','data');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.model-evaluator',NULL,'Model Evaluator','Model qualification, benchmark interpretation and suitability assessment.','Evaluate model suitability using declared capabilities plus empirical tests. Separate benchmark estimates from measured results and preserve hardware/runtime context.','analysis','inference.analysis','L2','builtin','active','{"category":"analysis","recommended_tool_bundles":["core","model-evaluation","verification"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.model-evaluator','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.model-evaluator','model-evaluation');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.model-evaluator','verification');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.tool-specialist',NULL,'Tool Specialist','Precise tool-driven execution with constrained autonomy.','Prefer deterministic tool operations over speculation. Inspect results after each mutation, respect approval boundaries and stop when a required capability is unavailable.','general','inference.general','L1','builtin','active','{"category":"general","recommended_tool_bundles":["core","tool-operator"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.tool-specialist','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.tool-specialist','tool-operator');
INSERT OR IGNORE INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES('agent.creative-strategist',NULL,'Creative Strategist','Ideation, alternatives and product strategy exploration.','Generate materially different options, explain trade-offs and constraints, and separate exploratory ideas from recommendations that require evidence.','general','inference.general','L1','builtin','active','{"category":"general","recommended_tool_bundles":["core","planning","research"]}',1,NULL,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.creative-strategist','core');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.creative-strategist','planning');
INSERT OR IGNORE INTO agent_profile_tool_bundles(profile_id,bundle_id) VALUES('agent.creative-strategist','research');
INSERT OR IGNORE INTO team_presets(id,name,description,source_kind,configuration_json,status,revision,created_at,updated_at) VALUES('team.general-problem-solving','General Problem Solving','General planning, execution and verification team','builtin','{"research_mode":false,"seats":[["Planner","agent.planner"],["General Assistant","agent.general"],["Verifier","agent.verifier"]]}','active',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO team_presets(id,name,description,source_kind,configuration_json,status,revision,created_at,updated_at) VALUES('team.software-engineering','Software Engineering','Architecture, implementation, review and verification','builtin','{"research_mode":false,"seats":[["Systems Architect","agent.systems-architect"],["Software Engineer","agent.software-engineer"],["Code Reviewer","agent.code-reviewer"],["Verifier","agent.verifier"]]}','active',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO team_presets(id,name,description,source_kind,configuration_json,status,revision,created_at,updated_at) VALUES('team.research','Research','Independent evidence gathering, criticism, audit and synthesis','builtin','{"research_mode":true,"seats":[["Lead Researcher","agent.research-lead"],["Independent Researcher","agent.researcher"],["Research Critic","agent.research-critic"],["Evidence Auditor","agent.evidence-auditor"],["Research Synthesizer","agent.research-synthesizer"]],"research":{"pin_models":true,"disable_model_substitution":true,"same_model_retries":true,"preserve_failed_seats":true,"independent_first_pass":true,"scoped_evidence":true,"record_raw_outputs":true,"full_provenance":true,"require_all_seats":true,"anonymized_cross_critique":true,"synthesis_pass":true}}','active',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO team_presets(id,name,description,source_kind,configuration_json,status,revision,created_at,updated_at) VALUES('team.infrastructure','Infrastructure','Infrastructure operations, DevOps, security and troubleshooting','builtin','{"research_mode":false,"seats":[["Infrastructure Engineer","agent.infrastructure"],["DevOps Engineer","agent.devops"],["Security Analyst","agent.security"],["Troubleshooter","agent.troubleshooter"]]}','active',1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);

-- Built-in non-executable capability packages surfaced in the Skills catalogue.
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.core-reasoning','digilogic.core-reasoning','1.0.0','Core Reasoning','DigiLogic','02e9adf7a3f441339a369f79dfc9e28798df328e7c3da3b441148ea31df4c333','builtin://digilogic.core-reasoning','builtin','builtin','installed','{"id":"digilogic.core-reasoning","name":"Core Reasoning","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","planning"],"compatible_roles":["general","planner"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.research-core','digilogic.research-core','1.0.0','Research Core','DigiLogic','3ae20bdc220bb6cee64cadf2a8ae045adcaea62e8aa5b294433a709cb106691a','builtin://digilogic.research-core','builtin','builtin','installed','{"id":"digilogic.research-core","name":"Research Core","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","research","verification"],"compatible_roles":["researcher","critic","synthesizer"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.code-engineering','digilogic.code-engineering','1.0.0','Code Engineering','DigiLogic','ceea35f6c4cc3abccd381b54fd9fb57803d22ea239538fc460ee8c068fa40b88','builtin://digilogic.code-engineering','builtin','builtin','installed','{"id":"digilogic.code-engineering","name":"Code Engineering","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","code","verification"],"compatible_roles":["engineer","reviewer"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.operations-core','digilogic.operations-core','1.0.0','Operations Core','DigiLogic','7cdc9fe11608749ba79e1a7ea7fa9d4fdb42859fb1f2e9d43a832efa65a12c68','builtin://digilogic.operations-core','builtin','builtin','installed','{"id":"digilogic.operations-core","name":"Operations Core","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","operations"],"compatible_roles":["operations","troubleshooter"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.infrastructure-core','digilogic.infrastructure-core','1.0.0','Infrastructure Core','DigiLogic','335becfde8532a9d6b1a5f22f33863915841b3df1b6b490c86c976bc46a5ed50','builtin://digilogic.infrastructure-core','builtin','builtin','installed','{"id":"digilogic.infrastructure-core","name":"Infrastructure Core","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","operations","infrastructure"],"compatible_roles":["infrastructure","devops"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.security-review','digilogic.security-review','1.0.0','Security Review','DigiLogic','dade46426708cb7cdb8424eb0d0f07e8015a62a96a07cc941d6b36a94f9bf84f','builtin://digilogic.security-review','builtin','builtin','installed','{"id":"digilogic.security-review","name":"Security Review","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","security","verification"],"compatible_roles":["security","reviewer"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.documentation','digilogic.documentation','1.0.0','Documentation','DigiLogic','e27ad91e8b1d4d6cf1a42f854e65821813def69bc578cf745f3c560fcb26ff09','builtin://digilogic.documentation','builtin','builtin','installed','{"id":"digilogic.documentation","name":"Documentation","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","documentation"],"compatible_roles":["writer","synthesizer"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
INSERT OR IGNORE INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES('skillpkg-digilogic.model-evaluation','digilogic.model-evaluation','1.0.0','Model Evaluation','DigiLogic','bfed9bbaed884408be4b9db37723807c01a606099aabc4746bc17d7522f76237','builtin://digilogic.model-evaluation','builtin','builtin','installed','{"id":"digilogic.model-evaluation","name":"Model Evaluation","version":"1.0.0","publisher":"DigiLogic","type":"capability-pack","tool_bundles":["core","model-evaluation","verification"],"compatible_roles":["evaluator"]}',NULL,1,CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000);
