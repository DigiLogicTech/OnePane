-- Bound the latest completion-event lookup independently of unrelated Task
-- history and the number of historical completions/revisions.
CREATE INDEX idx_events_task_latest_completion
ON events(aggregate_type,aggregate_id,event_type,sequence DESC);
