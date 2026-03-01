package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

// RequirePlan returns middleware that checks whether the user's subscription
// meets the minimum plan level. Returns 402 if the user's plan is insufficient.
//
// Plan hierarchy: free (0) < pro (1) < pro_plus (2)
func RequirePlan(minPlan string, subRepo *repository.SubscriptionRepo) gin.HandlerFunc {
	minLevel := model.PlanLevel(minPlan)

	return func(c *gin.Context) {
		userIDStr := GetUserID(c)
		if userIDStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
			return
		}

		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
			return
		}

		sub, err := subRepo.FindByUserID(c.Request.Context(), userID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to check subscription")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to check subscription"})
			return
		}

		// Determine user's current plan
		userPlan := model.PlanFree
		if sub != nil && (sub.Status == model.SubStatusActive || sub.Status == model.SubStatusTrialing) {
			userPlan = sub.Plan
		}

		if model.PlanLevel(userPlan) < minLevel {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":        "upgrade_required",
				"requiredPlan": minPlan,
				"currentPlan":  userPlan,
			})
			return
		}

		c.Next()
	}
}
