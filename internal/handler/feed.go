package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	claude      *service.ClaudeClient
	userRepo    *repository.UserRepo
}

func NewFeedHandler(
	feedService *service.FeedService,
	feedRepo *repository.FeedRepo,
	claude *service.ClaudeClient,
	userRepo *repository.UserRepo,
) *FeedHandler {
	return &FeedHandler{
		feedService: feedService,
		feedRepo:    feedRepo,
		claude:      claude,
		userRepo:    userRepo,
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

	limit := 100
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	jobs, err := h.feedRepo.GetUserFeed(c.Request.Context(), userID, limit)
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

// RefreshFeed triggers a feed refresh for the current user.
// The refresh runs in the background so the client gets an immediate response.
// POST /feed/refresh
func (h *FeedHandler) RefreshFeed(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	force := c.Query("force") == "true"

	// Run refresh in the background with a detached context so it isn't
	// cancelled when the HTTP response is sent back to the client.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		fetched, newJobs, err := h.feedService.RefreshUserFeed(bgCtx, userID, force)
		if err != nil {
			log.Error().Err(err).Str("userId", userID.String()).Msg("Background feed refresh failed")
			return
		}
		log.Info().
			Str("userId", userID.String()).
			Int("fetched", fetched).
			Int("new", newJobs).
			Msg("Background feed refresh complete")
	}()

	c.JSON(http.StatusOK, gin.H{
		"fetched": 0,
		"new":     0,
		"message": "Feed refresh started",
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

// CompareFeedJobs handles POST /feed/compare
// Accepts 2-4 feed job IDs, fetches them, calls Claude for structured comparison
func (h *FeedHandler) CompareFeedJobs(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		FeedJobIDs []string `json:"feedJobIds" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "feedJobIds is required"})
		return
	}

	if len(req.FeedJobIDs) < 2 || len(req.FeedJobIDs) > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Between 2 and 4 feed job IDs are required for comparison"})
		return
	}

	// Parse UUIDs
	ids := make([]uuid.UUID, 0, len(req.FeedJobIDs))
	for _, idStr := range req.FeedJobIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid feed job ID: %s", idStr)})
			return
		}
		ids = append(ids, id)
	}

	// Batch fetch feed jobs (scoped to user via user_feed join)
	feedJobs, err := h.feedRepo.GetFeedJobsByIDs(c.Request.Context(), userID, ids)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch feed jobs for comparison")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch jobs"})
		return
	}
	if len(feedJobs) != len(req.FeedJobIDs) {
		c.JSON(http.StatusNotFound, gin.H{"error": "One or more feed jobs not found"})
		return
	}

	// Reorder to match request order (SQL ANY doesn't guarantee order)
	jobMap := make(map[string]*model.FeedJob, len(feedJobs))
	for i := range feedJobs {
		jobMap[feedJobs[i].ID.String()] = &feedJobs[i]
	}
	ordered := make([]*model.FeedJob, 0, len(req.FeedJobIDs))
	for _, idStr := range req.FeedJobIDs {
		fj, ok := jobMap[idStr]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Feed job not found: %s", idStr)})
			return
		}
		ordered = append(ordered, fj)
	}

	// Fetch user profile for context
	user, err := h.userRepo.FindByID(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch user profile for feed comparison")
	}

	// Format job descriptions
	labels := []string{"Job A", "Job B", "Job C", "Job D"}
	var jobParts []string
	for i, fj := range ordered {
		jobParts = append(jobParts, formatFeedJobForComparison(labels[i], fj))
	}
	jobDescriptions := strings.Join(jobParts, "\n\n")
	profileStr := formatUserProfile(user)

	// Call Claude
	result, err := h.claude.CompareJobs(c.Request.Context(), jobDescriptions, profileStr)
	if err != nil {
		log.Error().Err(err).Msg("Failed to compare feed jobs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI comparison failed. Please try again."})
		return
	}

	c.JSON(http.StatusOK, result)
}

// formatFeedJobForComparison formats a FeedJob for Claude comparison,
// mirroring formatJobForComparison but using FeedJob fields.
func formatFeedJobForComparison(label string, fj *model.FeedJob) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("=== %s ===", label))
	parts = append(parts, fmt.Sprintf("Title: %s", fj.Title))
	parts = append(parts, fmt.Sprintf("Company: %s", fj.Company))

	if fj.Location != "" {
		parts = append(parts, fmt.Sprintf("Location: %s", fj.Location))
	}

	salaryStr := fj.SalaryText
	if salaryStr == "" && fj.SalaryMin > 0 {
		salaryStr = fmt.Sprintf("$%dk - $%dk", fj.SalaryMin/1000, fj.SalaryMax/1000)
	}
	if salaryStr != "" {
		parts = append(parts, fmt.Sprintf("Salary: %s", salaryStr))
	}

	if fj.JobType != "" {
		parts = append(parts, fmt.Sprintf("Type: %s", fj.JobType))
	}
	if len(fj.RequiredSkills) > 0 {
		parts = append(parts, fmt.Sprintf("Required Skills: %s", strings.Join(fj.RequiredSkills, ", ")))
	}

	if fj.Description != "" {
		desc := fj.Description
		if len(desc) > 2000 {
			desc = desc[:2000] + "..."
		}
		parts = append(parts, fmt.Sprintf("Description: %s", desc))
	}

	return strings.Join(parts, "\n")
}
