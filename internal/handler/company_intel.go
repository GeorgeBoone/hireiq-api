package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/service"
)

type CompanyHandler struct {
	yahoo  *service.YahooFinanceClient
	claude *service.ClaudeClient
}

func NewCompanyHandler(yahoo *service.YahooFinanceClient, claude *service.ClaudeClient) *CompanyHandler {
	return &CompanyHandler{yahoo: yahoo, claude: claude}
}

// GetIntel handles GET /company/intel?company=Apple&ticker=AAPL
//
// Flow:
//  1. If ticker is provided, fetch directly from Yahoo Finance
//  2. If only company name is provided, search Yahoo for ticker first
//  3. If Yahoo Finance fails or company is private, fall back to Claude AI estimation
//  4. Results are cached in-memory for 6 hours
func (h *CompanyHandler) GetIntel(c *gin.Context) {
	_, err := getUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	company := strings.TrimSpace(c.Query("company"))
	ticker := strings.TrimSpace(c.Query("ticker"))

	if company == "" && ticker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "company or ticker query param is required"})
		return
	}

	ctx := c.Request.Context()

	// ── Step 1: Try Yahoo Finance (public companies) ────────

	// If no ticker provided, search for one
	if ticker == "" && company != "" {
		found, searchErr := h.yahoo.SearchTicker(ctx, company)
		if searchErr != nil {
			log.Debug().Str("company", company).Err(searchErr).Msg("No ticker found, will try AI fallback")
		} else {
			ticker = found
		}
	}

	// If we have a ticker, fetch from Yahoo Finance
	if ticker != "" {
		intel, fetchErr := h.yahoo.FetchCompanyIntel(ctx, ticker)
		if fetchErr != nil {
			log.Warn().Str("ticker", ticker).Err(fetchErr).Msg("Yahoo Finance fetch failed, trying AI fallback")
		} else {
			// Override company name if the user provided one (Yahoo might return legal name)
			if company != "" && intel.Company == "" {
				intel.Company = company
			}
			c.JSON(http.StatusOK, intel)
			return
		}
	}

	// ── Step 2: Fall back to Claude for private companies ────

	if company == "" {
		// We only had a ticker and Yahoo failed — not much we can do
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Could not fetch company data. The ticker may be invalid.",
		})
		return
	}

	log.Info().Str("company", company).Msg("Fetching company intel via AI estimation")

	aiIntel, aiErr := h.claude.EstimateCompanyIntel(ctx, company)
	if aiErr != nil {
		log.Error().Str("company", company).Err(aiErr).Msg("AI company intel estimation failed")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Could not retrieve company information. Please try again.",
		})
		return
	}

	// Convert AI result to unified CompanyIntel format
	result := convertAIToCompanyIntel(company, aiIntel)
	c.JSON(http.StatusOK, result)
}

// convertAIToCompanyIntel maps the AI-estimated data to the same response shape
// as Yahoo Finance data, so the frontend gets a consistent interface
func convertAIToCompanyIntel(company string, ai *service.CompanyIntelAI) *service.CompanyIntel {
	intel := &service.CompanyIntel{
		Company:  ai.Company,
		IsPublic: ai.IsPublic,
		Source:   "ai_estimated",
		FetchedAt: time.Now(),
		Profile: service.CompanyProfile{
			Industry:          ai.Profile.Industry,
			Sector:            ai.Profile.Sector,
			FullTimeEmployees: ai.Profile.FullTimeEmployees,
			Website:           ai.Profile.Website,
			City:              ai.Profile.City,
			Country:           ai.Profile.Country,
			Summary:           ai.Profile.Summary,
			Founded:           ai.Profile.Founded,
		},
		Financials: service.CompanyFinance{
			RevenueGrowth:    ai.Financials.RevenueGrowth,
			ProfitMargins:    ai.Financials.ProfitMargins,
			// Private companies get formatted strings instead of raw numbers
			MarketCapFmt:     ai.Financials.Valuation,
			TotalRevenueFmt:  ai.Financials.EstimatedRevenue,
		},
		Ratings: service.CompanyRatings{},
	}

	// Use company name from request if AI didn't provide one
	if intel.Company == "" {
		intel.Company = company
	}

	// Map officers
	for _, o := range ai.Officers {
		intel.Officers = append(intel.Officers, service.Officer{
			Name:  o.Name,
			Title: o.Title,
		})
	}

	return intel
}
