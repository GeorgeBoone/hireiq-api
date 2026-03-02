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

// RemotiveClient wraps the Remotive free remote jobs API.
// No API key required.
type RemotiveClient struct {
	client *http.Client
}

func NewRemotiveClient() *RemotiveClient {
	return &RemotiveClient{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// ── Remotive API response types ──────────────────────

type remotiveResponse struct {
	JobCount int           `json:"job-count"`
	Jobs     []RemotiveJob `json:"jobs"`
}

type RemotiveJob struct {
	ID                        int      `json:"id"`
	Title                     string   `json:"title"`
	CompanyName               string   `json:"company_name"`
	CompanyLogo               string   `json:"company_logo"`
	Category                  string   `json:"category"`
	Tags                      []string `json:"tags"`
	JobType                   string   `json:"job_type"`
	PublicationDate           string   `json:"publication_date"`
	CandidateRequiredLocation string   `json:"candidate_required_location"`
	Salary                    string   `json:"salary"`
	URL                       string   `json:"url"`
	Description               string   `json:"description"`
}

// ── Search parameters ────────────────────────────────

type RemotiveQuery struct {
	Search   string // keyword search term
	Category string // slug like "software-dev", "data", "devops-sysadmin"
	Limit    int    // max results (default 20)
}

// ── Search method ────────────────────────────────────

func (c *RemotiveClient) Search(ctx context.Context, q RemotiveQuery) ([]RemotiveJob, error) {
	params := url.Values{}
	if q.Search != "" {
		params.Set("search", q.Search)
	}
	if q.Category != "" {
		params.Set("category", q.Category)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	params.Set("limit", strconv.Itoa(limit))

	reqURL := "https://remotive.com/api/remote-jobs?" + params.Encode()

	log.Info().
		Str("search", q.Search).
		Str("category", q.Category).
		Int("limit", limit).
		Msg("Searching Remotive API")

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating remotive request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Remotive API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading remotive response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Remotive API returned %d: %s",
			resp.StatusCode, string(body[:min(len(body), 500)]))
	}

	var result remotiveResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing Remotive response: %w", err)
	}

	log.Info().
		Int("results", len(result.Jobs)).
		Str("search", q.Search).
		Str("category", q.Category).
		Msg("Remotive API search complete")

	return result.Jobs, nil
}

// ── Query builder ────────────────────────────────────

// remotiveCategoryMap maps common skill keywords to Remotive category slugs.
var remotiveCategoryMap = map[string]string{
	"react":            "software-dev",
	"javascript":       "software-dev",
	"python":           "software-dev",
	"go":               "software-dev",
	"golang":           "software-dev",
	"java":             "software-dev",
	"typescript":       "software-dev",
	"rust":             "software-dev",
	"node":             "software-dev",
	"node.js":          "software-dev",
	"ruby":             "software-dev",
	"swift":            "software-dev",
	"kotlin":           "software-dev",
	"c++":              "software-dev",
	"c#":               "software-dev",
	".net":             "software-dev",
	"php":              "software-dev",
	"vue":              "software-dev",
	"angular":          "software-dev",
	"figma":            "design",
	"ui/ux":            "design",
	"design":           "design",
	"devops":           "devops-sysadmin",
	"kubernetes":       "devops-sysadmin",
	"docker":           "devops-sysadmin",
	"terraform":        "devops-sysadmin",
	"aws":              "devops-sysadmin",
	"azure":            "devops-sysadmin",
	"gcp":              "devops-sysadmin",
	"data science":     "data",
	"machine learning": "data",
	"sql":              "data",
	"analytics":        "data",
	"product":          "product",
	"qa":               "qa",
	"testing":          "qa",
}

