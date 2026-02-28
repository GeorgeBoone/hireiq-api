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
	text = stripCodeFences(text)

	var parsed ParsedJob
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("parsing extracted job data: %w (raw: %s)", err, text)
	}

	return &parsed, nil
}

// ── Fetch URL content ─────────────────────────────────

// FetchURLContent retrieves the text content of a URL for parsing.
// It extracts JSON-LD structured data if available, strips HTML tags,
// and attempts to fetch additional tab content from common ATS platforms.
func FetchURLContent(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Set a browser-like user agent so job sites don't block us
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("URL returned status %d", resp.StatusCode)
	}

	// Increase limit to 500KB to capture full page content including tabs
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
	if err != nil {
		return "", fmt.Errorf("reading URL content: %w", err)
	}

	html := string(body)
	var parts []string

	// 1. Extract JSON-LD structured data (highest quality — many ATS embed this)
	jsonLD := extractAllJSONLD(html)
	if jsonLD != "" {
		parts = append(parts, "=== STRUCTURED JOB DATA (JSON-LD) ===\n"+jsonLD)
	}

	// 2. Extract content from common ATS embedded data patterns
	atsData := extractATSData(html)
	if atsData != "" {
		parts = append(parts, "=== ATS JOB DATA ===\n"+atsData)
	}

	// 3. Strip HTML and extract visible text
	cleanText := stripHTML(html)
	// Collapse excessive whitespace
	for strings.Contains(cleanText, "\n\n\n") {
		cleanText = strings.ReplaceAll(cleanText, "\n\n\n", "\n\n")
	}
	cleanText = strings.TrimSpace(cleanText)

	if cleanText != "" {
		parts = append(parts, "=== PAGE TEXT ===\n"+cleanText)
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("no content extracted from URL")
	}

	result := strings.Join(parts, "\n\n")

	// Truncate to stay within reasonable limits for Claude
	if len(result) > 80000 {
		result = result[:80000]
	}

	return result, nil
}

// extractAllJSONLD finds all <script type="application/ld+json"> blocks and returns
// any that look like job postings (JobPosting schema or contain job-related fields)
func extractAllJSONLD(html string) string {
	var results []string
	searchHTML := html
	ldTag := `<script type="application/ld+json"`

	for {
		startIdx := strings.Index(strings.ToLower(searchHTML), strings.ToLower(ldTag))
		if startIdx == -1 {
			break
		}

		// Find the closing > of the script tag
		gtIdx := strings.Index(searchHTML[startIdx:], ">")
		if gtIdx == -1 {
			break
		}
		contentStart := startIdx + gtIdx + 1

		// Find </script>
		endIdx := strings.Index(strings.ToLower(searchHTML[contentStart:]), "</script>")
		if endIdx == -1 {
			break
		}

		jsonContent := strings.TrimSpace(searchHTML[contentStart : contentStart+endIdx])

		// Check if this JSON-LD is job-related
		lower := strings.ToLower(jsonContent)
		if strings.Contains(lower, "jobposting") ||
			strings.Contains(lower, "jobtitle") ||
			strings.Contains(lower, "job_title") ||
			strings.Contains(lower, "hiringorganization") ||
			strings.Contains(lower, "basesalary") ||
			strings.Contains(lower, "employmenttype") ||
			strings.Contains(lower, "description") {
			// Pretty-print if valid JSON
			var prettyBuf bytes.Buffer
			if err := json.Indent(&prettyBuf, []byte(jsonContent), "", "  "); err == nil {
				results = append(results, prettyBuf.String())
			} else {
				results = append(results, jsonContent)
			}
		}

		searchHTML = searchHTML[contentStart+endIdx:]
	}

	return strings.Join(results, "\n\n")
}

