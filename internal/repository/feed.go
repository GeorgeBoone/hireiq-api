package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type FeedRepo struct {
	pool *pgxpool.Pool
}

func NewFeedRepo(pool *pgxpool.Pool) *FeedRepo {
	return &FeedRepo{pool: pool}
}

// UpsertFeedJob inserts a feed job or returns the existing one (dedup by external_id + source)
func (r *FeedRepo) UpsertFeedJob(ctx context.Context, job *model.FeedJob) (*model.FeedJob, error) {
	var result model.FeedJob
	err := r.pool.QueryRow(ctx, `
		INSERT INTO feed_jobs (external_id, source, title, company, location,
		                       salary_min, salary_max, salary_text, job_type,
		                       description, required_skills, apply_url, company_logo,
		                       posted_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (external_id, source) DO UPDATE SET
			title = EXCLUDED.title,
			fetched_at = now()
		RETURNING id, external_id, source, title, company, location,
		          salary_min, salary_max, salary_text, job_type,
		          description, required_skills, apply_url, company_logo,
		          posted_at, fetched_at
	`, job.ExternalID, job.Source, job.Title, job.Company, job.Location,
		job.SalaryMin, job.SalaryMax, job.SalaryText, job.JobType,
		job.Description, job.RequiredSkills, job.ApplyURL, job.CompanyLogo,
		job.PostedAt, time.Now().Add(7*24*time.Hour), // Expires in 7 days
	).Scan(
		&result.ID, &result.ExternalID, &result.Source, &result.Title, &result.Company,
		&result.Location, &result.SalaryMin, &result.SalaryMax, &result.SalaryText,
		&result.JobType, &result.Description, &result.RequiredSkills, &result.ApplyURL,
		&result.CompanyLogo, &result.PostedAt, &result.FetchedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upserting feed job: %w", err)
	}
	return &result, nil
}

