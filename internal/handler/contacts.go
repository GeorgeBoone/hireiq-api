package handler

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"

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

// ImportLinkedIn handles POST /contacts/import/linkedin
// Accepts a LinkedIn connections CSV and bulk-creates contacts
func (h *ContactHandler) ImportLinkedIn(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only CSV files are supported"})
		return
	}

	// Limit to 5MB
	if header.Size > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File too large. Maximum size is 5MB."})
		return
	}

	// Parse CSV
	reader := csv.NewReader(file)

	// Read header row
	headers, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV headers"})
		return
	}

	// Strip UTF-8 BOM from first header (Windows LinkedIn exports)
	if len(headers) > 0 {
		headers[0] = strings.TrimPrefix(headers[0], "\xef\xbb\xbf")
	}

	// Map column names to indices
	colMap := make(map[string]int)
	for i, h := range headers {
		colMap[strings.TrimSpace(h)] = i
	}

	// Validate required columns
	if _, ok := colMap["First Name"]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LinkedIn CSV format. Missing 'First Name' column."})
		return
	}
	if _, ok := colMap["Last Name"]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LinkedIn CSV format. Missing 'Last Name' column."})
		return
	}

	// Parse rows into contacts
	var contacts []model.Contact
	var parseErrors int

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			parseErrors++
			continue
		}

		firstName := getCSVField(record, colMap, "First Name")
		lastName := getCSVField(record, colMap, "Last Name")
		company := getCSVField(record, colMap, "Company")
		position := getCSVField(record, colMap, "Position")
		email := getCSVField(record, colMap, "Email Address")

		name := strings.TrimSpace(firstName + " " + lastName)

		// Skip rows with no name or no company
		if name == "" || company == "" {
			parseErrors++
			continue
		}

		contacts = append(contacts, model.Contact{
			Name:       name,
			Company:    company,
			Role:       position,
			Email:      email,
			Connection: "1st", // LinkedIn connections are 1st degree
		})
	}

	if len(contacts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid contacts found in CSV"})
		return
	}

	imported, skipped, err := h.contactRepo.BulkCreate(c.Request.Context(), userID, contacts)
	if err != nil {
		log.Error().Err(err).Msg("Failed to bulk import contacts")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to import contacts"})
		return
	}

	log.Info().
		Int("imported", imported).
		Int("skipped", skipped).
		Int("parseErrors", parseErrors).
		Str("filename", header.Filename).
		Msg("LinkedIn CSV import completed")

	c.JSON(http.StatusOK, gin.H{
		"imported":    imported,
		"skipped":     skipped,
		"parseErrors": parseErrors,
		"total":       len(contacts) + parseErrors,
	})
}

// getCSVField safely retrieves a field from a CSV record by column name
func getCSVField(record []string, colMap map[string]int, column string) string {
	idx, ok := colMap[column]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}
