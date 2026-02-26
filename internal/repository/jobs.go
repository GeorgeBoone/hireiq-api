package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type JobRepo struct {
	pool *pgxpool.Pool
}

func NewJobRepo(pool *pgxpool.Pool) *JobRepo {
	return &JobRepo{pool: pool}
}

// List returns all jobs for a user, with optional filters
func (r *JobRepo) List(ctx context.Context, userID uuid.UUID, filter JobFilter) ([]model.Job, error) {
	query := `
		SELECT id, user_id, external_id, source, title, company, location,
		       salary_range, job_type, description, tags, required_skills,
		       preferred_skills, apply_url, hiring_email, company_logo,
		       company_color, match_score, bookmarked, status, created_at, updated_at
		FROM jobs
		WHERE user_id = $1
	`
	args := []any{userID}
	argIdx := 2

	if filter.BookmarkedOnly {
		query += fmt.Sprintf(" AND bookmarked = $%d", argIdx)
		args = append(args, true)
		argIdx++
	}
	if filter.Search != "" {
		query += fmt.Sprintf(" AND (LOWER(title) LIKE $%d OR LOWER(company) LIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+filter.Search+"%")
		argIdx++
	}
	if filter.LocationType == "remote" {
		query += " AND LOWER(location) LIKE '%remote%'"
	} else if filter.LocationType == "onsite" {
		query += " AND LOWER(location) NOT LIKE '%remote%'"
	}

	query += " ORDER BY match_score DESC, created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.Job
	for rows.Next() {
		var j model.Job
		err := rows.Scan(
			&j.ID, &j.UserID, &j.ExternalID, &j.Source, &j.Title, &j.Company,
			&j.Location, &j.SalaryRange, &j.JobType, &j.Description, &j.Tags,
			&j.RequiredSkills, &j.PreferredSkills, &j.ApplyURL, &j.HiringEmail,
			&j.CompanyLogo, &j.CompanyColor, &j.MatchScore, &j.Bookmarked,
			&j.Status,
			&j.CreatedAt, &j.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning job row: %w", err)
		}
		jobs = append(jobs, j)
	}

	return jobs, nil
}

// FindByID returns a single job
func (r *JobRepo) FindByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*model.Job, error) {
	var j model.Job
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, external_id, source, title, company, location,
		       salary_range, job_type, description, tags, required_skills,
		       preferred_skills, apply_url, hiring_email, company_logo,
		       company_color, match_score, bookmarked, created_at, updated_at
		FROM jobs
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&j.ID, &j.UserID, &j.ExternalID, &j.Source, &j.Title, &j.Company,
		&j.Location, &j.SalaryRange, &j.JobType, &j.Description, &j.Tags,
		&j.RequiredSkills, &j.PreferredSkills, &j.ApplyURL, &j.HiringEmail,
		&j.CompanyLogo, &j.CompanyColor, &j.MatchScore, &j.Bookmarked, &j.Status,
		&j.CreatedAt, &j.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding job: %w", err)
	}
	return &j, nil
}

// Create inserts a new job
func (r *JobRepo) Create(ctx context.Context, j *model.Job) (*model.Job, error) {
	var created model.Job
	err := r.pool.QueryRow(ctx, `
		INSERT INTO jobs (user_id, external_id, source, title, company, location,
		                  salary_range, job_type, description, tags, required_skills,
		                  preferred_skills, apply_url, hiring_email, company_logo,
		                  company_color, match_score, bookmarked, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id, user_id, external_id, source, title, company, location,
		          salary_range, job_type, description, tags, required_skills,
		          preferred_skills, apply_url, hiring_email, company_logo,
		          company_color, match_score, bookmarked, status, created_at, updated_at
	`, j.UserID, j.ExternalID, j.Source, j.Title, j.Company, j.Location,
		j.SalaryRange, j.JobType, j.Description, j.Tags, j.RequiredSkills,
		j.PreferredSkills, j.ApplyURL, j.HiringEmail, j.CompanyLogo,
		j.CompanyColor, j.MatchScore, j.Bookmarked, j.Status,
	).Scan(
		&created.ID, &created.UserID, &created.ExternalID, &created.Source,
		&created.Title, &created.Company, &created.Location, &created.SalaryRange,
		&created.JobType, &created.Description, &created.Tags, &created.RequiredSkills,
		&created.PreferredSkills, &created.ApplyURL, &created.HiringEmail,
		&created.CompanyLogo, &created.CompanyColor, &created.MatchScore,
		&created.Bookmarked, &created.Status, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating job: %w", err)
	}
	return &created, nil
}

// Update updates a job
func (r *JobRepo) Update(ctx context.Context, j *model.Job) (*model.Job, error) {
	var updated model.Job
	err := r.pool.QueryRow(ctx, `
		UPDATE jobs
		SET title = $3, company = $4, location = $5, salary_range = $6,
		    job_type = $7, description = $8, tags = $9, required_skills = $10,
		    preferred_skills = $11, apply_url = $12, hiring_email = $13,
		    match_score = $14, bookmarked = $15, status = $16, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, external_id, source, title, company, location,
		          salary_range, job_type, description, tags, required_skills,
		          preferred_skills, apply_url, hiring_email, company_logo,
		          company_color, match_score, bookmarked, status, created_at, updated_at
	`, j.ID, j.UserID, j.Title, j.Company, j.Location, j.SalaryRange,
		j.JobType, j.Description, j.Tags, j.RequiredSkills, j.PreferredSkills,
		j.ApplyURL, j.HiringEmail, j.MatchScore, j.Bookmarked,
		j.Status,
	).Scan(
		&updated.ID, &updated.UserID, &updated.ExternalID, &updated.Source,
		&updated.Title, &updated.Company, &updated.Location, &updated.SalaryRange,
		&updated.JobType, &updated.Description, &updated.Tags, &updated.RequiredSkills,
		&updated.PreferredSkills, &updated.ApplyURL, &updated.HiringEmail,
		&updated.CompanyLogo, &updated.CompanyColor, &updated.MatchScore,
		&updated.Bookmarked, &updated.Status, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("updating job: %w", err)
	}
	return &updated, nil
}

// Delete removes a job
func (r *JobRepo) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

// ToggleBookmark flips the bookmarked flag
func (r *JobRepo) ToggleBookmark(ctx context.Context, id uuid.UUID, userID uuid.UUID) (bool, error) {
	var bookmarked bool
	err := r.pool.QueryRow(ctx, `
		UPDATE jobs SET bookmarked = NOT bookmarked, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING bookmarked
	`, id, userID).Scan(&bookmarked)
	if err != nil {
		return false, fmt.Errorf("toggling bookmark: %w", err)
	}
	return bookmarked, nil
}

// JobFilter holds query parameters for listing jobs
type JobFilter struct {
	Search        string
	LocationType  string // "", "remote", "onsite"
	BookmarkedOnly bool
}

// UpdateStatus updates only the status field of a job
// Add this method to JobRepo in repository/jobs.go
func (r *JobRepo) UpdateStatus(ctx context.Context, jobID, userID uuid.UUID, status string) error {
	result, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = $1, updated_at = now()
		 WHERE id = $2 AND user_id = $3`,
		status, jobID, userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}
