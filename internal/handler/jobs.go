package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

type JobHandler struct {
	jobRepo *repository.JobRepo
	appRepo *repository.ApplicationRepo
}

func NewJobHandler(jobRepo *repository.JobRepo, appRepo *repository.ApplicationRepo) *JobHandler {
	return &JobHandler{jobRepo: jobRepo, appRepo: appRepo}
}

// ListJobs handles GET /jobs
func (h *JobHandler) ListJobs(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	filter := repository.JobFilter{
		Search:         c.Query("search"),
		LocationType:   c.Query("location"),
		BookmarkedOnly: c.Query("bookmarked") == "true",
	}

	jobs, err := h.jobRepo.List(c.Request.Context(), userID, filter)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list jobs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list jobs"})
		return
	}

	if jobs == nil {
		jobs = []model.Job{}
	}

	c.JSON(http.StatusOK, jobs)
}

// GetJob handles GET /jobs/:id
func (h *JobHandler) GetJob(c *gin.Context) {
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

	job, err := h.jobRepo.FindByID(c.Request.Context(), jobID, userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get job"})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// CreateJob handles POST /jobs
func (h *JobHandler) CreateJob(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var job model.Job
	if err := c.ShouldBindJSON(&job); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	job.UserID = userID

	created, err := h.jobRepo.Create(c.Request.Context(), &job)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save job"})
		return
	}

	c.JSON(http.StatusCreated, created)
}

// UpdateJob handles PUT /jobs/:id
func (h *JobHandler) UpdateJob(c *gin.Context) {
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

	var job model.Job
	if err := c.ShouldBindJSON(&job); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	job.ID = jobID
	job.UserID = userID

	updated, err := h.jobRepo.Update(c.Request.Context(), &job)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update job"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// DeleteJob handles DELETE /jobs/:id
func (h *JobHandler) DeleteJob(c *gin.Context) {
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

	if err := h.jobRepo.Delete(c.Request.Context(), jobID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ToggleBookmark handles POST /jobs/:id/bookmark
func (h *JobHandler) ToggleBookmark(c *gin.Context) {
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

	bookmarked, err := h.jobRepo.ToggleBookmark(c.Request.Context(), jobID, userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to toggle bookmark")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to toggle bookmark"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"bookmarked": bookmarked})
}

// Lightweight endpoint for Kanban drag-and-drop — only updates the status field
func (h *JobHandler) UpdateJobStatus(c *gin.Context) {
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status is required"})
		return
	}

	// Validate status value
	validStatuses := map[string]bool{
		"saved": true, "applied": true, "screening": true,
		"interview": true, "offer": true, "rejected": true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status. Must be: saved, applied, screening, interview, offer, rejected"})
		return
	}

	if err := h.jobRepo.UpdateStatus(c.Request.Context(), jobID, userID, req.Status); err != nil {
		log.Error().Err(err).Msg("Failed to update job status")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status"})
		return
	}

	// Sync application record if one exists (keeps pipeline tracker in sync with Kanban)
	if h.appRepo != nil {
		app, err := h.appRepo.FindByJobID(c.Request.Context(), userID, jobID)
		if err == nil && app != nil && app.Status != req.Status {
			if _, syncErr := h.appRepo.UpdateStatus(c.Request.Context(), app.ID, userID, req.Status, "Updated via Kanban board"); syncErr != nil {
				log.Warn().Err(syncErr).Msg("Failed to sync application status from Kanban")
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": req.Status})
}
