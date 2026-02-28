package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
	"github.com/yourusername/hireiq-api/internal/service"
)

type FeedHandler struct {
	feedService *service.FeedService
	feedRepo    *repository.FeedRepo
}

func NewFeedHandler(feedService *service.FeedService, feedRepo *repository.FeedRepo) *FeedHandler {
	return &FeedHandler{
		feedService: feedService,
		feedRepo:    feedRepo,
	}
}

// GetFeed returns the user's job feed, sorted by match score
// GET /feed
func (h *FeedHandler) GetFeed(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	jobs, err := h.feedRepo.GetUserFeed(c.Request.Context(), userID, 30)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get user feed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get feed"})
		return
	}

	if jobs == nil {
		jobs = []model.FeedJob{}
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// RefreshFeed triggers a feed refresh for the current user
// POST /feed/refresh
func (h *FeedHandler) RefreshFeed(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	fetched, newJobs, err := h.feedService.RefreshUserFeed(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to refresh feed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh feed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"fetched": fetched,
		"new":     newJobs,
		"message": "Feed refreshed",
	})
}

// DismissFeedJob hides a feed job from the user's feed
// POST /feed/:id/dismiss
func (h *FeedHandler) DismissFeedJob(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	feedJobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	if err := h.feedRepo.DismissFeedJob(c.Request.Context(), userID, feedJobID); err != nil {
		log.Error().Err(err).Msg("Failed to dismiss feed job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to dismiss"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Job dismissed"})
}

// SaveFeedJob copies a feed job to the user's CRM
// POST /feed/:id/save
func (h *FeedHandler) SaveFeedJob(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	feedJobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	job, err := h.feedRepo.SaveFeedJobToCRM(c.Request.Context(), userID, feedJobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save feed job to CRM")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save job"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Job saved to your tracker",
		"job":     job,
	})
}
