package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

type NetworkHandler struct {
	jobRepo     *repository.JobRepo
	contactRepo *repository.ContactRepo
}

func NewNetworkHandler(jobRepo *repository.JobRepo, contactRepo *repository.ContactRepo) *NetworkHandler {
	return &NetworkHandler{jobRepo: jobRepo, contactRepo: contactRepo}
}

// ListCompanies handles GET /network/companies
func (h *NetworkHandler) ListCompanies(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	companies, err := h.jobRepo.ListCompanies(c.Request.Context(), userID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list companies")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list companies"})
		return
	}

	if companies == nil {
		companies = []model.CompanySummary{}
	}

	c.JSON(http.StatusOK, companies)
}

// GetCompanyDetail handles GET /network/companies/:company/detail
func (h *NetworkHandler) GetCompanyDetail(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	company := c.Param("company")
	if company == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Company name is required"})
		return
	}

	jobs, err := h.jobRepo.ListByCompany(c.Request.Context(), userID, company)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list company jobs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list company jobs"})
		return
	}

	contacts, err := h.contactRepo.ListByCompany(c.Request.Context(), userID, company)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list company contacts")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list company contacts"})
		return
	}

	if jobs == nil {
		jobs = []model.Job{}
	}
	if contacts == nil {
		contacts = []model.Contact{}
	}

	c.JSON(http.StatusOK, gin.H{
		"company":  company,
		"jobs":     jobs,
		"contacts": contacts,
	})
}
