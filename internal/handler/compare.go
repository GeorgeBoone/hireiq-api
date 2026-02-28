package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
	"github.com/yourusername/hireiq-api/internal/service"
)

type CompareHandler struct {
	claude   *service.ClaudeClient
	jobRepo  *repository.JobRepo
	userRepo *repository.UserRepo
}

func NewCompareHandler(claude *service.ClaudeClient, jobRepo *repository.JobRepo, userRepo *repository.UserRepo) *CompareHandler {
	return &CompareHandler{claude: claude, jobRepo: jobRepo, userRepo: userRepo}
}

// Compare handles POST /ai/compare
// Accepts 2-4 job IDs, fetches them, calls Claude for structured comparison
func (h *CompareHandler) Compare(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		JobIDs []string `json:"jobIds" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jobIds is required"})
		return
	}

	if len(req.JobIDs) < 2 || len(req.JobIDs) > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Between 2 and 4 job IDs are required for comparison"})
		return
	}

	// Fetch all jobs (must belong to user)
	jobs := make([]*model.Job, 0, len(req.JobIDs))
	for _, idStr := range req.JobIDs {
		jobID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid job ID: %s", idStr)})
			return
		}

		job, err := h.jobRepo.FindByID(c.Request.Context(), jobID, userID)
		if err != nil {
			log.Error().Err(err).Str("jobId", idStr).Msg("Failed to fetch job for comparison")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch job"})
			return
		}
		if job == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Job not found: %s", idStr)})
			return
		}
		jobs = append(jobs, job)
	}

	// Fetch user profile for context
	user, err := h.userRepo.FindByID(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch user profile for comparison")
	}

	// Format job descriptions
	var jobParts []string
	labels := []string{"Job A", "Job B", "Job C", "Job D"}
	for i, job := range jobs {
		jobParts = append(jobParts, formatJobForComparison(labels[i], job))
	}
	jobDescriptions := strings.Join(jobParts, "\n\n")

	// Format user profile
	profileStr := formatUserProfile(user)

	// Call Claude
	result, err := h.claude.CompareJobs(c.Request.Context(), jobDescriptions, profileStr)
	if err != nil {
		log.Error().Err(err).Msg("Failed to compare jobs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI comparison failed. Please try again."})
		return
	}

	c.JSON(http.StatusOK, result)
}

func formatJobForComparison(label string, job *model.Job) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("=== %s ===", label))
	parts = append(parts, fmt.Sprintf("Title: %s", job.Title))
	parts = append(parts, fmt.Sprintf("Company: %s", job.Company))

	if job.Location != "" {
		parts = append(parts, fmt.Sprintf("Location: %s", job.Location))
	}
	if job.SalaryRange != "" {
		parts = append(parts, fmt.Sprintf("Salary: %s", job.SalaryRange))
	}
	if job.JobType != "" {
		parts = append(parts, fmt.Sprintf("Type: %s", job.JobType))
	}
	if len(job.RequiredSkills) > 0 {
		parts = append(parts, fmt.Sprintf("Required Skills: %s", strings.Join(job.RequiredSkills, ", ")))
	}
	if len(job.PreferredSkills) > 0 {
		parts = append(parts, fmt.Sprintf("Preferred Skills: %s", strings.Join(job.PreferredSkills, ", ")))
	}
	if job.Description != "" {
		desc := job.Description
		if len(desc) > 2000 {
			desc = desc[:2000] + "..."
		}
		parts = append(parts, fmt.Sprintf("Description: %s", desc))
	}
	if len(job.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("Tags: %s", strings.Join(job.Tags, ", ")))
	}

	return strings.Join(parts, "\n")
}

func formatUserProfile(user *model.User) string {
	if user == nil {
		return "No profile data available."
	}

	var parts []string
	if len(user.Skills) > 0 {
		parts = append(parts, fmt.Sprintf("Skills: %s", strings.Join(user.Skills, ", ")))
	}
	if user.Location != "" {
		parts = append(parts, fmt.Sprintf("Location: %s", user.Location))
	}
	if user.WorkStyle != "" {
		parts = append(parts, fmt.Sprintf("Work Style: %s", user.WorkStyle))
	}
	if user.SalaryMin > 0 || user.SalaryMax > 0 {
		parts = append(parts, fmt.Sprintf("Salary Range: $%dK - $%dK", user.SalaryMin/1000, user.SalaryMax/1000))
	}

	if len(parts) == 0 {
		return "No profile data available."
	}
	return strings.Join(parts, "\n")
}
