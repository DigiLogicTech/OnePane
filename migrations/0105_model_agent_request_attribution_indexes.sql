-- Reviewed ordinary AgentRequest encoding: the second message is a user message
-- containing a JSON STRING whose object declares protocol_version=v1. These are
-- attribution candidates, not validation of a complete AgentRequest or proof of
-- absent effects. Unsupported envelopes remain unresolved by recovery.
-- Nested CASE is intentional: SQL AND does not guarantee short-circuit order.
-- Existing valid outer requests may contain arbitrary non-JSON user text.
CREATE INDEX idx_inference_agent_request_task ON inference_requests(
  CASE WHEN json_extract(request_json,'$.messages[1].role')='user'
    AND json_type(request_json,'$.messages[1].content')='text'
  THEN CASE WHEN json_valid(json_extract(request_json,'$.messages[1].content'))
    THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'))='object'
      AND json_extract(json_extract(request_json,'$.messages[1].content'),'$.protocol_version')='v1'
    THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'),'$.task_id')='text'
      THEN json_extract(json_extract(request_json,'$.messages[1].content'),'$.task_id')
    END END END END
);
CREATE INDEX idx_inference_agent_request_attempt ON inference_requests(
  CASE WHEN json_extract(request_json,'$.messages[1].role')='user'
    AND json_type(request_json,'$.messages[1].content')='text'
  THEN CASE WHEN json_valid(json_extract(request_json,'$.messages[1].content'))
    THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'))='object'
      AND json_extract(json_extract(request_json,'$.messages[1].content'),'$.protocol_version')='v1'
    THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'),'$.attempt_id')='text'
      THEN json_extract(json_extract(request_json,'$.messages[1].content'),'$.attempt_id')
    END END END END
);
