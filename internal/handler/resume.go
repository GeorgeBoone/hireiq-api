package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/repository"
	"github.com/yourusername/hireiq-api/internal/service"
)

type ResumeHandler struct {
	claude  *service.ClaudeClient
	jobRepo *repository.JobRepo
}

func NewResumeHandler(claude *service.ClaudeClient, jobRepo *repository.JobRepo) *ResumeHandler {
	return &ResumeHandler{claude: claude, jobRepo: jobRepo}
}

// Upload handles POST /resume/upload
// Accepts a PDF file via multipart form, extracts text, returns it
func (h *ResumeHandler) Upload(c *gin.Context) {
	_, err := getUserID(c)
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

	// Validate file type
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only PDF files are supported"})
		return
	}

	// Limit to 10MB
	if header.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File too large. Maximum size is 10MB."})
		return
	}

	// Read file into memory
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read uploaded file")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	// Validate PDF magic bytes (header must start with %PDF)
	if len(fileBytes) < 4 || string(fileBytes[:4]) != "%PDF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid PDF file"})
		return
	}

	// Extract text
	text, err := extractPDFText(fileBytes)
	if err != nil {
		log.Error().Err(err).Msg("Failed to extract text from PDF")
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "Could not extract text from this PDF. It may be image-based or corrupted.",
		})
		return
	}

	text = strings.TrimSpace(text)
	if len(text) < 50 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "Very little text was extracted. This PDF may be image-based (scanned). Try a text-based PDF.",
		})
		return
	}

	log.Info().
		Str("filename", header.Filename).
		Int("bytes", len(fileBytes)).
		Int("textLen", len(text)).
		Msg("Resume PDF text extracted")

	c.JSON(http.StatusOK, gin.H{
		"text":     text,
		"filename": header.Filename,
	})
}

// Critique handles POST /resume/critique
// Sends resume text to Claude for structured analysis
func (h *ResumeHandler) Critique(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		ResumeText string `json:"resumeText" binding:"required"`
		JobID      string `json:"jobId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resumeText is required"})
		return
	}

	if len(req.ResumeText) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Resume text is too short"})
		return
	}

	// Cap at 30K chars
	if len(req.ResumeText) > 30000 {
		req.ResumeText = req.ResumeText[:30000]
	}

	// Optionally fetch target job for alignment context
	var jobContext string
	if req.JobID != "" {
		jobUUID, parseErr := uuid.Parse(req.JobID)
		if parseErr == nil {
			job, findErr := h.jobRepo.FindByID(c.Request.Context(), jobUUID, userID)
			if findErr == nil && job != nil {
				jobContext = fmt.Sprintf(
					"Target Role: %s at %s\nRequired Skills: %s\nPreferred Skills: %s\nJob Description: %s",
					job.Title, job.Company,
					strings.Join(job.RequiredSkills, ", "),
					strings.Join(job.PreferredSkills, ", "),
					truncateStr(job.Description, 500),
				)
			}
		}
	}

	log.Info().Int("resumeLen", len(req.ResumeText)).Bool("hasJob", jobContext != "").Msg("Running AI resume critique")

	result, err := h.claude.CritiqueResume(c.Request.Context(), req.ResumeText, jobContext)
	if err != nil {
		log.Error().Err(err).Msg("Failed to critique resume")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI analysis failed. Please try again."})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Fix handles POST /resume/fix
// Gets before/after fix suggestions for a specific issue
func (h *ResumeHandler) Fix(c *gin.Context) {
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		ResumeText string `json:"resumeText" binding:"required"`
		Issue      struct {
			Cat string `json:"cat"`
			Sev string `json:"sev"`
			Msg string `json:"msg"`
		} `json:"issue" binding:"required"`
		JobID string `json:"jobId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resumeText and issue are required"})
		return
	}

	// Optionally fetch job context
	var jobContext string
	if req.JobID != "" {
		jobUUID, parseErr := uuid.Parse(req.JobID)
		if parseErr == nil {
			job, findErr := h.jobRepo.FindByID(c.Request.Context(), jobUUID, userID)
			if findErr == nil && job != nil {
				jobContext = fmt.Sprintf("Target role: %s at %s\nRequired Skills: %s",
					job.Title, job.Company, strings.Join(job.RequiredSkills, ", "))
			}
		}
	}

	log.Info().Str("category", req.Issue.Cat).Str("severity", req.Issue.Sev).Msg("Getting AI fix suggestions")

	result, err := h.claude.FixResumeIssue(
		c.Request.Context(),
		req.ResumeText, req.Issue.Cat, req.Issue.Sev, req.Issue.Msg,
		jobContext,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get fix suggestions")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI fix suggestions failed. Please try again."})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ParseToProfile handles POST /resume/parse-profile
// Sends resume text to Claude and returns structured profile data
func (h *ResumeHandler) ParseToProfile(c *gin.Context) {
	_, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var req struct {
		ResumeText string `json:"resumeText" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resumeText is required"})
		return
	}

	if len(req.ResumeText) < 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Resume text is too short"})
		return
	}

	// Cap at 30K chars
	if len(req.ResumeText) > 30000 {
		req.ResumeText = req.ResumeText[:30000]
	}

	log.Info().Int("resumeLen", len(req.ResumeText)).Msg("Parsing resume to profile")

	result, err := h.claude.ParseResumeToProfile(c.Request.Context(), req.ResumeText)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse resume to profile")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI profile parsing failed. Please try again."})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ── Helpers ──────────────────────────────────────────

func extractPDFText(data []byte) (string, error) {
	// Write to temp file — ledongthuc/pdf requires a file reader
	tmpFile, err := os.CreateTemp("", "resume-*.pdf")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}

	f, reader, err := pdf.Open(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("opening PDF: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	numPages := reader.NumPage()

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			log.Warn().Int("page", i).Err(err).Msg("Failed to extract text from PDF page")
			continue
		}

		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(text)
	}

	return sb.String(), nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
