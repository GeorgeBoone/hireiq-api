package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
	"github.com/yourusername/hireiq-api/internal/service"
)

type BillingHandler struct {
	stripeService *service.StripeService
	subRepo       *repository.SubscriptionRepo
}

func NewBillingHandler(stripeService *service.StripeService, subRepo *repository.SubscriptionRepo) *BillingHandler {
	return &BillingHandler{
		stripeService: stripeService,
		subRepo:       subRepo,
	}
}

// GetSubscription handles GET /billing/subscription
// Returns the user's current subscription or a default free plan
func (h *BillingHandler) GetSubscription(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	sub, err := h.subRepo.FindByUserID(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get subscription")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription"})
		return
	}

	// If no subscription, return default free plan
	if sub == nil {
		c.JSON(http.StatusOK, gin.H{
			"plan":   model.PlanFree,
			"status": model.SubStatusActive,
		})
		return
	}

	c.JSON(http.StatusOK, sub)
}

// CreateCheckout handles POST /billing/checkout
// Accepts {plan, interval} and returns {url} for Stripe Checkout redirect
func (h *BillingHandler) CreateCheckout(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		Plan     string `json:"plan" binding:"required"`
		Interval string `json:"interval" binding:"required"` // "month" or "year"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan and interval are required"})
		return
	}

	// Validate plan
	if req.Plan != model.PlanPro && req.Plan != model.PlanProPlus {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid plan. Must be 'pro' or 'pro_plus'"})
		return
	}

	// Validate interval
	if req.Interval != "month" && req.Interval != "year" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid interval. Must be 'month' or 'year'"})
		return
	}

	url, err := h.stripeService.CreateCheckoutSession(c.Request.Context(), userID, req.Plan, req.Interval)
	if err != nil {
		log.Error().Err(err).Str("plan", req.Plan).Msg("Failed to create checkout session")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create checkout session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}

// CreatePortal handles POST /billing/portal
// Returns {url} for Stripe Billing Portal redirect
func (h *BillingHandler) CreatePortal(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	url, err := h.stripeService.CreatePortalSession(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create portal session")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create portal session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}

// HandleWebhook handles POST /billing/webhook
// Unauthenticated — uses Stripe signature verification instead
func (h *BillingHandler) HandleWebhook(c *gin.Context) {
	event, err := h.stripeService.VerifyWebhook(c.Request.Body, c.GetHeader("Stripe-Signature"))
	if err != nil {
		log.Warn().Err(err).Msg("Invalid webhook signature")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid signature"})
		return
	}

	if err := h.stripeService.HandleWebhookEvent(c.Request.Context(), event); err != nil {
		log.Error().Err(err).Str("type", string(event.Type)).Msg("Failed to process webhook event")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process event"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}
