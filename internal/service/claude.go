package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClaudeClient wraps the Anthropic Messages API
type ClaudeClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewClaudeClient(apiKey, baseURL string) *ClaudeClient {
	return &ClaudeClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── Anthropic API request/response types ──────────────

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ── Parsed job result ─────────────────────────────────

// ParsedJob is the structured data Claude extracts from a job posting
type ParsedJob struct {
	Title           string   `json:"title"`
	Company         string   `json:"company"`
	Location        string   `json:"location"`
	SalaryRange     string   `json:"salary_range"`
	JobType         string   `json:"job_type"`
	Description     string   `json:"description"`
	RequiredSkills  []string `json:"required_skills"`
	PreferredSkills []string `json:"preferred_skills"`
	ApplyURL        string   `json:"apply_url"`
	HiringEmail     string   `json:"hiring_email"`
	Tags            []string `json:"tags"`
	Source          string   `json:"source"`
}

// ── Parse job posting ─────────────────────────────────

const parseSystemPrompt = `You are a job posting parser. Extract structured data from job postings.

Always respond with ONLY a JSON object (no markdown, no backticks, no explanation) with these fields:
{
  "title": "Job title",
  "company": "Company name",
  "location": "Location (include Remote if applicable)",
  "salary_range": "Salary range if mentioned, empty string if not",
  "job_type": "full-time, part-time, contract, or internship",
  "description": "A clean summary of the role (2-4 sentences max, not the full posting)",
  "required_skills": ["skill1", "skill2"],
  "preferred_skills": ["skill1", "skill2"],
  "apply_url": "Application URL if found, empty string if not",
  "hiring_email": "Recruiter/hiring email if found, empty string if not",
  "tags": ["relevant", "category", "tags"],
  "source": "linkedin, greenhouse, lever, indeed, glassdoor, angellist, or other"
}

Rules:
- Extract only what's explicitly stated. Don't invent data.
- For skills, separate required vs preferred/nice-to-have.
- For tags, infer 2-5 relevant categories (e.g. "fintech", "series-b", "startup", "enterprise").
- For source, infer from the content or URL if possible.
- Keep the description concise — summarize the role, don't copy the full posting.
- If a field isn't present in the posting, use an empty string or empty array.`

// ParseJobPosting sends raw text (or fetched URL content) to Claude for extraction
func (c *ClaudeClient) ParseJobPosting(ctx context.Context, rawText string) (*ParsedJob, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Claude API key not configured")
	}

	reqBody := claudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 1500,
		System:    parseSystemPrompt,
		Messages: []claudeMessage{
			{
				Role:    "user",
				Content: "Parse this job posting and return the JSON:\n\n" + rawText,
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Claude API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Claude API returned %d: %s", resp.StatusCode, string(body))
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return nil, fmt.Errorf("parsing Claude response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from Claude")
	}

	// Extract the text content and parse as JSON
	text := claudeResp.Content[0].Text

	// Strip markdown code fences if Claude includes them
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		// Remove opening ```json or ```
		if idx := strings.Index(text, "\n"); idx != -1 {
			text = text[idx+1:]
		}
		// Remove closing ```
		if idx := strings.LastIndex(text, "```"); idx != -1 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}

	var parsed ParsedJob
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("parsing extracted job data: %w (raw: %s)", err, text)
	}

	return &parsed, nil
}

// ── Fetch URL content ─────────────────────────────────

// FetchURLContent retrieves the text content of a URL for parsing
func FetchURLContent(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Set a browser-like user agent so job sites don't block us
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("URL returned status %d", resp.StatusCode)
	}

	// Limit to 100KB to avoid massive pages
	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return "", fmt.Errorf("reading URL content: %w", err)
	}

	return string(body), nil
}