// BuildRemotiveQueries generates Remotive queries from a user profile.
// Target roles are the PRIMARY search driver.
// Only skips if user explicitly prefers onsite-only work.
func BuildRemotiveQueries(user *model.User) []RemotiveQuery {
	// Only skip if user explicitly wants onsite-only work
	if strings.EqualFold(user.WorkStyle, "onsite") {
		return nil
	}

	seen := make(map[string]bool)
	var queries []RemotiveQuery

	// ── PRIMARY: Target roles ──
	for _, role := range user.TargetRoles {
		role = strings.TrimSpace(role)
		key := strings.ToLower(role)
		if role != "" && !seen[key] {
			seen[key] = true
			queries = append(queries, RemotiveQuery{
				Search: role,
				Limit:  50,
			})
		}
	}

	// ── SECONDARY: Skills as keyword search ──
	if len(user.Skills) > 0 && len(queries) < 3 {
		topSkills := user.Skills
		if len(topSkills) > 3 {
			topSkills = topSkills[:3]
		}
		q := strings.Join(topSkills, " ")
		key := strings.ToLower(q)
		if !seen[key] {
			seen[key] = true
			queries = append(queries, RemotiveQuery{
				Search: q,
				Limit:  50,
			})
		}
	}

	// ── TERTIARY: Skills mapped to Remotive categories ──
	categoryUsed := make(map[string]bool)
	for _, skill := range user.Skills {
		if len(queries) >= 5 {
			break
		}
		if cat, ok := remotiveCategoryMap[strings.ToLower(skill)]; ok && !categoryUsed[cat] {
			categoryUsed[cat] = true
			queries = append(queries, RemotiveQuery{
				Category: cat,
				Limit:    50,
			})
		}
	}

	// ── QUATERNARY: Experience titles ──
	if len(user.Experience) > 0 && len(queries) < 6 {
		title := strings.TrimSpace(user.Experience[0].Title)
		key := strings.ToLower(title)
		if title != "" && !seen[key] {
			seen[key] = true
			queries = append(queries, RemotiveQuery{
				Search: title,
				Limit:  30,
			})
		}
	}

	// Cap at 6 queries
	if len(queries) > 6 {
		queries = queries[:6]
	}

	return queries
}

// ── Converter ────────────────────────────────────────

// convertRemotiveJob transforms a Remotive API result into our FeedJob model.
func convertRemotiveJob(rj RemotiveJob) *model.FeedJob {
	// Parse salary — Remotive returns freeform text like "$120k-$160k" or ""
	salaryText := rj.Salary

	// Parse job type
	jobType := "full-time"
	switch strings.ToLower(rj.JobType) {
	case "part_time":
		jobType = "part-time"
	case "contract":
		jobType = "contract"
	case "freelance":
		jobType = "contract"
	case "internship":
		jobType = "internship"
	}

	// Parse posted date
	var postedAt *time.Time
	if rj.PublicationDate != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, rj.PublicationDate); err == nil {
				postedAt = &t
				break
			}
		}
	}

	// Location — always remote, may include required location
	location := "Remote"
	loc := strings.TrimSpace(rj.CandidateRequiredLocation)
	if loc != "" && !strings.EqualFold(loc, "Anywhere") && !strings.EqualFold(loc, "Worldwide") {
		location = "Remote / " + loc
	}

	// Strip HTML from description (Remotive returns HTML), then truncate (UTF-8 safe)
	desc := truncateUTF8(stripHTML(rj.Description), 2000)

	skills := rj.Tags
	if skills == nil {
		skills = []string{}
	}

	return &model.FeedJob{
		ExternalID:     fmt.Sprintf("remotive-%d", rj.ID),
		Source:         "remotive",
		Title:          rj.Title,
		Company:        rj.CompanyName,
		Location:       location,
		SalaryMin:      0,
		SalaryMax:      0,
		SalaryText:     salaryText,
		JobType:        jobType,
		Description:    desc,
		RequiredSkills: skills,
		ApplyURL:       rj.URL,
		CompanyLogo:    rj.CompanyLogo,
		PostedAt:       postedAt,
	}
}

// stripHTML is defined in claude.go and shared across the service package.
