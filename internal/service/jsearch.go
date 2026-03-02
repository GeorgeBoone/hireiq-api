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
	NumPages   int // pages to fetch per query (default 1, max 3)
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

	numPages := q.NumPages
	if numPages <= 0 || numPages > 5 {
		numPages = 1
	}

	// Fetch each page separately — more reliable than num_pages which
	// may be capped on free-tier RapidAPI plans.
	var allResults []JSearchJob

	for page := 1; page <= numPages; page++ {
		params := url.Values{}
		params.Set("query", query)
		params.Set("page", strconv.Itoa(page))
		params.Set("num_pages", "1")
		params.Set("date_posted", "month")

		if q.RemoteOnly {
			params.Set("remote_jobs_only", "true")
		}

		reqURL := "https://jsearch.p.rapidapi.com/search?" + params.Encode()

		log.Info().
			Str("query", query).
			Int("page", page).
			Msg("Searching JSearch API")

		results, err := c.fetchPage(ctx, reqURL)
		if err != nil {
			log.Error().Err(err).Int("page", page).Str("query", query).Msg("JSearch page fetch failed")
			break // stop paging on error (likely rate limit or no more results)
		}

		allResults = append(allResults, results...)

		// If this page returned fewer than 10, there are no more pages
		if len(results) < 10 {
			break
		}
	}

	log.Info().
		Int("results", len(allResults)).
		Str("query", query).
		Int("pages", numPages).
		Msg("JSearch API search complete")

	return allResults, nil
}

// fetchPage makes a single HTTP request to JSearch and returns the results.
func (c *JSearchClient) fetchPage(ctx context.Context, reqURL string) ([]JSearchJob, error) {
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

	return result.Data, nil
}

// BuildQueriesFromProfile generates JSearch queries from the user profile.
// Target roles are the PRIMARY search driver (highest page counts).
// Skills and experience titles are SECONDARY for broader coverage.
func BuildQueriesFromProfile(user *model.User) []JSearchQuery {
	remoteOnly := strings.EqualFold(user.WorkStyle, "remote")
	location := user.Location
	seen := make(map[string]bool)

	add := func(queries []JSearchQuery, query string, pages int) []JSearchQuery {
		q := strings.TrimSpace(query)
		key := strings.ToLower(q)
		if q == "" || seen[key] {
			return queries
		}
		seen[key] = true
		return append(queries, JSearchQuery{
			Query:      q,
			Location:   location,
			RemoteOnly: remoteOnly,
			NumPages:   pages,
		})
	}

	var queries []JSearchQuery

	// ── PRIMARY: Target roles (highest priority, most pages) ──
	for _, role := range user.TargetRoles {
		role = strings.TrimSpace(role)
		if role != "" {
			queries = add(queries, role, 3)
		}
	}

	// ── SECONDARY: Skills-based queries (fill remaining slots) ──
	if len(user.Skills) > 0 && len(queries) < 4 {
		topSkills := user.Skills
		if len(topSkills) > 3 {
			topSkills = topSkills[:3]
		}
		queries = add(queries, strings.Join(topSkills, " ")+" developer", 2)
	}

	if len(user.Skills) > 3 && len(queries) < 5 {
		secondSet := user.Skills[3:]
		if len(secondSet) > 3 {
			secondSet = secondSet[:3]
		}
		queries = add(queries, strings.Join(secondSet, " ")+" engineer", 2)
	}

	// ── TERTIARY: Experience titles ──
	for i := 0; i < len(user.Experience) && i < 2 && len(queries) < 6; i++ {
		title := strings.TrimSpace(user.Experience[i].Title)
		if title != "" {
			queries = add(queries, title, 2)
		}
	}

	// ── FALLBACK: If no target roles, skills, or experience ──
	if len(queries) == 0 {
		return append(queries, JSearchQuery{
			Query:      "software engineer",
			Location:   location,
			RemoteOnly: remoteOnly,
			NumPages:   2,
		})
	}

	// Safety cap
	if len(queries) > 8 {
		queries = queries[:8]
	}

	return queries
}
