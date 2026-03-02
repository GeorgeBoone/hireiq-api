package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

// FeedService orchestrates job feed refresh across multiple sources.
type FeedService struct {
	jsearch  *JSearchClient
	remotive *RemotiveClient
	adzuna   *AdzunaClient
	feedRepo *repository.FeedRepo
	userRepo *repository.UserRepo
}

func NewFeedService(
	jsearch *JSearchClient,
	remotive *RemotiveClient,
	adzuna *AdzunaClient,
	feedRepo *repository.FeedRepo,
	userRepo *repository.UserRepo,
) *FeedService {
	return &FeedService{
		jsearch:  jsearch,
		remotive: remotive,
		adzuna:   adzuna,
		feedRepo: feedRepo,
		userRepo: userRepo,
	}
}

// RefreshUserFeed fetches new jobs for a user based on their profile.
// Set force=true to bypass the refresh throttle.
// Sources are fetched concurrently to keep total latency manageable.
func (s *FeedService) RefreshUserFeed(ctx context.Context, userID uuid.UUID, force bool) (int, int, error) {
	// Get user profile
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return 0, 0, fmt.Errorf("user not found: %w", err)
	}

	// Check if refresh is needed (throttle to once per 2 hours, skippable with force)
	if !force {
		lastRefresh, err := s.feedRepo.GetLastRefresh(ctx, userID)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to check last refresh, continuing anyway")
		}
		if lastRefresh != nil && time.Since(*lastRefresh) < 2*time.Hour {
			log.Info().
				Str("userId", userID.String()).
				Time("lastRefresh", *lastRefresh).
				Msg("Feed recently refreshed, skipping")
			return 0, 0, nil
		}
	}

	// Use a 90-second timeout for the entire refresh to prevent runaway requests
	refreshCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Run all sources concurrently
	var mu sync.Mutex
	totalFetched := 0
	totalNew := 0

	var wg sync.WaitGroup

	// ── Source 1: JSearch ──────────────────────────────
	if s.jsearch != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, n := s.refreshFromJSearch(refreshCtx, user, userID)
			mu.Lock()
			totalFetched += f
			totalNew += n
			mu.Unlock()
		}()
	}

	// ── Source 2: Remotive (always available, no key) ──
	if s.remotive != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, n := s.refreshFromRemotive(refreshCtx, user, userID)
			mu.Lock()
			totalFetched += f
			totalNew += n
			mu.Unlock()
		}()
	}

	// ── Source 3: Adzuna (only if configured) ──────────
	if s.adzuna != nil && s.adzuna.Enabled() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, n := s.refreshFromAdzuna(refreshCtx, user, userID)
			mu.Lock()
			totalFetched += f
			totalNew += n
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Log combined refresh
	if err := s.feedRepo.LogRefresh(ctx, userID, "multi-source", totalFetched, totalNew); err != nil {
		log.Warn().Err(err).Msg("Failed to log refresh")
	}

	log.Info().
		Str("userId", userID.String()).
		Int("fetched", totalFetched).
		Int("new", totalNew).
		Msg("Feed refresh complete (all sources)")

	return totalFetched, totalNew, nil
}

// ── Per-source refresh helpers ───────────────────────

func (s *FeedService) refreshFromJSearch(ctx context.Context, user *model.User, userID uuid.UUID) (int, int) {
	queries := BuildQueriesFromProfile(user)
	fetched, newJobs := 0, 0

	log.Info().Int("queryCount", len(queries)).Msg("JSearch: starting refresh")

	for _, q := range queries {
		results, err := s.jsearch.Search(ctx, q)
		if err != nil {
			log.Error().Err(err).Str("source", "jsearch").Str("query", q.Query).Msg("Query failed")
			continue
		}
		fetched += len(results)

		queryNew := 0
		for _, jsJob := range results {
			feedJob := convertJSearchJob(jsJob)
			if s.upsertAndLink(ctx, userID, user, feedJob) {
				queryNew++
			}
		}
		newJobs += queryNew

		log.Info().
			Str("source", "jsearch").
			Str("query", q.Query).
			Int("results", len(results)).
			Int("new", queryNew).
			Msg("Query complete")
	}

	log.Info().Str("source", "jsearch").Int("fetched", fetched).Int("new", newJobs).Msg("JSearch refresh done")
	return fetched, newJobs
}