// LinkJobToUser creates a user_feed entry linking a feed job to a user with a match score
func (r *FeedRepo) LinkJobToUser(ctx context.Context, userID, feedJobID uuid.UUID, matchScore int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_feed (user_id, feed_job_id, match_score)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, feed_job_id) DO UPDATE SET
			match_score = EXCLUDED.match_score
	`, userID, feedJobID, matchScore)
	if err != nil {
		return fmt.Errorf("linking job to user: %w", err)
	}
	return nil
}

// GetUserFeed returns feed jobs for a user, ordered by match score, excluding dismissed
func (r *FeedRepo) GetUserFeed(ctx context.Context, userID uuid.UUID, limit int) ([]model.FeedJob, error) {
	if limit == 0 {
		limit = 30
	}

	rows, err := r.pool.Query(ctx, `
		SELECT fj.id, fj.external_id, fj.source, fj.title, fj.company, fj.location,
		       fj.salary_min, fj.salary_max, fj.salary_text, fj.job_type,
		       fj.description, fj.required_skills, fj.apply_url, fj.company_logo,
		       fj.posted_at, fj.fetched_at,
		       uf.match_score, uf.dismissed, uf.saved, uf.saved_job_id
		FROM user_feed uf
		JOIN feed_jobs fj ON fj.id = uf.feed_job_id
		WHERE uf.user_id = $1
		  AND uf.dismissed = false
		  AND (fj.expires_at IS NULL OR fj.expires_at > now())
		ORDER BY uf.match_score DESC, fj.posted_at DESC NULLS LAST
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("getting user feed: %w", err)
	}
	defer rows.Close()

	var jobs []model.FeedJob
	for rows.Next() {
		var j model.FeedJob
		err := rows.Scan(
			&j.ID, &j.ExternalID, &j.Source, &j.Title, &j.Company, &j.Location,
			&j.SalaryMin, &j.SalaryMax, &j.SalaryText, &j.JobType,
			&j.Description, &j.RequiredSkills, &j.ApplyURL, &j.CompanyLogo,
			&j.PostedAt, &j.FetchedAt,
			&j.MatchScore, &j.Dismissed, &j.Saved, &j.SavedJobID,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning feed job: %w", err)
		}
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// DismissFeedJob marks a feed job as dismissed for a user
func (r *FeedRepo) DismissFeedJob(ctx context.Context, userID, feedJobID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_feed SET dismissed = true
		WHERE user_id = $1 AND feed_job_id = $2
	`, userID, feedJobID)
	if err != nil {
		return fmt.Errorf("dismissing feed job: %w", err)
	}
	return nil
}

// SaveFeedJobToCRM copies a feed job into the user's jobs table and marks it saved
func (r *FeedRepo) SaveFeedJobToCRM(ctx context.Context, userID, feedJobID uuid.UUID) (*model.Job, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get the feed job
	var fj model.FeedJob
	err = tx.QueryRow(ctx, `
		SELECT id, external_id, source, title, company, location,
		       salary_min, salary_max, salary_text, job_type,
		       description, required_skills, apply_url, company_logo
		FROM feed_jobs WHERE id = $1
	`, feedJobID).Scan(
		&fj.ID, &fj.ExternalID, &fj.Source, &fj.Title, &fj.Company, &fj.Location,
		&fj.SalaryMin, &fj.SalaryMax, &fj.SalaryText, &fj.JobType,
		&fj.Description, &fj.RequiredSkills, &fj.ApplyURL, &fj.CompanyLogo,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("feed job not found")
	}
	if err != nil {
		return nil, fmt.Errorf("getting feed job: %w", err)
	}

	// Build salary range text
	salaryRange := fj.SalaryText
	if salaryRange == "" && fj.SalaryMin > 0 {
		salaryRange = fmt.Sprintf("$%dk - $%dk", fj.SalaryMin/1000, fj.SalaryMax/1000)
	}

	// Get the match score from user_feed
	var matchScore int
	_ = tx.QueryRow(ctx, `
		SELECT match_score FROM user_feed
		WHERE user_id = $1 AND feed_job_id = $2
	`, userID, feedJobID).Scan(&matchScore)

	// Insert into user's jobs
	var job model.Job
	err = tx.QueryRow(ctx, `
		INSERT INTO jobs (user_id, external_id, source, title, company, location,
		                  salary_range, job_type, description, required_skills,
		                  apply_url, company_logo, match_score, bookmarked, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, false, 'saved')
		RETURNING id, user_id, external_id, source, title, company, location,
		          salary_range, job_type, description, tags, required_skills,
		          preferred_skills, apply_url, hiring_email, company_logo,
		          company_color, match_score, bookmarked, status, created_at, updated_at
	`, userID, fj.ExternalID, fj.Source, fj.Title, fj.Company, fj.Location,
		salaryRange, fj.JobType, fj.Description, fj.RequiredSkills,
		fj.ApplyURL, fj.CompanyLogo, matchScore,
	).Scan(
		&job.ID, &job.UserID, &job.ExternalID, &job.Source, &job.Title, &job.Company,
		&job.Location, &job.SalaryRange, &job.JobType, &job.Description, &job.Tags,
		&job.RequiredSkills, &job.PreferredSkills, &job.ApplyURL, &job.HiringEmail,
		&job.CompanyLogo, &job.CompanyColor, &job.MatchScore, &job.Bookmarked, &job.Status,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("saving job to CRM: %w", err)
	}

	// Mark as saved in user_feed
	_, err = tx.Exec(ctx, `
		UPDATE user_feed SET saved = true, saved_job_id = $3
		WHERE user_id = $1 AND feed_job_id = $2
	`, userID, feedJobID, job.ID)
	if err != nil {
		return nil, fmt.Errorf("marking feed job as saved: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return &job, nil
}

// GetLastRefresh returns when a user's feed was last refreshed
func (r *FeedRepo) GetLastRefresh(ctx context.Context, userID uuid.UUID) (*time.Time, error) {
	var refreshedAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT refreshed_at FROM feed_refresh_log
		WHERE user_id = $1
		ORDER BY refreshed_at DESC
		LIMIT 1
	`, userID).Scan(&refreshedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting last refresh: %w", err)
	}
	return &refreshedAt, nil
}

// LogRefresh records a feed refresh
func (r *FeedRepo) LogRefresh(ctx context.Context, userID uuid.UUID, query string, fetched, newJobs int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO feed_refresh_log (user_id, query_used, jobs_fetched, jobs_new)
		VALUES ($1, $2, $3, $4)
	`, userID, query, fetched, newJobs)
	if err != nil {
		return fmt.Errorf("logging refresh: %w", err)
	}
	return nil
}

// CleanExpiredFeedJobs removes feed jobs past their expiration
func (r *FeedRepo) CleanExpiredFeedJobs(ctx context.Context) (int, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM feed_jobs WHERE expires_at < now()
	`)
	if err != nil {
		return 0, fmt.Errorf("cleaning expired jobs: %w", err)
	}
	return int(result.RowsAffected()), nil
}
