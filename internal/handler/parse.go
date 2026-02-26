package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/service"
)

type ParseHandler struct {
	claude *service.ClaudeClient
}

func NewParseHandler(claude *service.ClaudeClient) *ParseHandler {
	return &ParseHandler{claude: claude}
}

// ParseJobPosting handles POST /jobs/parse
// Accepts either raw text or a URL, parses it with Claude, returns structured job data
func (h *ParseHandler) ParseJobPosting(c *gin.Context) {
	var req struct {
		Text string `json:"text"` // Raw pasted text
		URL  string `json:"url"`  // Or a URL to fetch first
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Need either text or URL
	if strings.TrimSpace(req.Text) == "" && strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide either 'text' or 'url'"})
		return
	}

	content := req.Text

	// If URL provided, fetch its content first
	if req.URL != "" {
		log.Info().Str("url", req.URL).Msg("Fetching job posting URL")

		fetched, err := service.FetchURLContent(c.Request.Context(), req.URL)
		if err != nil {
			log.Warn().Err(err).Str("url", req.URL).Msg("Failed to fetch URL")
			// If URL fetch fails but we also have text, fall back to text
			if content == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Could not fetch URL. Try pasting the job description text instead.",
				})
				return
			}
			// Otherwise use the text they pasted
		} else {
			// Combine URL content with any pasted text for better context
			if content != "" {
				content = "Source URL: " + req.URL + "\n\n" + content + "\n\nPage content:\n" + fetched
			} else {
				content = "Source URL: " + req.URL + "\n\n" + fetched
			}
		}
	}

	// Truncate to ~50K chars to stay within Claude's context and keep costs down
	if len(content) > 50000 {
		content = content[:50000]
	}

	log.Info().Int("contentLength", len(content)).Msg("Parsing job posting with Claude")

	parsed, err := h.claude.ParseJobPosting(c.Request.Context(), content)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse job posting")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to parse job posting. Please try again or enter details manually.",
		})
		return
	}

	// If URL was provided and source wasn't detected, try to infer from URL
	if req.URL != "" && parsed.Source == "" {
		parsed.Source = inferSource(req.URL)
	}

	// If URL was provided and no apply_url was extracted, use the original URL
	if req.URL != "" && parsed.ApplyURL == "" {
		parsed.ApplyURL = req.URL
	}

	c.JSON(http.StatusOK, parsed)
}

// inferSource guesses the job source from the URL domain
func inferSource(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, "linkedin.com"):
		return "linkedin"
	case strings.Contains(lower, "greenhouse.io"):
		return "greenhouse"
	case strings.Contains(lower, "lever.co"):
		return "lever"
	case strings.Contains(lower, "indeed.com"):
		return "indeed"
	case strings.Contains(lower, "glassdoor.com"):
		return "glassdoor"
	case strings.Contains(lower, "wellfound.com") || strings.Contains(lower, "angel.co"):
		return "angellist"
	case strings.Contains(lower, "workday.com"):
		return "workday"
	case strings.Contains(lower, "ashbyhq.com"):
		return "ashby"
	default:
		return "other"
	}
}
