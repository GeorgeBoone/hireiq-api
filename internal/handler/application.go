package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

type ApplicationHandler struct {
	appRepo *repository.ApplicationRepo
	jobRepo *repository.JobRepo
}

func NewApplicationHandler(appRepo *repository.ApplicationRepo, jobRepo *repository.JobRepo) *ApplicationHandler {
	return &ApplicationHandler{appRepo: appRepo, jobRepo: jobRepo}
}

// Get returns the application for a specific job
// GET /jobs/:id/application
func (h *ApplicationHandler) Get(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	app, err := h.appRepo.FindByJobID(c.Request.Context(), userID, jobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get application")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get application"})
		return
	}

	// Return null (not 404) so frontend can distinguish "no application yet" from errors
	if app == nil {
		c.JSON(http.StatusOK, nil)
		return
	}

	c.JSON(http.StatusOK, app)
}

// Create creates a new application for a job
// POST /jobs/:id/application
func (h *ApplicationHandler) Create(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	var req struct {
		Status       string  `json:"status"`
		AppliedAt    *string `json:"appliedAt"`
		NextStep     string  `json:"nextStep"`
		FollowUpDate *string `json:"followUpDate"`
		FollowUpType string  `json:"followUpType"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Default status to "applied" if not specified
	status := req.Status
	if status == "" {
		status = model.StatusApplied
	}
	if !model.ValidStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
		return
	}

	// Parse optional time fields
	var appliedAt *time.Time
	if req.AppliedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.AppliedAt)
		if err == nil {
			appliedAt = &t
		}
	}
	var followUpDate *time.Time
	if req.FollowUpDate != nil {
		t, err := time.Parse(time.RFC3339, *req.FollowUpDate)
		if err == nil {
			followUpDate = &t
		}
	}

	app := &model.Application{
		UserID:       userID,
		JobID:        jobID,
		Status:       status,
		AppliedAt:    appliedAt,
		NextStep:     req.NextStep,
		FollowUpDate: followUpDate,
		FollowUpType: req.FollowUpType,
	}

	created, err := h.appRepo.Create(c.Request.Context(), app)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create application")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create application"})
		return
	}

	// Sync jobs.status to keep Kanban board consistent
	if syncErr := h.jobRepo.UpdateStatus(c.Request.Context(), jobID, userID, status); syncErr != nil {
		log.Warn().Err(syncErr).Msg("Failed to sync job status after application create")
	}

	c.JSON(http.StatusCreated, created)
}

// UpdateStatus changes the application status and records history
// PUT /jobs/:id/application/status
func (h *ApplicationHandler) UpdateStatus(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status is required"})
		return
	}

	if !model.ValidStatus(req.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
		return
	}

	// Look up application by job ID
	app, err := h.appRepo.FindByJobID(c.Request.Context(), userID, jobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find application")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find application"})
		return
	}
	if app == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Application not found"})
		return
	}

	updated, err := h.appRepo.UpdateStatus(c.Request.Context(), app.ID, userID, req.Status, req.Note)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update application status")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status"})
		return
	}

	// Sync jobs.status to keep Kanban board consistent
	if syncErr := h.jobRepo.UpdateStatus(c.Request.Context(), jobID, userID, req.Status); syncErr != nil {
		log.Warn().Err(syncErr).Msg("Failed to sync job status after application status update")
	}

	c.JSON(http.StatusOK, updated)
}

// UpdateDetails updates follow-up fields without changing status
// PUT /jobs/:id/application/details
func (h *ApplicationHandler) UpdateDetails(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	var req struct {
		NextStep       string  `json:"nextStep"`
		FollowUpDate   *string `json:"followUpDate"`
		FollowUpType   string  `json:"followUpType"`
		FollowUpUrgent bool    `json:"followUpUrgent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Look up application by job ID
	app, err := h.appRepo.FindByJobID(c.Request.Context(), userID, jobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find application")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find application"})
		return
	}
	if app == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Application not found"})
		return
	}

	// Parse optional follow-up date
	var followUpDate *time.Time
	if req.FollowUpDate != nil {
		t, err := time.Parse(time.RFC3339, *req.FollowUpDate)
		if err == nil {
			followUpDate = &t
		}
	}

	updated, err := h.appRepo.UpdateDetails(
		c.Request.Context(), app.ID, userID,
		req.NextStep, followUpDate, req.FollowUpType, req.FollowUpUrgent,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update application details")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update details"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// GetHistory returns the status change timeline for a job's application
// GET /jobs/:id/application/history
func (h *ApplicationHandler) GetHistory(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	// Look up application by job ID
	app, err := h.appRepo.FindByJobID(c.Request.Context(), userID, jobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find application")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find application"})
		return
	}
	if app == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Application not found"})
		return
	}

	history, err := h.appRepo.GetHistory(c.Request.Context(), app.ID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get application history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get history"})
		return
	}

	if history == nil {
		history = []model.StatusHistory{}
	}

	c.JSON(http.StatusOK, history)
}
