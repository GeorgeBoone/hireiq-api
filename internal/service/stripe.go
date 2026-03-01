package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stripe/stripe-go/v81"
	billingportalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v81/checkout/session"
	stripecustomer "github.com/stripe/stripe-go/v81/customer"
	stripesub "github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/webhook"
	"github.com/yourusername/hireiq-api/internal/config"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

// StripeService handles all Stripe API interactions
type StripeService struct {
	cfg      *config.Config
	custRepo *repository.StripeCustomerRepo
	subRepo  *repository.SubscriptionRepo
	userRepo *repository.UserRepo
}

func NewStripeService(
	cfg *config.Config,
	custRepo *repository.StripeCustomerRepo,
	subRepo *repository.SubscriptionRepo,
	userRepo *repository.UserRepo,
) *StripeService {
	stripe.Key = cfg.StripeSecretKey
	return &StripeService{
		cfg:      cfg,
		custRepo: custRepo,
		subRepo:  subRepo,
		userRepo: userRepo,
	}
}

// GetOrCreateCustomer ensures a Stripe customer exists for the given user
func (s *StripeService) GetOrCreateCustomer(ctx context.Context, userID uuid.UUID) (*model.StripeCustomer, error) {
	// Check if we already have a record
	existing, err := s.custRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("looking up stripe customer: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Fetch user to get email/name
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("finding user for stripe customer: %w", err)
	}

	// Create Stripe customer
	params := &stripe.CustomerParams{
		Email: stripe.String(user.Email),
		Name:  stripe.String(user.Name),
	}
	params.AddMetadata("hireiq_user_id", userID.String())

	cust, err := stripecustomer.New(params)
	if err != nil {
		return nil, fmt.Errorf("creating stripe customer: %w", err)
	}

	// Save mapping
	sc, err := s.custRepo.Upsert(ctx, userID, cust.ID, user.Email)
	if err != nil {
		return nil, fmt.Errorf("saving stripe customer: %w", err)
	}

	log.Info().Str("userId", userID.String()).Str("stripeId", cust.ID).Msg("Stripe customer created")
	return sc, nil
}

// ResolvePriceID maps plan + interval to a Stripe Price ID from config
func (s *StripeService) ResolvePriceID(plan, interval string) (string, error) {
	switch {
	case plan == model.PlanPro && interval == "month":
		return s.cfg.StripePriceProMo, nil
	case plan == model.PlanPro && interval == "year":
		return s.cfg.StripePriceProAn, nil
	case plan == model.PlanProPlus && interval == "month":
		return s.cfg.StripePriceProPlusMo, nil
	case plan == model.PlanProPlus && interval == "year":
		return s.cfg.StripePriceProPlusAn, nil
	default:
		return "", fmt.Errorf("unknown plan/interval: %s/%s", plan, interval)
	}
}

// CreateCheckoutSession builds a Stripe Checkout Session and returns the URL
func (s *StripeService) CreateCheckoutSession(ctx context.Context, userID uuid.UUID, plan, interval string) (string, error) {
	// Resolve price ID
	priceID, err := s.ResolvePriceID(plan, interval)
	if err != nil {
		return "", err
	}
	if priceID == "" {
		return "", fmt.Errorf("stripe price not configured for %s/%s", plan, interval)
	}

	// Ensure Stripe customer exists
	sc, err := s.GetOrCreateCustomer(ctx, userID)
	if err != nil {
		return "", err
	}

	// Build checkout session
	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(sc.StripeCustomerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(s.cfg.FrontendURL + "?checkout=success"),
		CancelURL:  stripe.String(s.cfg.FrontendURL + "?checkout=cancel"),
	}
	params.AddMetadata("hireiq_user_id", userID.String())
	params.AddMetadata("plan", plan)
	params.AddMetadata("interval", interval)

	sess, err := checkoutsession.New(params)
	if err != nil {
		return "", fmt.Errorf("creating checkout session: %w", err)
	}

	log.Info().
		Str("userId", userID.String()).
		Str("plan", plan).
		Str("interval", interval).
		Msg("Checkout session created")

	return sess.URL, nil
}