func (s *FeedService) refreshFromRemotive(ctx context.Context, user *model.User, userID uuid.UUID) (int, int) {
	queries := BuildRemotiveQueries(user)
	if len(queries) == 0 {
		log.Info().Str("source", "remotive").Str("workStyle", user.WorkStyle).Msg("Remotive skipped (no queries)")
		return 0, 0
	}

	fetched, newJobs := 0, 0

	log.Info().Int("queryCount", len(queries)).Str("workStyle", user.WorkStyle).Msg("Remotive: starting refresh")

	for _, q := range queries {
		results, err := s.remotive.Search(ctx, q)
		if err != nil {
			log.Error().Err(err).Str("source", "remotive").Str("search", q.Search).Str("category", q.Category).Msg("Query failed")
			continue
		}
		fetched += len(results)

		queryNew := 0
		for _, rjJob := range results {
			feedJob := convertRemotiveJob(rjJob)
			if s.upsertAndLink(ctx, userID, user, feedJob) {
				queryNew++
			}
		}
		newJobs += queryNew

		log.Info().
			Str("source", "remotive").
			Str("search", q.Search).
			Str("category", q.Category).
			Int("limit", q.Limit).
			Int("results", len(results)).
			Int("new", queryNew).
			Msg("Query complete")
	}

	log.Info().Str("source", "remotive").Int("fetched", fetched).Int("new", newJobs).Msg("Remotive refresh done")
	return fetched, newJobs
}

func (s *FeedService) refreshFromAdzuna(ctx context.Context, user *model.User, userID uuid.UUID) (int, int) {
	queries := BuildAdzunaQueries(user)
	fetched, newJobs := 0, 0

	log.Info().Int("queryCount", len(queries)).Msg("Adzuna: starting refresh")

	for _, q := range queries {
		results, err := s.adzuna.Search(ctx, q)
		if err != nil {
			log.Error().Err(err).Str("source", "adzuna").Str("keywords", q.Keywords).Msg("Query failed")
			continue
		}
		fetched += len(results)

		queryNew := 0
		for _, ajJob := range results {
			feedJob := convertAdzunaJob(ajJob)
			if s.upsertAndLink(ctx, userID, user, feedJob) {
				queryNew++
			}
		}
		newJobs += queryNew

		log.Info().
			Str("source", "adzuna").
			Str("keywords", q.Keywords).
			Int("results", len(results)).
			Int("new", queryNew).
			Msg("Query complete")
	}

	log.Info().Str("source", "adzuna").Int("fetched", fetched).Int("new", newJobs).Msg("Adzuna refresh done")
	return fetched, newJobs
}

// upsertAndLink is the shared upsert + score + link logic for all sources.
func (s *FeedService) upsertAndLink(ctx context.Context, userID uuid.UUID, user *model.User, feedJob *model.FeedJob) bool {
	// Sanitize all string fields to ensure valid UTF-8 for PostgreSQL
	sanitizeFeedJob(feedJob)

	stored, err := s.feedRepo.UpsertFeedJob(ctx, feedJob)
	if err != nil {
		log.Error().Err(err).Str("source", feedJob.Source).Str("externalId", feedJob.ExternalID).Msg("Failed to upsert feed job")
		return false
	}

	score := calculateMatchScore(user, stored)

	if err := s.feedRepo.LinkJobToUser(ctx, userID, stored.ID, score); err != nil {
		log.Error().Err(err).Str("source", feedJob.Source).Msg("Failed to link job to user")
		return false
	}

	return true
}

// RescoreUserFeed recalculates match scores for all existing feed jobs
// for a user. Call this when the user's profile changes (e.g. target roles, skills).
func (s *FeedService) RescoreUserFeed(ctx context.Context, userID uuid.UUID) (int, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return 0, fmt.Errorf("user not found: %w", err)
	}

	jobs, err := s.feedRepo.GetUserFeedForRescore(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("getting feed for rescore: %w", err)
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	scores := make(map[uuid.UUID]int, len(jobs))
	for i := range jobs {
		scores[jobs[i].ID] = calculateMatchScore(user, &jobs[i])
	}

	if err := s.feedRepo.BatchUpdateMatchScores(ctx, userID, scores); err != nil {
		return 0, fmt.Errorf("batch updating scores: %w", err)
	}

	log.Info().
		Str("userId", userID.String()).
		Int("rescored", len(scores)).
		Msg("Feed match scores recalculated")

	return len(scores), nil
}

