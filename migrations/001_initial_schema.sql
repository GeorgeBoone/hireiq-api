-- HireIQ Initial Schema
-- Run with: psql $DATABASE_URL -f migrations/001_initial_schema.sql

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- USERS
-- ============================================================
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    firebase_uid    TEXT UNIQUE NOT NULL,
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    bio             TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    work_style      TEXT NOT NULL DEFAULT '',
    salary_min      INTEGER NOT NULL DEFAULT 0,
    salary_max      INTEGER NOT NULL DEFAULT 0,
    skills          TEXT[] NOT NULL DEFAULT '{}',
    github_url      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_firebase ON users(firebase_uid);

-- ============================================================
-- JOBS
-- ============================================================
CREATE TABLE jobs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    external_id      TEXT NOT NULL DEFAULT '',
    source           TEXT NOT NULL DEFAULT 'manual',
    title            TEXT NOT NULL,
    company          TEXT NOT NULL,
    location         TEXT NOT NULL DEFAULT '',
    salary_range     TEXT NOT NULL DEFAULT '',
    job_type         TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    tags             TEXT[] NOT NULL DEFAULT '{}',
    required_skills  TEXT[] NOT NULL DEFAULT '{}',
    preferred_skills TEXT[] NOT NULL DEFAULT '{}',
    apply_url        TEXT NOT NULL DEFAULT '',
    hiring_email     TEXT NOT NULL DEFAULT '',
    company_logo     TEXT NOT NULL DEFAULT '',
    company_color    TEXT NOT NULL DEFAULT '#4f46e5',
    match_score      INTEGER NOT NULL DEFAULT 0,
    bookmarked       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_jobs_user ON jobs(user_id);
CREATE INDEX idx_jobs_user_bookmarked ON jobs(user_id, bookmarked) WHERE bookmarked = true;
CREATE INDEX idx_jobs_user_match ON jobs(user_id, match_score DESC);

-- ============================================================
-- APPLICATIONS
-- ============================================================
CREATE TABLE applications (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id           UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    status           TEXT NOT NULL DEFAULT 'saved',
    applied_at       TIMESTAMPTZ,
    next_step        TEXT NOT NULL DEFAULT '',
    follow_up_date   TIMESTAMPTZ,
    follow_up_type   TEXT NOT NULL DEFAULT '',
    follow_up_urgent BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, job_id)
);

CREATE INDEX idx_apps_user ON applications(user_id);
CREATE INDEX idx_apps_user_status ON applications(user_id, status);
CREATE INDEX idx_apps_followup ON applications(user_id, follow_up_date)
    WHERE follow_up_date IS NOT NULL;

-- ============================================================
-- STATUS HISTORY (for application timeline)
-- ============================================================
CREATE TABLE status_history (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id   UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    from_status      TEXT NOT NULL DEFAULT '',
    to_status        TEXT NOT NULL,
    changed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    note             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_history_app ON status_history(application_id, changed_at);

-- ============================================================
-- NOTES
-- ============================================================
CREATE TABLE notes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notes_job ON notes(job_id, created_at DESC);
CREATE INDEX idx_notes_user ON notes(user_id, created_at DESC);

-- ============================================================
-- CONTACTS
-- ============================================================
CREATE TABLE contacts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    company       TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT '',
    connection    TEXT NOT NULL DEFAULT '3rd',
    phone         TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    tip           TEXT NOT NULL DEFAULT '',
    enriched      BOOLEAN NOT NULL DEFAULT false,
    enriched_data JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contacts_user ON contacts(user_id);
CREATE INDEX idx_contacts_user_company ON contacts(user_id, company);

-- ============================================================
-- RESUMES
-- ============================================================
CREATE TABLE resumes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL DEFAULT '',
    raw_text        TEXT NOT NULL DEFAULT '',
    file_url        TEXT NOT NULL DEFAULT '',
    critique_result JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_resumes_user ON resumes(user_id, created_at DESC);

-- ============================================================
-- COMPETITIVE POSITIONING (aggregated snapshots)
-- ============================================================
CREATE TABLE competitive_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    snapshot_date   DATE NOT NULL,
    applicant_count INTEGER NOT NULL DEFAULT 0,
    avg_match_score INTEGER NOT NULL DEFAULT 0,
    trend           TEXT NOT NULL DEFAULT 'steady',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_competitive_job ON competitive_snapshots(job_id, snapshot_date DESC);