// CreatePortalSession builds a Stripe Billing Portal session and returns the URL
func (s *StripeService) CreatePortalSession(ctx context.Context, userID uuid.UUID) (string, error) {
	sc, err := s.custRepo.FindByUserID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("looking up stripe customer: %w", err)
	}
	if sc == nil {
		return "", fmt.Errorf("no stripe customer found for user")
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sc.StripeCustomerID),
		ReturnURL: stripe.String(s.cfg.FrontendURL),
	}

	sess, err := billingportalsession.New(params)
	if err != nil {
		return "", fmt.Errorf("creating portal session: %w", err)
	}

	return sess.URL, nil
}

// VerifyWebhook verifies the Stripe webhook signature and returns the event
func (s *StripeService) VerifyWebhook(body io.Reader, signature string) (*stripe.Event, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading webhook body: %w", err)
	}

	log.Debug().
		Int("payloadLen", len(payload)).
		Str("signaturePrefix", truncate(signature, 20)).
		Str("secretPrefix", truncate(s.cfg.StripeWebhookSecret, 10)).
		Msg("Webhook verification attempt")

	// In development, allow skipping signature verification if secret is empty or verification fails
	event, err := webhook.ConstructEvent(payload, signature, s.cfg.StripeWebhookSecret)
	if err != nil {
		if s.cfg.Env == "development" {
			log.Warn().Err(err).Msg("Webhook signature failed — falling back to raw parse (dev mode)")
			var fallbackEvent stripe.Event
			if jsonErr := json.Unmarshal(payload, &fallbackEvent); jsonErr != nil {
				return nil, fmt.Errorf("verifying webhook signature: %w (raw parse also failed: %v)", err, jsonErr)
			}
			return &fallbackEvent, nil
		}
		return nil, fmt.Errorf("verifying webhook signature: %w", err)
	}

	return &event, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// HandleWebhookEvent processes a Stripe webhook event
func (s *StripeService) HandleWebhookEvent(ctx context.Context, event *stripe.Event) error {
	log.Info().
		Str("type", string(event.Type)).
		Str("id", event.ID).
		Msg("Processing Stripe webhook")

	switch event.Type {
	case "checkout.session.completed":
		return s.handleCheckoutCompleted(ctx, event)
	case "customer.subscription.created", "customer.subscription.updated":
		return s.handleSubscriptionUpsert(ctx, event)
	case "customer.subscription.deleted":
		return s.handleSubscriptionDeleted(ctx, event)
	case "invoice.payment_failed":
		return s.handlePaymentFailed(ctx, event)
	default:
		log.Debug().Str("type", string(event.Type)).Msg("Ignoring unhandled webhook type")
		return nil
	}
}

func (s *StripeService) handleCheckoutCompleted(ctx context.Context, event *stripe.Event) error {
	var session struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
		Mode         string `json:"mode"`
	}
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return fmt.Errorf("unmarshaling checkout session: %w", err)
	}

	// Only handle subscription checkouts
	if session.Mode != "subscription" || session.Subscription == "" {
		log.Debug().Str("mode", session.Mode).Msg("Ignoring non-subscription checkout")
		return nil
	}

	// Fetch the full subscription from Stripe API
	sub, err := stripesub.Get(session.Subscription, nil)
	if err != nil {
		return fmt.Errorf("fetching subscription from Stripe: %w", err)
	}

	// Find user by Stripe customer ID
	custRecord, err := s.custRepo.FindByStripeID(ctx, session.Customer)
	if err != nil {
		return fmt.Errorf("looking up customer: %w", err)
	}
	if custRecord == nil {
		log.Warn().Str("stripeCustomer", session.Customer).Msg("Checkout for unknown customer")
		return nil
	}

	// Determine plan from price ID
	priceID := sub.Items.Data[0].Price.ID
	plan := s.planFromPriceID(priceID)

	var periodEnd *time.Time
	if sub.CurrentPeriodEnd != 0 {
		t := time.Unix(sub.CurrentPeriodEnd, 0)
		periodEnd = &t
	}

	_, err = s.subRepo.Upsert(ctx, &model.Subscription{
		UserID:            custRecord.UserID,
		StripeSubID:       sub.ID,
		StripePriceID:     priceID,
		Plan:              plan,
		Status:            string(sub.Status),
		CurrentPeriodEnd:  periodEnd,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
	})
	if err != nil {
		return fmt.Errorf("upserting subscription from checkout: %w", err)
	}

	log.Info().
		Str("userId", custRecord.UserID.String()).
		Str("plan", plan).
		Str("status", string(sub.Status)).
		Msg("Subscription created via checkout.session.completed")

	return nil
}

