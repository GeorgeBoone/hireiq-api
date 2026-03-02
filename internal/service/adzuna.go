package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
)

// AdzunaClient wraps the Adzuna job search API.
// Requires app_id and app_key from developer.adzuna.com (free tier available).
type AdzunaClient struct {
	appID  string
	appKey string
	client *http.Client
}

func NewAdzunaClient(appID, appKey string) *AdzunaClient {
	return &AdzunaClient{
		appID:  appID,
		appKey: appKey,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Enabled returns true if Adzuna API keys are configured.
func (c *AdzunaClient) Enabled() bool {
	return c.appID != "" && c.appKey != ""
}

// ── Adzuna API response types ────────────────────────

type adzunaResponse struct {
	Results []AdzunaJob `json:"results"`
	Count   int         `json:"count"`
}

type AdzunaJob struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Company      adzunaCompany  `json:"company"`
	Location     adzunaLocation `json:"location"`
	SalaryMin    float64        `json:"salary_min"`
	SalaryMax    float64        `json:"salary_max"`
	RedirectURL  string         `json:"redirect_url"`
	Created      string         `json:"created"`
	Category     adzunaCategory `json:"category"`
	ContractType string         `json:"contract_type"`
	ContractTime string         `json:"contract_time"`
}

type adzunaCompany struct {
	DisplayName string `json:"display_name"`
}

type adzunaLocation struct {
	DisplayName string   `json:"display_name"`
	Area        []string `json:"area"`
}

type adzunaCategory struct {
	Label string `json:"label"`
	Tag   string `json:"tag"`
}

// ── Search parameters ────────────────────────────────

type AdzunaQuery struct {
	Keywords       string // "what" parameter
	Location       string // "where" parameter
	Country        string // 2-letter country code (default "us")
	ResultsPerPage int    // max 50
	MaxDaysOld     int    // filter by recency
	FullTime       bool
	SalaryMin      int
}

// ── Search method ────────────────────────────────────

func (c *AdzunaClient) Search(ctx context.Context, q AdzunaQuery) ([]AdzunaJob, error) {
	if !c.Enabled() {
		return nil, nil // silently skip if not configured
	}

	country := q.Country
	if country == "" {
		country = "us"
	}
	resultsPerPage := q.ResultsPerPage
	if resultsPerPage <= 0 || resultsPerPage > 50 {
		resultsPerPage = 25
	}

	params := url.Values{}
	params.Set("app_id", c.appID)
	params.Set("app_key", c.appKey)
	params.Set("results_per_page", strconv.Itoa(resultsPerPage))
	params.Set("sort_by", "date")
	params.Set("content-type", "application/json")

	if q.Keywords != "" {
		params.Set("what", q.Keywords)
	}
	if q.Location != "" {
		params.Set("where", q.Location)
	}
	if q.MaxDaysOld > 0 {
		params.Set("max_days_old", strconv.Itoa(q.MaxDaysOld))
	}
	if q.FullTime {
		params.Set("full_time", "1")
	}
	if q.SalaryMin > 0 {
		params.Set("salary_min", strconv.Itoa(q.SalaryMin))
	}

	reqURL := fmt.Sprintf("https://api.adzuna.com/v1/api/jobs/%s/search/1?%s",
		country, params.Encode())

	log.Info().
		Str("keywords", q.Keywords).
		Str("location", q.Location).
		Str("country", country).
		Msg("Searching Adzuna API")

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating adzuna request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Adzuna API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading adzuna response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Adzuna API returned %d: %s",
			resp.StatusCode, string(body[:min(len(body), 500)]))
	}

	var result adzunaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing Adzuna response: %w", err)
	}

	log.Info().
		Int("results", len(result.Results)).
		Str("keywords", q.Keywords).
		Msg("Adzuna API search complete")

	return result.Results, nil
}

// ── Query builder ────────────────────────────────────

// BuildAdzunaQueries generates Adzuna queries from a user profile.
// Target roles are the PRIMARY search driver.
func BuildAdzunaQueries(user *model.User) []AdzunaQuery {
	isRemote := strings.EqualFold(user.WorkStyle, "remote")
	location := user.Location

	seen := make(map[string]bool)
	var queries []AdzunaQuery

	add := func(keywords string) {
		k := strings.ToLower(strings.TrimSpace(keywords))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true

		q := AdzunaQuery{
			Keywords:       keywords,
			Country:        "us",
			ResultsPerPage: 50,
			MaxDaysOld:     30,
			FullTime:       true,
			SalaryMin:      user.SalaryMin,
		}

		if isRemote {
			q.Keywords = keywords + " remote"
		} else if location != "" {
			q.Location = location
		}

		queries = append(queries, q)
	}

	// ── PRIMARY: Target roles ──
	for _, role := range user.TargetRoles {
		role = strings.TrimSpace(role)
		if role != "" {
			add(role)
		}
	}

	// ── SECONDARY: Skills-based queries ──
	if len(user.Skills) > 0 && len(queries) < 4 {
		topSkills := user.Skills
		if len(topSkills) > 3 {
			topSkills = topSkills[:3]
		}
		add(strings.Join(topSkills, " "))
	}

	// ── TERTIARY: Experience title ──
	if len(user.Experience) > 0 && len(queries) < 5 {
		title := strings.TrimSpace(user.Experience[0].Title)
		if title != "" {
			add(title)
		}
	}

	// ── FALLBACK ──
	if len(queries) == 0 {
		add("software engineer")
		add("developer")
	}

	// Cap at 6 queries
	if len(queries) > 6 {
		queries = queries[:6]
	}

	return queries
}

// ── Converter ────────────────────────────────────────

// convertAdzunaJob transforms an Adzuna API result into our FeedJob model.
func convertAdzunaJob(aj AdzunaJob) *model.FeedJob {
	salaryMin := int(aj.SalaryMin)
	salaryMax := int(aj.SalaryMax)
	salaryText := ""
	if salaryMin > 0 || salaryMax > 0 {
		salaryText = fmt.Sprintf("$%dk - $%dk/yr", salaryMin/1000, salaryMax/1000)
	}

	// Parse job type
	jobType := "full-time"
	switch strings.ToLower(aj.ContractTime) {
	case "part_time":
		jobType = "part-time"
	}
	switch strings.ToLower(aj.ContractType) {
	case "contract":
		jobType = "contract"
	}

	// Parse posted date
	var postedAt *time.Time
	if aj.Created != "" {
		if t, err := time.Parse(time.RFC3339, aj.Created); err == nil {
			postedAt = &t
		}
	}

	// Location
	location := aj.Location.DisplayName
	if location == "" && len(aj.Location.Area) > 0 {
		location = strings.Join(aj.Location.Area, ", ")
	}

	// Truncate description (UTF-8 safe)
	desc := truncateUTF8(aj.Description, 2000)

	return &model.FeedJob{
		ExternalID:     fmt.Sprintf("adzuna-%s", aj.ID),
		Source:         "adzuna",
		Title:          aj.Title,
		Company:        aj.Company.DisplayName,
		Location:       location,
		SalaryMin:      salaryMin,
		SalaryMax:      salaryMax,
		SalaryText:     salaryText,
		JobType:        jobType,
		Description:    desc,
		RequiredSkills: []string{}, // Adzuna doesn't provide skills
		ApplyURL:       aj.RedirectURL,
		CompanyLogo:    "", // Adzuna doesn't provide logos
		PostedAt:       postedAt,
	}
}
