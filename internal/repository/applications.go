package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

type ApplicationRepo struct {
	pool *pgxpool.Pool
}

func NewApplicationRepo(pool *pgxpool.Pool) *ApplicationRepo {
	return &ApplicationRepo{pool: pool}
}

// FindByJobID returns the application for a user's job
func (r *ApplicationRepo) FindByJobID(ctx context.Context, userID, jobID uuid.UUID) (*model.Application, error) {
	var a model.Application
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, job_id, status, applied_at, next_step,
		       follow_up_date, follow_up_type, follow_up_urgent,
		       created_at, updated_at
		FROM applications
		WHERE user_id = $1 AND job_id = $2
	`, userID, jobID).Scan(
		&a.ID, &a.UserID, &a.JobID, &a.Status, &a.AppliedAt, &a.NextStep,
		&a.FollowUpDate, &a.FollowUpType, &a.FollowUpUrgent,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding application: %w", err)
	}
	return &a, nil
}

// ListByUser returns all applications with joined job data
func (r *ApplicationRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Application, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.user_id, a.job_id, a.status, a.applied_at, a.next_step,
		       a.follow_up_date, a.follow_up_type, a.follow_up_urgent,
		       a.created_at, a.updated_at,
		       j.title, j.company, j.location, j.salary_range, j.company_color, j.company_logo
		FROM applications a
		JOIN jobs j ON j.id = a.job_id
		WHERE a.user_id = $1
		ORDER BY a.updated_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	defer rows.Close()

	var apps []model.Application
	for rows.Next() {
		var a model.Application
		var job model.Job
		err := rows.Scan(
			&a.ID, &a.UserID, &a.JobID, &a.Status, &a.AppliedAt, &a.NextStep,
			&a.FollowUpDate, &a.FollowUpType, &a.FollowUpUrgent,
			&a.CreatedAt, &a.UpdatedAt,
			&job.Title, &job.Company, &job.Location, &job.SalaryRange,
			&job.CompanyColor, &job.CompanyLogo,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning application row: %w", err)
		}
		a.Job = &job
		apps = append(apps, a)
	}
	return apps, nil
}

// Create creates a new application
func (r *ApplicationRepo) Create(ctx context.Context, a *model.Application) (*model.Application, error) {
	var created model.Application
	err := r.pool.QueryRow(ctx, `
		INSERT INTO applications (user_id, job_id, status, applied_at, next_step,
		                          follow_up_date, follow_up_type, follow_up_urgent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, job_id, status, applied_at, next_step,
		          follow_up_date, follow_up_type, follow_up_urgent,
		          created_at, updated_at
	`, a.UserID, a.JobID, a.Status, a.AppliedAt, a.NextStep,
		a.FollowUpDate, a.FollowUpType, a.FollowUpUrgent,
	).Scan(
		&created.ID, &created.UserID, &created.JobID, &created.Status,
		&created.AppliedAt, &created.NextStep, &created.FollowUpDate,
		&created.FollowUpType, &created.FollowUpUrgent,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating application: %w", err)
	}
	return &created, nil
}

// UpdateStatus changes application status and records history
func (r *ApplicationRepo) UpdateStatus(ctx context.Context, id, userID uuid.UUID, newStatus, note string) (*model.Application, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get current status
	var currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT status FROM applications WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(&currentStatus)
	if err != nil {
		return nil, fmt.Errorf("fetching current status: %w", err)
	}

	// Update status
	var updated model.Application
	err = tx.QueryRow(ctx, `
		UPDATE applications
		SET status = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, job_id, status, applied_at, next_step,
		          follow_up_date, follow_up_type, follow_up_urgent,
		          created_at, updated_at
	`, id, userID, newStatus).Scan(
		&updated.ID, &updated.UserID, &updated.JobID, &updated.Status,
		&updated.AppliedAt, &updated.NextStep, &updated.FollowUpDate,
		&updated.FollowUpType, &updated.FollowUpUrgent,
		&updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("updating application status: %w", err)
	}

	// Record status change history
	_, err = tx.Exec(ctx, `
		INSERT INTO status_history (application_id, from_status, to_status, note)
		VALUES ($1, $2, $3, $4)
	`, id, currentStatus, newStatus, note)
	if err != nil {
		return nil, fmt.Errorf("recording status history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return &updated, nil
}

// GetHistory returns status change history for an application
func (r *ApplicationRepo) GetHistory(ctx context.Context, applicationID uuid.UUID) ([]model.StatusHistory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, application_id, from_status, to_status, changed_at, note
		FROM status_history
		WHERE application_id = $1
		ORDER BY changed_at ASC
	`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("fetching status history: %w", err)
	}
	defer rows.Close()

	var history []model.StatusHistory
	for rows.Next() {
		var h model.StatusHistory
		if err := rows.Scan(&h.ID, &h.ApplicationID, &h.FromStatus, &h.ToStatus, &h.ChangedAt, &h.Note); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		history = append(history, h)
	}
	return history, nil
}

// CountByStatus returns pipeline counts for the dashboard
func (r *ApplicationRepo) CountByStatus(ctx context.Context, userID uuid.UUID) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM applications
		WHERE user_id = $1
		GROUP BY status
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("counting by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scanning count row: %w", err)
		}
		counts[status] = count
	}
	return counts, nil
}