func (s *StripeService) handleSubscriptionUpsert(ctx context.Context, event *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshaling subscription event: %w", err)
	}

	// Find user by Stripe customer ID
	custRecord, err := s.custRepo.FindByStripeID(ctx, sub.Customer.ID)
	if err != nil {
		return fmt.Errorf("looking up customer: %w", err)
	}
	if custRecord == nil {
		log.Warn().Str("stripeCustomer", sub.Customer.ID).Msg("Webhook for unknown customer")
		return nil
	}

	// Determine plan from price ID
	plan := s.planFromPriceID(sub.Items.Data[0].Price.ID)

	var periodEnd *time.Time
	if sub.CurrentPeriodEnd != 0 {
		t := time.Unix(sub.CurrentPeriodEnd, 0)
		periodEnd = &t
	}

	_, err = s.subRepo.Upsert(ctx, &model.Subscription{
		UserID:            custRecord.UserID,
		StripeSubID:       sub.ID,
		StripePriceID:     sub.Items.Data[0].Price.ID,
		Plan:              plan,
		Status:            string(sub.Status),
		CurrentPeriodEnd:  periodEnd,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
	})
	if err != nil {
		return fmt.Errorf("upserting subscription: %w", err)
	}

	log.Info().
		Str("userId", custRecord.UserID.String()).
		Str("plan", plan).
		Str("status", string(sub.Status)).
		Msg("Subscription updated via webhook")

	return nil
}

func (s *StripeService) handleSubscriptionDeleted(ctx context.Context, event *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshaling subscription deleted event: %w", err)
	}

	err := s.subRepo.UpdateStatus(ctx, sub.ID, model.SubStatusCanceled, false)
	if err != nil {
		return fmt.Errorf("canceling subscription: %w", err)
	}

	log.Info().Str("stripeSubId", sub.ID).Msg("Subscription canceled via webhook")
	return nil
}

func (s *StripeService) handlePaymentFailed(ctx context.Context, event *stripe.Event) error {
	var invoice struct {
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("unmarshaling invoice event: %w", err)
	}

	if invoice.Subscription == "" {
		return nil // one-time payment, not relevant
	}

	err := s.subRepo.UpdateStatus(ctx, invoice.Subscription, model.SubStatusPastDue, false)
	if err != nil {
		return fmt.Errorf("marking subscription past due: %w", err)
	}

	log.Warn().Str("stripeSubId", invoice.Subscription).Msg("Payment failed — subscription marked past_due")
	return nil
}

// planFromPriceID maps a Stripe Price ID back to a plan name
func (s *StripeService) planFromPriceID(priceID string) string {
	switch priceID {
	case s.cfg.StripePriceProMo, s.cfg.StripePriceProAn:
		return model.PlanPro
	case s.cfg.StripePriceProPlusMo, s.cfg.StripePriceProPlusAn:
		return model.PlanProPlus
	default:
		return model.PlanFree
	}
}