// extractATSData looks for common ATS platform data patterns embedded in script tags
// (e.g., Workday, Taleo, Greenhouse, Lever store job data in JS variables or data attributes)
func extractATSData(html string) string {
	var parts []string

	// Pattern 1: __NEXT_DATA__ (Next.js sites like Greenhouse, Ashby)
	if data := extractBetween(html, `<script id="__NEXT_DATA__" type="application/json">`, `</script>`); data != "" {
		// Extract just the job-related portion to avoid massive payloads
		if trimmed := extractJobFromNextData(data); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	// Pattern 2: window.__data or window.__INITIAL_STATE__ (common SPA pattern)
	for _, prefix := range []string{
		"window.__data", "window.__INITIAL_STATE__", "window.__JOB_DATA__",
		"window.jobData", "window._initialData", "window.__PRELOADED_STATE__",
	} {
		if data := extractJSVariable(html, prefix); data != "" {
			parts = append(parts, data)
		}
	}

	// Pattern 3: data-automation attributes or data-job-* attributes
	// Many ATS embed tab content in hidden divs with data attributes
	for _, attr := range []string{
		`data-tab="description"`, `data-tab="benefits"`, `data-tab="requirements"`,
		`data-automation="jobDescription"`, `data-automation="jobBenefits"`,
		`id="job-description"`, `id="job-benefits"`, `id="job-requirements"`,
		`class="job-description"`, `class="job-details"`, `class="job-benefits"`,
		`data-testid="job-description"`, `data-testid="benefits"`,
	} {
		if content := extractElementContent(html, attr); content != "" {
			stripped := stripHTML(content)
			if len(stripped) > 50 { // Only include if substantial
				parts = append(parts, stripped)
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

// extractBetween extracts content between two markers
func extractBetween(html, startMarker, endMarker string) string {
	startIdx := strings.Index(html, startMarker)
	if startIdx == -1 {
		return ""
	}
	contentStart := startIdx + len(startMarker)
	endIdx := strings.Index(html[contentStart:], endMarker)
	if endIdx == -1 {
		return ""
	}
	return strings.TrimSpace(html[contentStart : contentStart+endIdx])
}

// extractJSVariable extracts the value assigned to a JS variable
func extractJSVariable(html, varName string) string {
	idx := strings.Index(html, varName)
	if idx == -1 {
		return ""
	}
	// Find the = sign
	rest := html[idx+len(varName):]
	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx == -1 || eqIdx > 5 {
		return ""
	}
	rest = strings.TrimSpace(rest[eqIdx+1:])

	// Find matching end (look for </script> or ;)
	endIdx := strings.Index(rest, "</script>")
	if endIdx == -1 {
		endIdx = strings.Index(rest, ";\n")
		if endIdx == -1 {
			return ""
		}
	}
	data := strings.TrimSpace(rest[:endIdx])
	data = strings.TrimSuffix(data, ";")

	// Only return if it looks like JSON
	if (strings.HasPrefix(data, "{") || strings.HasPrefix(data, "[")) && len(data) > 50 {
		// Truncate very large embedded data
		if len(data) > 30000 {
			data = data[:30000]
		}
		return data
	}
	return ""
}

// extractJobFromNextData extracts job-relevant data from Next.js __NEXT_DATA__
func extractJobFromNextData(data string) string {
	// Parse as JSON and try to find job-related keys
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return ""
	}

	// Look for props.pageProps which usually contains the data
	props, ok := parsed["props"].(map[string]interface{})
	if !ok {
		return ""
	}
	pageProps, ok := props["pageProps"].(map[string]interface{})
	if !ok {
		return ""
	}

	// Re-serialize just the pageProps (much smaller than full __NEXT_DATA__)
	result, err := json.MarshalIndent(pageProps, "", "  ")
	if err != nil {
		return ""
	}

	output := string(result)
	if len(output) > 30000 {
		output = output[:30000]
	}
	return output
}

// extractElementContent finds an HTML element with a given attribute and returns its inner content
func extractElementContent(html, attr string) string {
	idx := strings.Index(html, attr)
	if idx == -1 {
		return ""
	}

	// Walk backward to find the opening <
	start := idx
	for start > 0 && html[start] != '<' {
		start--
	}

	// Find the > that closes this opening tag
	gtIdx := strings.Index(html[idx:], ">")
	if gtIdx == -1 {
		return ""
	}
	contentStart := idx + gtIdx + 1

	// Determine the tag name
	tagPart := html[start+1 : idx]
	tagName := strings.Fields(tagPart)[0]

	// Find the matching closing tag (handle nesting)
	closeTag := "</" + tagName + ">"
	openTag := "<" + tagName
	depth := 1
	pos := contentStart

	for depth > 0 && pos < len(html) {
		nextOpen := strings.Index(html[pos:], openTag)
		nextClose := strings.Index(html[pos:], closeTag)

		if nextClose == -1 {
			break
		}

		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			pos += nextOpen + len(openTag)
		} else {
			depth--
			if depth == 0 {
				return html[contentStart : pos+nextClose]
			}
			pos += nextClose + len(closeTag)
		}
	}

	return ""
}

// stripHTML removes HTML tags and decodes common entities
func stripHTML(html string) string {
	var result strings.Builder
	inTag := false
	inScript := false
	inStyle := false

	lower := strings.ToLower(html)

	for i := 0; i < len(html); i++ {
		if i < len(html)-7 && lower[i:i+7] == "<script" {
			inScript = true
		}
		if i < len(html)-9 && lower[i:i+9] == "</script>" {
			inScript = false
			i += 8
			continue
		}
		if i < len(html)-6 && lower[i:i+6] == "<style" {
			inStyle = true
		}
		if i < len(html)-8 && lower[i:i+8] == "</style>" {
			inStyle = false
			i += 7
			continue
		}
		if inScript || inStyle {
			continue
		}

		if html[i] == '<' {
			inTag = true
			// Add newline for block elements
			if i < len(html)-3 {
				tag := strings.ToLower(html[i:])
				for _, block := range []string{"<br", "<p", "<div", "<h1", "<h2", "<h3", "<h4", "<li", "<tr"} {
					if strings.HasPrefix(tag, block) {
						result.WriteByte('\n')
						break
					}
				}
			}
			continue
		}
		if html[i] == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteByte(html[i])
		}
	}

	text := result.String()
	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&apos;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&#x27;", "'")
	text = strings.ReplaceAll(text, "&#x2F;", "/")

	return text
}

// ── Resume Critique ───────────────────────────────────

// CritiqueResult is the structured response from resume critique
type CritiqueResult struct {
	Score     int              `json:"score"`
	Issues    []CritiqueIssue  `json:"issues"`
	Strengths []string         `json:"strengths"`
	TopTip    string           `json:"topTip"`
}

type CritiqueIssue struct {
	Cat string `json:"cat"`
	Sev string `json:"sev"`
	Msg string `json:"msg"`
}

const critiqueSystemPrompt = `You are HireIQ's resume critique AI. Analyze resumes and provide actionable feedback.

CRITICAL RULES:
- Do NOT rewrite the resume. Only provide specific recommendations.
- Be direct and actionable — every issue should tell the user exactly what to change.
- Focus on what will make the biggest difference for getting interviews.

Respond with ONLY a JSON object (no markdown, no backticks, no explanation):
{
  "score": 72,
  "issues": [
    {"cat": "Impact", "sev": "critical", "msg": "Your bullet points lack quantifiable metrics. Instead of 'Improved site performance', say 'Reduced page load time by 40% through code splitting, improving Core Web Vitals from 62 to 94'."},
    {"cat": "Language", "sev": "warning", "msg": "Weak verb 'helped' found in 3 bullets. Replace with action verbs: 'Architected', 'Spearheaded', 'Delivered'."}
  ],
  "strengths": ["Clear section organization", "Includes relevant technical skills"],
  "topTip": "The single most impactful change: add 2-3 metrics to your most recent role showing business impact (revenue, users, performance, cost savings)."
}

Categories: Impact, Language, Structure, Formatting, Alignment, Clarity, Punctuation, Length, ATS
Severities: critical (blocks interviews), warning (weakens impression), info (nice to improve)

Guidelines:
- Give 4-8 issues, ordered by severity (critical first)
- Give 2-5 strengths — find genuine positives
- Score 0-100: 90+ is exceptional, 70-89 is solid, 50-69 needs work, <50 has major issues
- Check for: quantifiable metrics, action verbs vs weak verbs (worked, helped, used, did, made, attended), clichés (fast-paced, team player, detail-oriented), ATS compatibility, consistent formatting, appropriate length
- If a target role is provided, check skill alignment and tailor advice accordingly
- topTip should be the single highest-impact change they can make`

// CritiqueResume sends a resume to Claude for structured analysis
func (c *ClaudeClient) CritiqueResume(ctx context.Context, resumeText, jobContext string) (*CritiqueResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Claude API key not configured")
	}

	userContent := "Analyze this resume and return the JSON critique:\n\n" + resumeText
	if jobContext != "" {
		userContent += "\n\n---\n" + jobContext
	}

	reqBody := claudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 2000,
		System:    critiqueSystemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: userContent},
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

	text := strings.TrimSpace(claudeResp.Content[0].Text)
	text = stripCodeFences(text)

	var result CritiqueResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parsing critique result: %w (raw: %s)", err, text)
	}

	return &result, nil
}

