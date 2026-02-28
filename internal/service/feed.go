package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/yourusername/hireiq-api/internal/model"
	"github.com/yourusername/hireiq-api/internal/repository"
)

// FeedService orchestrates job feed refresh
type FeedService struct {
	jsearch  *JSearchClient
	feedRepo *repository.FeedRepo
	userRepo *repository.UserRepo
}

func NewFeedService(jsearch *JSearchClient, feedRepo *repository.FeedRepo, userRepo *repository.UserRepo) *FeedService {
	return &FeedService{
		jsearch:  jsearch,
		feedRepo: feedRepo,
		userRepo: userRepo,
	}
}

// RefreshUserFeed fetches new jobs for a user based on their profile
func (s *FeedService) RefreshUserFeed(ctx context.Context, userID uuid.UUID) (int, int, error) {
	// Get user profile
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return 0, 0, fmt.Errorf("user not found: %w", err)
	}

	// Check if refresh is needed (throttle to once per 6 hours)
	lastRefresh, err := s.feedRepo.GetLastRefresh(ctx, userID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check last refresh, continuing anyway")
	}
	if lastRefresh != nil && time.Since(*lastRefresh) < 6*time.Hour {
		log.Info().
			Str("userId", userID.String()).
			Time("lastRefresh", *lastRefresh).
			Msg("Feed recently refreshed, skipping")
		return 0, 0, nil
	}

	// Build search queries from profile
	queries := BuildQueriesFromProfile(user.Skills, user.Location, user.WorkStyle)

	totalFetched := 0
	totalNew := 0

	for _, q := range queries {
		// Fetch from JSearch
		results, err := s.jsearch.Search(ctx, q)
		if err != nil {
			log.Error().Err(err).Str("query", q.Query).Msg("JSearch query failed")
			continue
		}

		totalFetched += len(results)

		// Process each result
		for _, jsJob := range results {
			// Convert to our model
			feedJob := convertJSearchJob(jsJob)

			// Upsert into feed_jobs (dedup by external_id + source)
			stored, err := s.feedRepo.UpsertFeedJob(ctx, feedJob)
			if err != nil {
				log.Error().Err(err).Str("jobId", jsJob.JobID).Msg("Failed to upsert feed job")
				continue
			}

			// Calculate match score
			score := calculateMatchScore(user, stored)

			// Link to user with score
			if err := s.feedRepo.LinkJobToUser(ctx, userID, stored.ID, score); err != nil {
				log.Error().Err(err).Msg("Failed to link job to user")
				continue
			}

			totalNew++
		}

		// Log refresh
		if err := s.feedRepo.LogRefresh(ctx, userID, q.Query, len(results), totalNew); err != nil {
			log.Warn().Err(err).Msg("Failed to log refresh")
		}
	}

	log.Info().
		Str("userId", userID.String()).
		Int("fetched", totalFetched).
		Int("new", totalNew).
		Msg("Feed refresh complete")

	return totalFetched, totalNew, nil
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

	// Truncate description for storage (keep first 2000 chars)
	desc := js.JobDescription
	if len(desc) > 2000 {
		desc = desc[:2000] + "..."
	}

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

// calculateMatchScore computes a 0-100 match score between a user and a feed job
func calculateMatchScore(user *model.User, job *model.FeedJob) int {
	score := 50 // Base score

	if len(user.Skills) == 0 {
		return score
	}

	// Skill overlap (up to +30 points)
	if len(job.RequiredSkills) > 0 {
		matches := 0
		for _, userSkill := range user.Skills {
			for _, jobSkill := range job.RequiredSkills {
				if strings.EqualFold(userSkill, jobSkill) {
					matches++
					break
				}
			}
		}
		skillRatio := float64(matches) / float64(len(job.RequiredSkills))
		score += int(skillRatio * 30)
	}

	// Title/description keyword match (up to +10 points)
	titleLower := strings.ToLower(job.Title + " " + job.Description)
	skillMentions := 0
	for _, skill := range user.Skills {
		if strings.Contains(titleLower, strings.ToLower(skill)) {
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

	// Location match (+5 points)
	if user.WorkStyle != "" && job.Location != "" {
		if strings.EqualFold(user.WorkStyle, "remote") && strings.Contains(strings.ToLower(job.Location), "remote") {
			score += 5
		} else if user.Location != "" && strings.Contains(strings.ToLower(job.Location), strings.ToLower(user.Location)) {
			score += 5
		}
	}

	// Salary match (+5 points)
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
