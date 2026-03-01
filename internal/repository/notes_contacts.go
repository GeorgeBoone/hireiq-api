package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourusername/hireiq-api/internal/model"
)

// ---- Notes ----

type NoteRepo struct {
	pool *pgxpool.Pool
}

func NewNoteRepo(pool *pgxpool.Pool) *NoteRepo {
	return &NoteRepo{pool: pool}
}

func (r *NoteRepo) ListByJob(ctx context.Context, userID, jobID uuid.UUID) ([]model.Note, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, job_id, content, created_at
		FROM notes
		WHERE user_id = $1 AND job_id = $2
		ORDER BY created_at DESC
	`, userID, jobID)
	if err != nil {
		return nil, fmt.Errorf("listing notes: %w", err)
	}
	defer rows.Close()

	var notes []model.Note
	for rows.Next() {
		var n model.Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.JobID, &n.Content, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func (r *NoteRepo) Create(ctx context.Context, userID, jobID uuid.UUID, content string) (*model.Note, error) {
	var n model.Note
	err := r.pool.QueryRow(ctx, `
		INSERT INTO notes (user_id, job_id, content)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, job_id, content, created_at
	`, userID, jobID, content).Scan(&n.ID, &n.UserID, &n.JobID, &n.Content, &n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("creating note: %w", err)
	}
	return &n, nil
}

func (r *NoteRepo) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM notes WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting note: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("note not found")
	}
	return nil
}

// RecentByUser returns the N most recent notes across all jobs (for dashboard)
func (r *NoteRepo) RecentByUser(ctx context.Context, userID uuid.UUID, limit int) ([]model.NoteWithJob, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT n.id, n.user_id, n.job_id, n.content, n.created_at,
		       j.title, j.company
		FROM notes n
		JOIN jobs j ON j.id = n.job_id
		WHERE n.user_id = $1
		ORDER BY n.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("fetching recent notes: %w", err)
	}
	defer rows.Close()

	var notes []model.NoteWithJob
	for rows.Next() {
		var n model.NoteWithJob
		if err := rows.Scan(&n.ID, &n.UserID, &n.JobID, &n.Content, &n.CreatedAt, &n.JobTitle, &n.Company); err != nil {
			return nil, fmt.Errorf("scanning recent note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// ---- Contacts ----

type ContactRepo struct {
	pool *pgxpool.Pool
}

func NewContactRepo(pool *pgxpool.Pool) *ContactRepo {
	return &ContactRepo{pool: pool}
}

func (r *ContactRepo) List(ctx context.Context, userID uuid.UUID, search string) ([]model.Contact, error) {
	query := `
		SELECT id, user_id, name, company, role, connection, phone, email,
		       tip, enriched, enriched_data, created_at, updated_at
		FROM contacts
		WHERE user_id = $1
	`
	args := []any{userID}

	if search != "" {
		query += ` AND (LOWER(name) LIKE $2 OR LOWER(company) LIKE $2
		           OR LOWER(role) LIKE $2 OR LOWER(email) LIKE $2)`
		args = append(args, "%"+search+"%")
	}
	query += " ORDER BY company, name"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing contacts: %w", err)
	}
	defer rows.Close()

	var contacts []model.Contact
	for rows.Next() {
		var c model.Contact
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.Name, &c.Company, &c.Role, &c.Connection,
			&c.Phone, &c.Email, &c.Tip, &c.Enriched, &c.EnrichedData,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, nil
}

func (r *ContactRepo) Create(ctx context.Context, c *model.Contact) (*model.Contact, error) {
	var created model.Contact
	err := r.pool.QueryRow(ctx, `
		INSERT INTO contacts (user_id, name, company, role, connection, phone, email, tip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, name, company, role, connection, phone, email,
		          tip, enriched, enriched_data, created_at, updated_at
	`, c.UserID, c.Name, c.Company, c.Role, c.Connection, c.Phone, c.Email, c.Tip,
	).Scan(
		&created.ID, &created.UserID, &created.Name, &created.Company, &created.Role,
		&created.Connection, &created.Phone, &created.Email, &created.Tip,
		&created.Enriched, &created.EnrichedData, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating contact: %w", err)
	}
	return &created, nil
}

func (r *ContactRepo) Update(ctx context.Context, c *model.Contact) (*model.Contact, error) {
	var updated model.Contact
	err := r.pool.QueryRow(ctx, `
		UPDATE contacts
		SET name = $3, company = $4, role = $5, connection = $6,
		    phone = $7, email = $8, tip = $9, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, name, company, role, connection, phone, email,
		          tip, enriched, enriched_data, created_at, updated_at
	`, c.ID, c.UserID, c.Name, c.Company, c.Role, c.Connection,
		c.Phone, c.Email, c.Tip,
	).Scan(
		&updated.ID, &updated.UserID, &updated.Name, &updated.Company, &updated.Role,
		&updated.Connection, &updated.Phone, &updated.Email, &updated.Tip,
		&updated.Enriched, &updated.EnrichedData, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("updating contact: %w", err)
	}
	return &updated, nil
}

func (r *ContactRepo) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM contacts WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting contact: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("contact not found")
	}
	return nil
}

// ListByCompany returns contacts for a specific company
func (r *ContactRepo) ListByCompany(ctx context.Context, userID uuid.UUID, company string) ([]model.Contact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, company, role, connection, phone, email,
		       tip, enriched, enriched_data, created_at, updated_at
		FROM contacts
		WHERE user_id = $1 AND LOWER(company) = LOWER($2)
		ORDER BY name ASC
	`, userID, company)
	if err != nil {
		return nil, fmt.Errorf("listing contacts by company: %w", err)
	}
	defer rows.Close()

	var contacts []model.Contact
	for rows.Next() {
		var c model.Contact
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.Name, &c.Company, &c.Role, &c.Connection,
			&c.Phone, &c.Email, &c.Tip, &c.Enriched, &c.EnrichedData,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, nil
}

// Stats returns aggregated contact stats for the dashboard
func (r *ContactRepo) Stats(ctx context.Context, userID uuid.UUID) (*model.ContactStats, error) {
	stats := &model.ContactStats{ByCompany: make(map[string]int)}

	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE connection = '1st'),
			COUNT(*) FILTER (WHERE email != ''),
			COUNT(*) FILTER (WHERE phone != '')
		FROM contacts WHERE user_id = $1
	`, userID).Scan(&stats.Total, &stats.FirstDegree, &stats.WithEmail, &stats.WithPhone)
	if err != nil {
		return nil, fmt.Errorf("fetching contact stats: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT company, COUNT(*) FROM contacts
		WHERE user_id = $1
		GROUP BY company ORDER BY COUNT(*) DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("fetching contact company counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var company string
		var count int
		if err := rows.Scan(&company, &count); err != nil {
			return nil, fmt.Errorf("scanning company count: %w", err)
		}
		stats.ByCompany[company] = count
	}

	return stats, nil
}

// BulkCreate inserts multiple contacts, skipping duplicates (same name+company for the user).
// Returns the count of successfully inserted rows and skipped duplicates.
func (r *ContactRepo) BulkCreate(ctx context.Context, userID uuid.UUID, contacts []model.Contact) (inserted int, skipped int, err error) {
	if len(contacts) == 0 {
		return 0, 0, nil
	}

	// Fetch existing contacts to check for duplicates (application-level dedup)
	existing, err := r.List(ctx, userID, "")
	if err != nil {
		return 0, 0, fmt.Errorf("fetching existing contacts: %w", err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, e := range existing {
		key := strings.ToLower(e.Name) + "||" + strings.ToLower(e.Company)
		existingSet[key] = true
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var insertedCount int
	for _, c := range contacts {
		key := strings.ToLower(c.Name) + "||" + strings.ToLower(c.Company)
		if existingSet[key] {
			skipped++
			continue
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO contacts (user_id, name, company, role, connection, phone, email, tip)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, userID, c.Name, c.Company, c.Role, c.Connection, c.Phone, c.Email, c.Tip)
		if err != nil {
			return 0, 0, fmt.Errorf("inserting contact %q: %w", c.Name, err)
		}
		existingSet[key] = true // prevent intra-batch duplicates
		insertedCount++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("committing transaction: %w", err)
	}

	return insertedCount, skipped, nil
}
