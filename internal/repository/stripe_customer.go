package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type StripeCustomerRepo struct {
	pool *pgxpool.Pool
}

func NewStripeCustomerRepo(pool *pgxpool.Pool) *StripeCustomerRepo {
	return &StripeCustomerRepo{pool: pool}
}

// FindByUserID returns the Stripe customer linked to a HireIQ user
func (r *StripeCustomerRepo) FindByUserID(ctx context.Context, userID uuid.UUID) (*model.StripeCustomer, error) {
	var sc model.StripeCustomer
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_customer_id, email, created_at, updated_at
		FROM stripe_customers
		WHERE user_id = $1
	`, userID).Scan(
		&sc.ID, &sc.UserID, &sc.StripeCustomerID, &sc.Email,
		&sc.CreatedAt, &sc.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding stripe customer by user: %w", err)
	}
	return &sc, nil
}

// FindByStripeID returns the Stripe customer by Stripe's customer ID
func (r *StripeCustomerRepo) FindByStripeID(ctx context.Context, stripeCustomerID string) (*model.StripeCustomer, error) {
	var sc model.StripeCustomer
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_customer_id, email, created_at, updated_at
		FROM stripe_customers
		WHERE stripe_customer_id = $1
	`, stripeCustomerID).Scan(
		&sc.ID, &sc.UserID, &sc.StripeCustomerID, &sc.Email,
		&sc.CreatedAt, &sc.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding stripe customer by stripe id: %w", err)
	}
	return &sc, nil
}

// Upsert creates or updates a Stripe customer record
func (r *StripeCustomerRepo) Upsert(ctx context.Context, userID uuid.UUID, stripeCustomerID, email string) (*model.StripeCustomer, error) {
	var sc model.StripeCustomer
	err := r.pool.QueryRow(ctx, `
		INSERT INTO stripe_customers (user_id, stripe_customer_id, email)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		SET stripe_customer_id = $2, email = $3, updated_at = now()
		RETURNING id, user_id, stripe_customer_id, email, created_at, updated_at
	`, userID, stripeCustomerID, email).Scan(
		&sc.ID, &sc.UserID, &sc.StripeCustomerID, &sc.Email,
		&sc.CreatedAt, &sc.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upserting stripe customer: %w", err)
	}
	return &sc, nil
}
