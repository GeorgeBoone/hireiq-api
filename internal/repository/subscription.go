package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type SubscriptionRepo struct {
	pool *pgxpool.Pool
}

func NewSubscriptionRepo(pool *pgxpool.Pool) *SubscriptionRepo {
	return &SubscriptionRepo{pool: pool}
}

// FindByUserID returns the subscription for a user
func (r *SubscriptionRepo) FindByUserID(ctx context.Context, userID uuid.UUID) (*model.Subscription, error) {
	var s model.Subscription
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_sub_id, stripe_price_id, plan, status,
		       current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions
		WHERE user_id = $1
	`, userID).Scan(
		&s.ID, &s.UserID, &s.StripeSubID, &s.StripePriceID,
		&s.Plan, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding subscription by user: %w", err)
	}
	return &s, nil
}

// FindByStripeSubID returns the subscription by Stripe's subscription ID
func (r *SubscriptionRepo) FindByStripeSubID(ctx context.Context, stripeSubID string) (*model.Subscription, error) {
	var s model.Subscription
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_sub_id, stripe_price_id, plan, status,
		       current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions
		WHERE stripe_sub_id = $1
	`, stripeSubID).Scan(
		&s.ID, &s.UserID, &s.StripeSubID, &s.StripePriceID,
		&s.Plan, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding subscription by stripe id: %w", err)
	}
	return &s, nil
}

// Upsert creates or updates a subscription record (keyed on user_id)
func (r *SubscriptionRepo) Upsert(ctx context.Context, sub *model.Subscription) (*model.Subscription, error) {
	var s model.Subscription
	err := r.pool.QueryRow(ctx, `
		INSERT INTO subscriptions (user_id, stripe_sub_id, stripe_price_id, plan, status, current_period_end, cancel_at_period_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id) DO UPDATE
		SET stripe_sub_id = $2, stripe_price_id = $3, plan = $4, status = $5,
		    current_period_end = $6, cancel_at_period_end = $7, updated_at = now()
		RETURNING id, user_id, stripe_sub_id, stripe_price_id, plan, status,
		          current_period_end, cancel_at_period_end, created_at, updated_at
	`, sub.UserID, sub.StripeSubID, sub.StripePriceID, sub.Plan, sub.Status,
		sub.CurrentPeriodEnd, sub.CancelAtPeriodEnd,
	).Scan(
		&s.ID, &s.UserID, &s.StripeSubID, &s.StripePriceID,
		&s.Plan, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upserting subscription: %w", err)
	}
	return &s, nil
}

// UpdateStatus updates only the status and cancel_at_period_end fields
func (r *SubscriptionRepo) UpdateStatus(ctx context.Context, stripeSubID, status string, cancelAtPeriodEnd bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE subscriptions
		SET status = $2, cancel_at_period_end = $3, updated_at = now()
		WHERE stripe_sub_id = $1
	`, stripeSubID, status, cancelAtPeriodEnd)
	if err != nil {
		return fmt.Errorf("updating subscription status: %w", err)
	}
	return nil
}
