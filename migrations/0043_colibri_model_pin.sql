-- RC11: an explicit local Colibri pin occupies one resident Colibri slot
-- per node. Native runtime only; unrelated deployments and model data untouched.
-- A unique partial index prevents races between concurrent pin requests.
CREATE UNIQUE INDEX IF NOT EXISTS idx_rc11_one_colibri_pin_per_node
 ON model_deployments(node_id)
 WHERE node_id IS NOT NULL AND LOWER(COALESCE(runtime_name,''))='colibri'
 AND json_extract(runtime_config_json,'$.colibri_pinned')=1;
