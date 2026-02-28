package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// JSearchClient wraps the JSearch API on RapidAPI
type JSearchClient struct {
	apiKey string
	client *http.Client
}

func NewJSearchClient(apiKey string) *JSearchClient {
	return &JSearchClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// ── JSearch API response types ────────────────────────

type jsearchResponse struct {
	Status string        `json:"status"`
	Data   []JSearchJob  `json:"data"`
}

// JSearchJob is the raw job listing from the API
type JSearchJob struct {
	JobID              string  `json:"job_id"`
	JobTitle           string  `json:"job_title"`
	EmployerName       string  `json:"employer_name"`
	EmployerLogo       string  `json:"employer_logo"`
	JobCity            string  `json:"job_city"`
	JobState           string  `json:"job_state"`
	JobCountry         string  `json:"job_country"`
	JobIsRemote        bool    `json:"job_is_remote"`
	JobDescription     string  `json:"job_description"`
	JobEmploymentType  string  `json:"job_employment_type"`
	JobApplyLink       string  `json:"job_apply_link"`
	JobMinSalary       *float64 `json:"job_min_salary"`
	JobMaxSalary       *float64 `json:"job_max_salary"`
	JobSalaryCurrency  string  `json:"job_salary_currency"`
	JobSalaryPeriod    string  `json:"job_salary_period"`
	JobPostedAt        string  `json:"job_posted_at_datetime_utc"`
	JobRequiredSkills  []string `json:"job_required_skills"`
}

// ── Search parameters ─────────────────────────────────

type JSearchQuery struct {
	Query      string // e.g. "React developer"
	Location   string // e.g. "San Francisco" or "" for remote
	RemoteOnly bool
	PageSize   int    // max 20
}

// ── Search method ─────────────────────────────────────

func (c *JSearchClient) Search(ctx context.Context, q JSearchQuery) ([]JSearchJob, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("RapidAPI key not configured")
	}

	// Build the query string
	query := q.Query
	if q.Location != "" && !q.RemoteOnly {
		query += " in " + q.Location
	}
	if q.RemoteOnly {
		query += " remote"
	}

	pageSize := q.PageSize
	if pageSize == 0 || pageSize > 20 {
		pageSize = 10
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("page", "1")
	params.Set("num_pages", "1")
	params.Set("date_posted", "week") // Only recent listings

	if q.RemoteOnly {
		params.Set("remote_jobs_only", "true")
	}

	reqURL := "https://jsearch.p.rapidapi.com/search?" + params.Encode()

	log.Info().
		Str("query", query).
		Str("url", reqURL).
		Msg("Searching JSearch API")

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("x-rapidapi-host", "jsearch.p.rapidapi.com")
	req.Header.Set("x-rapidapi-key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling JSearch API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JSearch API returned %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
	}

	var result jsearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing JSearch response: %w", err)
	}

	log.Info().
		Int("results", len(result.Data)).
		Str("query", query).
		Msg("JSearch API returned results")

	return result.Data, nil
}

// BuildQueriesFromProfile generates JSearch queries based on user skills and preferences
func BuildQueriesFromProfile(skills []string, location string, workStyle string) []JSearchQuery {
	var queries []JSearchQuery
	remoteOnly := strings.EqualFold(workStyle, "remote")

	if len(skills) == 0 {
		// Fallback: generic software job search
		queries = append(queries, JSearchQuery{
			Query:      "software engineer",
			Location:   location,
			RemoteOnly: remoteOnly,
			PageSize:   10,
		})
		return queries
	}

	// Strategy: combine top skills into 2-3 targeted queries
	// rather than one query per skill (saves API calls)

	// Query 1: Top 2-3 skills combined
	topSkills := skills
	if len(topSkills) > 3 {
		topSkills = topSkills[:3]
	}
	queries = append(queries, JSearchQuery{
		Query:      strings.Join(topSkills, " ") + " developer",
		Location:   location,
		RemoteOnly: remoteOnly,
		PageSize:   10,
	})

	// Query 2: If enough skills, use a different combination
	if len(skills) > 3 {
		secondSet := skills[2:]
		if len(secondSet) > 3 {
			secondSet = secondSet[:3]
		}
		queries = append(queries, JSearchQuery{
			Query:      strings.Join(secondSet, " ") + " engineer",
			Location:   location,
			RemoteOnly: remoteOnly,
			PageSize:   10,
		})
	}

	return queries
}
