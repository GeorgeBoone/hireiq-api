-- Feed jobs: cached listings from job APIs, shared across users
-- Deduplication key is external_id + source
CREATE TABLE IF NOT EXISTS feed_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id     TEXT NOT NULL,           -- JSearch job_id or other API ID
    source          TEXT NOT NULL,            -- jsearch, remotive, themuse
    title           TEXT NOT NULL,
    company         TEXT NOT NULL,
    location        TEXT,
    salary_min      INTEGER,
    salary_max      INTEGER,
    salary_text     TEXT,                     -- Raw salary string from API
    job_type        TEXT,                     -- full-time, part-time, contract
    description     TEXT,
    required_skills TEXT[] DEFAULT '{}',
    apply_url       TEXT,
    company_logo    TEXT,
    posted_at       TIMESTAMPTZ,             -- When the job was posted
    fetched_at      TIMESTAMPTZ DEFAULT now(),-- When we fetched it
    expires_at      TIMESTAMPTZ,             -- Auto-cleanup after this date
    raw_data        JSONB,                   -- Full API response for future use
    UNIQUE(external_id, source)
);

-- Per-user feed: links users to feed jobs with personalized match scores
CREATE TABLE IF NOT EXISTS user_feed (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    feed_job_id     UUID REFERENCES feed_jobs(id) ON DELETE CASCADE,
    match_score     INTEGER DEFAULT 0,        -- AI-computed match 0-100
    dismissed       BOOLEAN DEFAULT false,    -- User dismissed this listing
    saved           BOOLEAN DEFAULT false,    -- User saved to their CRM
    saved_job_id    UUID REFERENCES jobs(id), -- Link to CRM job if saved
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, feed_job_id)
);

-- Tracks when each user's feed was last refreshed
CREATE TABLE IF NOT EXISTS feed_refresh_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    query_used      TEXT,                     -- The search query we sent
    jobs_fetched    INTEGER DEFAULT 0,
    jobs_new        INTEGER DEFAULT 0,        -- How many were new (not dupes)
    refreshed_at    TIMESTAMPTZ DEFAULT now()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_feed_jobs_source ON feed_jobs(source, external_id);
CREATE INDEX IF NOT EXISTS idx_feed_jobs_expires ON feed_jobs(expires_at);
CREATE INDEX IF NOT EXISTS idx_user_feed_user ON user_feed(user_id, dismissed, match_score DESC);
CREATE INDEX IF NOT EXISTS idx_user_feed_job ON user_feed(feed_job_id);
CREATE INDEX IF NOT EXISTS idx_feed_refresh_user ON feed_refresh_log(user_id, refreshed_at DESC);