// ── Resume Fix Suggestions ────────────────────────────

type FixResult struct {
	Suggestions []FixSuggestion `json:"suggestions"`
}

type FixSuggestion struct {
	Before      string `json:"before"`
	After       string `json:"after"`
	Explanation string `json:"explanation"`
}

const fixSystemPrompt = `You are HireIQ's resume coach. A user's resume has a specific issue. Provide targeted fixes.

CRITICAL RULES:
- Do NOT rewrite the full resume.
- Provide 2-3 specific, actionable before/after suggestions for THIS issue only.
- Each suggestion should show what's currently in the resume (or the pattern that's wrong) and how to improve it.

Respond with ONLY a JSON object (no markdown, no backticks):
{
  "suggestions": [
    {
      "before": "Built and maintained multiple React applications",
      "after": "Architected and scaled 3 React applications serving 50k+ monthly active users",
      "explanation": "Adding scope and metrics demonstrates measurable impact"
    }
  ]
}

Keep suggestions directly tied to the specific issue. Be concrete — use actual text from the resume where possible.`

// FixResumeIssue gets before/after fix suggestions for a specific resume issue
func (c *ClaudeClient) FixResumeIssue(ctx context.Context, resumeText, issueCat, issueSev, issueMsg, jobContext string) (*FixResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Claude API key not configured")
	}

	userContent := fmt.Sprintf(
		"Resume:\n%s\n\nIssue to fix:\nCategory: %s\nSeverity: %s\nDetails: %s",
		resumeText, issueCat, issueSev, issueMsg,
	)
	if jobContext != "" {
		userContent += "\n\n" + jobContext
	}

	reqBody := claudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 1500,
		System:    fixSystemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: userContent},
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

	text := strings.TrimSpace(claudeResp.Content[0].Text)
	text = stripCodeFences(text)

	var result FixResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parsing fix suggestions: %w (raw: %s)", err, text)
	}

	return &result, nil
}

