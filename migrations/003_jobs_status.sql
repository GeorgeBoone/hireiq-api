-- Add status column to jobs table for Kanban board
-- Valid values: saved, applied, screening, interview, offer, rejected
-- Default: 'saved' (when user first adds a job to tracker)

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'saved';

-- Index for filtering by status (Kanban columns)
CREATE INDEX IF NOT EXISTS idx_jobs_user_status ON jobs(user_id, status);
