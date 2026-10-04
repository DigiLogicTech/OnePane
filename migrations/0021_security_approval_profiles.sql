-- OnePane graduated security approval profiles.
-- High = prompt for every approval-class operation.
-- Medium = auto-approve low-risk authorized operations (recommended/default).
-- Low = auto-approve low + medium risk authorized operations.
-- YOLO remains represented by approval_mode=auto_authorized and supersedes this
-- profile for the current session only. Hard policy denials remain denials.

ALTER TABLE chat_session_controls
    ADD COLUMN approval_level TEXT NOT NULL DEFAULT 'medium'
    CHECK (approval_level IN ('high','medium','low'));