// ── Company Intel AI Estimation ────────────────────────

// CompanyIntelAI is the AI-estimated data for private companies
type CompanyIntelAI struct {
	Company  string `json:"company"`
	IsPublic bool   `json:"isPublic"`
	Profile  struct {
		Industry          string `json:"industry"`
		Sector            string `json:"sector"`
		FullTimeEmployees int64  `json:"fullTimeEmployees"`
		Website           string `json:"website"`
		City              string `json:"city"`
		Country           string `json:"country"`
		Summary           string `json:"summary"`
		Founded           int    `json:"founded"`
	} `json:"profile"`
	Financials struct {
		Valuation        string  `json:"valuation"`
		EstimatedRevenue string  `json:"estimatedRevenue"`
		RevenueGrowth    float64 `json:"revenueGrowth"`
		ProfitMargins    float64 `json:"profitMargins"`
	} `json:"financials"`
	Officers []struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	} `json:"officers"`
}

const companyIntelSystemPrompt = `You are a company research analyst. Given a company name, provide your best estimates of company data.

Respond with ONLY a JSON object (no markdown, no backticks, no explanation):
{
  "company": "Company Name",
  "isPublic": false,
  "profile": {
    "industry": "Software Development",
    "sector": "Technology",
    "fullTimeEmployees": 500,
    "website": "https://company.com",
    "city": "San Francisco",
    "country": "United States",
    "summary": "Brief 2-3 sentence description of what the company does.",
    "founded": 2015
  },
  "financials": {
    "valuation": "$500M",
    "estimatedRevenue": "$30M-$50M annual",
    "revenueGrowth": 0.25,
    "profitMargins": 0.10
  },
  "officers": [
    {"name": "Jane Doe", "title": "CEO & Founder"},
    {"name": "John Smith", "title": "CTO"}
  ]
}

Rules:
- Use your knowledge to provide reasonable estimates. Be transparent these are estimates.
- For financials, use ranges for estimated revenue (e.g. "$10M-$20M annual").
- For valuation, use last known funding round or reasonable estimate.
- Include 2-5 key executives you know of.
- If you genuinely don't know something, use 0 for numbers and empty strings for text.
- isPublic should be false for private companies.`

// EstimateCompanyIntel uses Claude to estimate company data for private companies
func (c *ClaudeClient) EstimateCompanyIntel(ctx context.Context, company string) (*CompanyIntelAI, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Claude API key not configured")
	}

	reqBody := claudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 1500,
		System:    companyIntelSystemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: "Provide company intelligence data for: " + company},
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

	text := strings.TrimSpace(claudeResp.Content[0].Text)
	text = stripCodeFences(text)

	var result CompanyIntelAI
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parsing company intel: %w (raw: %s)", err, text)
	}

	if result.Company == "" {
		result.Company = company
	}

	return &result, nil
}

