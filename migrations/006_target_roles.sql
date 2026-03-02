-- 006: Add target_roles column for explicit job role targeting
-- Run with: psql $DATABASE_URL -f migrations/006_target_roles.sql

ALTER TABLE users
    ADD COLUMN target_roles TEXT[] NOT NULL DEFAULT '{}';
