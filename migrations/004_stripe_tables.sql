-- 004_stripe_tables.sql
-- Stripe billing integration: customers, subscriptions, payment events

-- ============================================================
-- STRIPE CUSTOMERS — links HireIQ users to Stripe customer IDs
-- ============================================================
CREATE TABLE stripe_customers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stripe_customer_id  TEXT UNIQUE NOT NULL,
    email               TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stripe_customers_stripe_id ON stripe_customers(stripe_customer_id);

-- ============================================================
-- SUBSCRIPTIONS — tracks active subscription state per user
-- ============================================================
CREATE TABLE subscriptions (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stripe_sub_id        TEXT UNIQUE NOT NULL,
    stripe_price_id      TEXT NOT NULL,
    plan                 TEXT NOT NULL DEFAULT 'free',
    status               TEXT NOT NULL DEFAULT 'active',
    current_period_end   TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_subscriptions_status ON subscriptions(status);

-- ============================================================
-- PAYMENT EVENTS — webhook audit trail
-- ============================================================
CREATE TABLE payment_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stripe_event_id    TEXT UNIQUE NOT NULL,
    event_type         TEXT NOT NULL,
    stripe_customer_id TEXT,
    data               JSONB NOT NULL DEFAULT '{}'::jsonb,
    processed          BOOLEAN NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_payment_events_type ON payment_events(event_type);
CREATE INDEX idx_payment_events_created ON payment_events(created_at DESC);
