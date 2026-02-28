package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

type ContactHandler struct {
	contactRepo *repository.ContactRepo
}

func NewContactHandler(contactRepo *repository.ContactRepo) *ContactHandler {
	return &ContactHandler{contactRepo: contactRepo}
}

// List handles GET /contacts
func (h *ContactHandler) List(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	search := c.Query("search")
	contacts, err := h.contactRepo.List(c.Request.Context(), userID, search)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list contacts")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list contacts"})
		return
	}

	if contacts == nil {
		contacts = []model.Contact{}
	}

	c.JSON(http.StatusOK, contacts)
}

// Create handles POST /contacts
func (h *ContactHandler) Create(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var contact model.Contact
	if err := c.ShouldBindJSON(&contact); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	contact.UserID = userID

	created, err := h.contactRepo.Create(c.Request.Context(), &contact)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create contact")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create contact"})
		return
	}

	c.JSON(http.StatusCreated, created)
}

// Update handles PUT /contacts/:id
func (h *ContactHandler) Update(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid contact ID"})
		return
	}

	var contact model.Contact
	if err := c.ShouldBindJSON(&contact); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	contact.ID = contactID
	contact.UserID = userID

	updated, err := h.contactRepo.Update(c.Request.Context(), &contact)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update contact")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update contact"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// Delete handles DELETE /contacts/:id
func (h *ContactHandler) Delete(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	contactID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid contact ID"})
		return
	}

	if err := h.contactRepo.Delete(c.Request.Context(), contactID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Contact not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