// ── Job Comparison ─────────────────────────────────────

// CompareResult is the structured response from job comparison
type CompareResult struct {
	Recommendation       string              `json:"recommendation"`       // label of recommended job ("Job A")
	RecommendationReason string              `json:"recommendationReason"` // 1-2 sentence reason
	Rankings             []JobRanking        `json:"rankings"`             // ordered best to worst
	Dimensions           []CompareDimension  `json:"dimensions"`           // per-dimension breakdown
	Summary              string              `json:"summary"`              // overall 2-3 sentence recommendation
	Caveats              []string            `json:"caveats"`              // things to consider
}

type JobRanking struct {
	Label string `json:"label"` // "Job A", "Job B", etc.
	Rank  int    `json:"rank"`  // 1 = best
	Score int    `json:"score"` // overall 0-100
}

type CompareDimension struct {
	Name   string         `json:"name"`   // e.g. "Compensation"
	Winner string         `json:"winner"` // label ("Job A") or "tie"
	Scores map[string]int `json:"scores"` // label -> score 0-100
	Notes  string         `json:"notes"`  // short explanation
}

const compareSystemPrompt = `You are HireIQ's job comparison AI. Compare job opportunities for a candidate and recommend the best fit.

Jobs are labeled "Job A", "Job B", etc. Respond with ONLY a JSON object (no markdown, no backticks):
{
  "recommendation": "Job A",
  "recommendationReason": "Brief 1-2 sentence reason this job is the best fit.",
  "rankings": [
    {"label": "Job A", "rank": 1, "score": 85},
    {"label": "Job B", "rank": 2, "score": 72}
  ],
  "dimensions": [
    {"name": "Compensation", "winner": "Job A", "scores": {"Job A": 85, "Job B": 72}, "notes": "Job A offers $20K higher base with similar equity."},
    {"name": "Growth Potential", "winner": "Job B", "scores": {"Job A": 60, "Job B": 88}, "notes": "Company B is Series A with rapid scaling trajectory."},
    {"name": "Skill Alignment", "winner": "tie", "scores": {"Job A": 78, "Job B": 80}, "notes": "Both roles align well with the candidate's skills."},
    {"name": "Work-Life Balance", "winner": "Job A", "scores": {"Job A": 90, "Job B": 65}, "notes": "Job A is fully remote."},
    {"name": "Company Stability", "winner": "Job A", "scores": {"Job A": 82, "Job B": 55}, "notes": "Company A is profitable with 500+ employees."},
    {"name": "Culture Fit", "winner": "Job B", "scores": {"Job A": 70, "Job B": 85}, "notes": "Company B matches candidate preferences."}
  ],
  "summary": "Overall recommendation with nuance. 2-3 sentences.",
  "caveats": ["Things the candidate should verify", "Up to 3 caveats"]
}

Rules:
- Score each dimension 0-100 for ALL jobs being compared.
- Rankings: order jobs from best (rank 1) to worst. Overall score reflects all dimensions weighted by candidate fit.
- Be honest about trade-offs. Don't oversell any option.
- If info is missing for a dimension, estimate based on what's available and note the uncertainty.
- Consider the candidate's stated preferences (salary range, work style, location) heavily.
- Always provide exactly 6 dimensions in the order: Compensation, Growth Potential, Skill Alignment, Work-Life Balance, Company Stability, Culture Fit.
- The "scores" map must include an entry for every job label.
- For "winner", use the job label or "tie" if scores are within 5 points.`

// CompareJobs sends job details to Claude for structured comparison analysis
func (c *ClaudeClient) CompareJobs(ctx context.Context, jobDescriptions string, userProfile string) (*CompareResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Claude API key not configured")
	}

	userContent := fmt.Sprintf(
		"Compare these jobs for the candidate and return the JSON analysis:\n\n%s\n\n=== CANDIDATE PROFILE ===\n%s",
		jobDescriptions, userProfile,
	)

	reqBody := claudeRequest{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 2500,
		System:    compareSystemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: userContent},
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

	text := strings.TrimSpace(claudeResp.Content[0].Text)
	text = stripCodeFences(text)

	var result CompareResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parsing comparison result: %w (raw: %s)", err, text)
	}

	return &result, nil
}

// stripCodeFences removes markdown ```json ... ``` wrappers
func stripCodeFences(text string) string {
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx != -1 {
			text = text[idx+1:]
		}
		if idx := strings.LastIndex(text, "```"); idx != -1 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}
	return text
}
