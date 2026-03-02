-- 005: Add rich profile sections (JSONB arrays)
-- Run with: psql $DATABASE_URL -f migrations/005_profile_sections.sql

ALTER TABLE users
    ADD COLUMN experience     JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN education      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN certifications JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN languages      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN volunteer      JSONB NOT NULL DEFAULT '[]';