// convertJSearchJob transforms a JSearch API result into our FeedJob model
func convertJSearchJob(js JSearchJob) *model.FeedJob {
	// Build location string
	location := ""
	if js.JobIsRemote {
		location = "Remote"
	}
	if js.JobCity != "" {
		if location != "" {
			location += " / "
		}
		location += js.JobCity
		if js.JobState != "" {
			location += ", " + js.JobState
		}
	}

	// Parse salary
	salaryMin := 0
	salaryMax := 0
	salaryText := ""
	if js.JobMinSalary != nil {
		salaryMin = int(*js.JobMinSalary)
	}
	if js.JobMaxSalary != nil {
		salaryMax = int(*js.JobMaxSalary)
	}
	if salaryMin > 0 || salaryMax > 0 {
		if js.JobSalaryPeriod == "YEAR" {
			salaryText = fmt.Sprintf("$%dk - $%dk/yr", salaryMin/1000, salaryMax/1000)
		} else if js.JobSalaryPeriod == "HOUR" {
			salaryText = fmt.Sprintf("$%d - $%d/hr", salaryMin, salaryMax)
		}
	}

	// Parse employment type
	jobType := "full-time"
	switch strings.ToUpper(js.JobEmploymentType) {
	case "PARTTIME":
		jobType = "part-time"
	case "CONTRACTOR":
		jobType = "contract"
	case "INTERN":
		jobType = "internship"
	}

	// Parse posted date
	var postedAt *time.Time
	if js.JobPostedAt != "" {
		if t, err := time.Parse(time.RFC3339, js.JobPostedAt); err == nil {
			postedAt = &t
		}
	}

	// Truncate description for storage (UTF-8 safe)
	desc := truncateUTF8(js.JobDescription, 2000)

	skills := js.JobRequiredSkills
	if skills == nil {
		skills = []string{}
	}

	return &model.FeedJob{
		ExternalID:     js.JobID,
		Source:         "jsearch",
		Title:          js.JobTitle,
		Company:        js.EmployerName,
		Location:       location,
		SalaryMin:      salaryMin,
		SalaryMax:      salaryMax,
		SalaryText:     salaryText,
		JobType:        jobType,
		Description:    desc,
		RequiredSkills: skills,
		ApplyURL:       js.JobApplyLink,
		CompanyLogo:    js.EmployerLogo,
		PostedAt:       postedAt,
	}
}

// calculateMatchScore computes a 0-100 match score between a user and a feed job.
// Scoring breakdown:
//   - Target role match:  up to +25 points (highest weight)
//   - Skill overlap:      up to +25 points
//   - Keyword mentions:   up to +10 points
//   - Location match:     up to +5 points
//   - Salary match:       up to +5 points
//   - Base:               30 points
func calculateMatchScore(user *model.User, job *model.FeedJob) int {
	score := 30 // Base score

	jobTitleLower := strings.ToLower(job.Title)
	jobTextLower := strings.ToLower(job.Title + " " + job.Description)

	// ── Target role match (up to +25 points) ──
	// This is the highest-weight signal: does the job title match what the user wants?
	if len(user.TargetRoles) > 0 {
		bestRoleMatch := 0.0
		for _, role := range user.TargetRoles {
			roleLower := strings.ToLower(strings.TrimSpace(role))
			if roleLower == "" {
				continue
			}

			// Exact title match → full points
			if strings.Contains(jobTitleLower, roleLower) {
				bestRoleMatch = 1.0
				break
			}

			// Check individual words from the role in the job title
			roleWords := strings.Fields(roleLower)
			matchedWords := 0
			for _, w := range roleWords {
				if strings.Contains(jobTitleLower, w) {
					matchedWords++
				}
			}
			if len(roleWords) > 0 {
				ratio := float64(matchedWords) / float64(len(roleWords))
				if ratio > bestRoleMatch {
					bestRoleMatch = ratio
				}
			}

			// Also check description for role mention (half credit)
			if bestRoleMatch < 0.5 && strings.Contains(jobTextLower, roleLower) {
				bestRoleMatch = 0.5
			}
		}
		score += int(bestRoleMatch * 25)
	}

	// ── Skill overlap (up to +25 points) ──
	if len(user.Skills) > 0 {
		userSkillSet := make(map[string]bool, len(user.Skills))
		for _, s := range user.Skills {
			userSkillSet[strings.ToLower(s)] = true
		}

		if len(job.RequiredSkills) > 0 {
			matches := 0
			for _, jobSkill := range job.RequiredSkills {
				if userSkillSet[strings.ToLower(jobSkill)] {
					matches++
				}
			}
			skillRatio := float64(matches) / float64(len(job.RequiredSkills))
			score += int(skillRatio * 25)
		}

		// Skill keyword mentions in title/description (up to +10 points)
		skillMentions := 0
		for _, skill := range user.Skills {
			if strings.Contains(jobTextLower, strings.ToLower(skill)) {
				skillMentions++
			}
		}
		if skillMentions > 0 {
			bonus := skillMentions * 3
			if bonus > 10 {
				bonus = 10
			}
			score += bonus
		}
	}

	// ── Location match (+5 points) ──
	if user.WorkStyle != "" && job.Location != "" {
		if strings.EqualFold(user.WorkStyle, "remote") && strings.Contains(strings.ToLower(job.Location), "remote") {
			score += 5
		} else if user.Location != "" && strings.Contains(strings.ToLower(job.Location), strings.ToLower(user.Location)) {
			score += 5
		}
	}

	// ── Salary match (+5 points) ──
	if user.SalaryMin > 0 && job.SalaryMax > 0 {
		if job.SalaryMax >= user.SalaryMin {
			score += 5
		}
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}

	return score
}
